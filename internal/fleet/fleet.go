// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package fleet is the claudia-backed implementation of butler.Fleet:
// launches, directs, and stops disposable agent processes behind
// durable threads. It also implements butler.Participants for agents
// that exist only in the registry (🎯T114 unified deliver path).
package fleet

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/seatstate"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/attrib"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/mcpattach"
	"github.com/marcelocantos/jevons/internal/thread"
)

// Default timeouts for the launch handshake and a directed turn's reply.
const (
	defaultReadyTimeout = 45 * time.Second
	defaultReplyTimeout = 10 * time.Minute
)

// There is deliberately no post-ready settle on the launch path.
//
// jevons used to sleep two seconds after claudia reported a Claude Session
// ready (🎯T282), because Claude Code's startup splash draws a prompt box
// that satisfied claudia's ready pattern while the TUI was still mounting:
// a turn sent into that window had its submit keystroke dropped, and the
// direct blocked until its caller timed out (intermittent journey J10
// hangs, the composer still holding the prompt minutes later).
//
// That belonged in claudia, which owns the pane, and now lives there:
// tmuxagent.MatchReady rejects the splash by its ghost placeholder, so
// ready means the composer accepts and submits input (🎯T284). Waiting on
// top of a trustworthy signal only taxes every agent start. If a launch
// race resurfaces, fix the signal in claudia — do not reintroduce a sleep
// here.

// Claudia adapts a claudia.Registry to the butler.Fleet interface and
// to butler.Participants (agent-only deliver).
type Claudia struct {
	reg             *claudia.Registry
	defaultProvider claudia.Provider
	readyTimeout    time.Duration
	replyTimeout    time.Duration
	// Installed at startup; synchronous directs are agent-origin requests.
	recordRequest func(name, text string) error

	mu sync.Mutex

	// seats is the daemon's one seat-state authority (🎯T766.2). The
	// fleet stops and removes processes, so it is the party that knows when
	// a seat stops being alive. Nil in tests.
	seats *seatstate.Authority

	// logf is the durable event sink for migration decisions (🎯 audit:
	// migrate.go previously recorded its richest decisions — handover
	// dispatch, interrupt-before-forced-migrate, "registry missed a
	// provider switch", session remap — only via slog, which is not
	// captured in the eventlog or jevons_logs_tail. Nil is safe: a
	// fleet.Claudia built without SetEventLogger just stays slog-only,
	// same as before this field existed.
	logf fleetlog.Logger

	// Provider migration (🎯T285): session roots resolve a predecessor's
	// transcript, and handovers persists the pointer across the rotation
	// that destroys it. Both optional — without them Launch behaves as
	// before and migration is unavailable rather than silently cold.
	roots     discovery.Roots
	handovers *handover.Store
	// seatPlans holds migration and placement fields the published
	// AgentDef does not carry.
	seatPlans *claudia.SeatPolicyStore
	rotations *handover.RotationStore
	// retainedHistory reads the host's durable agent journal when a stopped
	// predecessor has no provider transcript (notably direct Codex seats).
	// It supplies inert history; Claudia still owns transfer and handover.
	retainedHistory func(name string) (string, error)

	// onModelSwitch records a model change that actually landed. Nil drops
	// the note; the switch still happens. Same-model, same-provider calls
	// are not delivered — a no-op must not look like a switch in the journal.
	onModelSwitch func(*ModelSwitch)

	// liveSetModel replaces Agent.SetModel in tests. Nil uses the live agent.
	liveSetModel func(name, model string) error

	// onLaunch brackets a launch this adapter performs (🎯T426). It is called
	// BEFORE the process comes up and returns the function to call once it
	// has. The host attaches whatever must ride EVERY launch — today the
	// mcpserver event sink, whose absence takes an agent's turn ends,
	// send-queue drain, upward reports and auto-deregistration with it.
	//
	// A bracket rather than a notification because the host also has to know
	// about the WINDOW: from reg.Launch to the readiness handshake the
	// successor is registered, alive and not yet wired, and a watcher that
	// cannot tell that state from a launch road nobody wired reports a
	// healthy compaction as an outage.
	//
	// This adapter is the shared road for compaction (the context ceiling
	// governor), provider migration, thread launches and deliver-rehydrate,
	// so the hook lands once instead of at four call sites that each have to
	// remember. Name only, deliberately: the host resolves the process from
	// the registry it already owns, and fleet stays free of mcpserver.
	onLaunch func(name string) func()

	// removals is the accounted-removal chokepoint (🎯T435). A thread that
	// drops its registry row disappears from the fleet surface, and a
	// disappearance nobody can explain is read as an orphaning. Nil is safe
	// — the removal still happens, it simply has no journal to reach.
	removals *fleetlog.Account

	// mcp is this daemon's jevonsmcp attach (claudia 🎯T40). Zero value
	// leaves AgentDef.MCPServers empty — hermetic tests that never call
	// SetMCP keep prior behaviour.
	mcp mcpattach.Args

	// turnGate is the 🎯T392.1 spend lever: delay, pause, or refuse the
	// next turn. Nil admits. The gate must not remint, rewind, or seed.
	turnGate func(agent, sessionID string) error
}

// NewClaudia wraps a registry as a Fleet. Default provider resolves from
// env / Grok (🎯T148); main should call SetDefaultProvider with the
// config-resolved value.
func NewClaudia(reg *claudia.Registry) *Claudia {
	a := seatstate.RegistryAuthority(reg)
	a.ObserveRegistry(reg)
	return &Claudia{
		seats:           a,
		reg:             reg,
		defaultProvider: cli.ResolveProvider("", ""),
		readyTimeout:    defaultReadyTimeout,
		replyTimeout:    defaultReplyTimeout,
	}
}

// SetRemovalAccount installs the accounted-removal chokepoint (🎯T435). The
// daemon builds one Account for the process and gives every removal path the
// same one, so a row leaving here is explained on the surfaces read elsewhere.
// SetEventLogger wires the durable event sink migration decisions log to,
// alongside their existing slog output. Nil keeps the package slog-only.
func (f *Claudia) SetEventLogger(log fleetlog.Logger) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logf = log
}

// logEvent is the nil-safe dual-write helper migrate.go uses alongside its
// slog calls, so a migration decision reaches the durable eventlog without
// requiring the reader to also have the right terminal scrollback.
func (f *Claudia) logEvent(component, decision string, fields map[string]any) {
	if f == nil {
		return
	}
	f.mu.Lock()
	log := f.logf
	f.mu.Unlock()
	if log != nil {
		log(component, decision, fields)
	}
}

func (f *Claudia) SetRemovalAccount(a *fleetlog.Account) {
	if f == nil {
		return
	}
	f.removals = a
}

// SetModelSwitchHook installs the durable note for a landed model change.
// Nil clears it. The hook runs only after the new model is on the registry
// row (or, for a live SetModel, after that call and the row write both
// succeed). Fleet stays free of the event journal; the host writes it.
func (f *Claudia) SetModelSwitchHook(fn func(*ModelSwitch)) {
	if f == nil {
		return
	}
	f.onModelSwitch = fn
}

// SetLaunchHook installs the per-launch host callback (🎯T426). Nil clears it.
func (f *Claudia) SetLaunchHook(fn func(name string) func()) {
	if f == nil {
		return
	}
	f.onLaunch = fn
}

// SetTurnGate installs the 🎯T392.1 turn-rate seam. Nil (the default)
// admits every turn. The function must not remint, rewind, or inject a
// T285 seed — session identity is T40.2 / T285.
func (f *Claudia) SetTurnGate(fn func(agent, sessionID string) error) {
	if f == nil {
		return
	}
	f.turnGate = fn
}

// allowTurn asks the turn-rate gate whether this send may run. Nil gate
// admits. A deferred error leaves the registry session id untouched.
func (f *Claudia) allowTurn(id string) error {
	if f == nil || f.turnGate == nil {
		return nil
	}
	sessionID := ""
	if f.reg != nil {
		if def := f.reg.Def(id); def != nil {
			sessionID = def.SessionID
		}
	}
	return f.turnGate(id, sessionID)
}

func (f *Claudia) SetRequestRecorder(fn func(name, text string) error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordRequest = fn
}

// launching tells the host a launch has begun for name and returns the
// function that ends it. The completion runs after the readiness handshake, so
// the host never wires a half-started pane, and the span between the two is
// the host's answer to "is this unwired process a fault or a launch".
func (f *Claudia) launching(name string) func() {
	if f == nil || f.onLaunch == nil {
		return func() {}
	}
	if done := f.onLaunch(name); done != nil {
		return done
	}
	return func() {}
}

// SetDefaultProvider sets the daemon-wide backend for new threads when
// the thread record has no provider (🎯T148).
func (f *Claudia) SetDefaultProvider(p claudia.Provider) {
	if p != "" {
		f.defaultProvider = p
	}
}

// SetMCP installs the live jevonsmcp endpoint so every mint/Launch carries
// discovered system servers plus this daemon's HTTP MCP (🎯T464 / claudia T40).
func (f *Claudia) SetMCP(a mcpattach.Args) {
	if f == nil {
		return
	}
	f.mcp = a
}

// SessionMCPServers is the list a registry row should carry for provider.
func (f *Claudia) SessionMCPServers(provider claudia.Provider, workDir string) []claudia.MCPServer {
	if f == nil || strings.TrimSpace(f.mcp.URL) == "" {
		return nil
	}
	return mcpattach.SessionServers(f.mcp, provider, workDir)
}

func mcpServersEqual(a, b []claudia.MCPServer) bool {
	return mcpattach.ServersEqual(a, b)
}

// providerForLaunch picks the registry provider for a thread Launch.
// Never clobbers a non-empty stored provider (resume keeps backend).
func providerForLaunch(stored, fromThread, defaultProv claudia.Provider) claudia.Provider {
	return cli.SelectAgentProvider(string(fromThread), stored, defaultProv)
}

// CodexWorkSandbox is the Session sandbox a mint should request.
// Codex work agents need workspace-write (claudia 🎯T37); asides and
// other providers stay empty so claudia's read-only default holds.
// role=auditor is always read-only (🎯T536.2) even when purpose=work.
// CodexWorkSandboxTuning is what a Codex work seat needs on top of the
// sandbox mode so it can do the job the project asks of it (🎯T598).
//
// workspace-write alone gives a seat an empty writable-root list and no
// network, which is exactly enough to edit files and not enough to prove
// anything: bin/gate records its verdict under ~/.jevons/gates,
// deliberately outside the tree so a gate cannot be edited by the work it
// judges (🎯T386), and the journey and UI oracles bind loopback ports.
// A seat without these comes up looking healthy and fails at its first
// gate — which is how 🎯T557.1 stalled twice.
//
// Only work seats, and only Codex. Nothing here grants danger-full-access.
func CodexWorkSandboxTuning(prov claudia.Provider, purpose, role string) (writableRoots []string, networkAccess bool) {
	if CodexWorkSandbox(prov, purpose, role) == "" {
		return nil, false
	}
	roots := []string{gate.DefaultStoreRoot()}
	return roots, true
}

// CodexWorkSandboxRefusal names why a Codex work seat must not start,
// or "" when it may (🎯T598). A seat that cannot obtain the access its
// mission requires is refused at spawn with a reason, rather than
// started into a first gate it can never pass.
func CodexWorkSandboxRefusal(prov claudia.Provider, purpose, role string) string {
	if CodexWorkSandbox(prov, purpose, role) == "" {
		return ""
	}
	roots, network := CodexWorkSandboxTuning(prov, purpose, role)
	if !network {
		return "codex work seat: loopback access unavailable — journey and UI oracles cannot bind"
	}
	for _, r := range roots {
		if strings.TrimSpace(r) == "" {
			return "codex work seat: gate store root is unknown — bin/gate could not record a verdict"
		}
	}
	return ""
}

func CodexWorkSandbox(prov claudia.Provider, purpose, role string) string {
	if strings.EqualFold(strings.TrimSpace(role), "auditor") {
		return ""
	}
	if prov != claudia.ProviderCodex {
		return ""
	}
	if purpose != "" && purpose != claudia.PurposeWork {
		return ""
	}
	return codexSandboxWorkspaceWrite
}

// CodexWorkGitWrite is whether a Codex work seat is granted its repo's
// git directories as writable roots (🎯T849).
//
// claudia 🎯T112 made that grant opt-in: a workspace-write seat keeps
// Codex's protection of .git unless the spawner asks, so `git commit`
// and `git worktree add` fail with "Operation not permitted" and the
// seat learns it only from the first failure. jevons asked for nothing,
// which is why every codex seat minted here after claudia readmitted
// them could edit files and land none of it — no codex commit reached
// this repo between 2026-09-06 and the fix.
//
// The grant is deliberately an escape hatch in claudia's sandbox:
// .git/hooks and .git/config are code git runs unsandboxed, as the
// operator. The fleet takes it anyway, for the seats it already trusts
// with workspace-write, because a work seat that cannot commit cannot
// produce evidence — and it takes it for exactly those seats, which is
// what ties this to the mode rather than letting it drift.
func CodexWorkGitWrite(prov claudia.Provider, purpose, role string) bool {
	return CodexWorkSandbox(prov, purpose, role) == codexSandboxWorkspaceWrite
}

// codexSandboxWorkspaceWrite is the one mode that carves .git out, and so
// the only one the git grant means anything for (claudia 🎯T109/🎯T112).
const codexSandboxWorkspaceWrite = "workspace-write"

// WorkSessionGoal is the host-owned Session objective for a work mint
// (claudia 🎯T39 / jevons 🎯T510). Asides and the overseer stay empty
// so one Send stays one turn. Prompt wins; otherwise a bound target
// or a standing work instruction.
func WorkSessionGoal(purpose, targetID, prompt string, autoStart bool) string {
	switch strings.TrimSpace(purpose) {
	case claudia.PurposeAside, claudia.PurposeOverseer:
		return ""
	}
	if !autoStart && strings.TrimSpace(targetID) == "" && strings.TrimSpace(prompt) == "" {
		return ""
	}
	if p := strings.TrimSpace(prompt); p != "" {
		return p
	}
	if id := strings.TrimSpace(targetID); id != "" {
		return "Achieve 🎯" + id
	}
	return "Continue the assigned work until it is finished."
}

// ensureRegistered mints or backfills the registry row for a thread without
// spawning a process. Dual-write half of Launch (🎯T114/T148) and hermetic
// surface for 🎯T215 provider=claude Session stitch tests.
//
// Provider is set on mint or backfilled when empty; never forced to Grok on
// resume when a stored provider exists. Materialized stays false until a real
// (or fake-backend) Launch succeeds inside claudia.Registry.
func (f *Claudia) ensureRegistered(t *thread.Thread) error {
	threadPurpose := strings.TrimSpace(t.Purpose)
	purpose := threadPurpose
	if purpose == "" {
		purpose = claudia.PurposeAside // thread path → aside by default
	}
	threadProv := claudia.Provider(strings.TrimSpace(t.Provider))

	// Ensure a registry def. Resume when SessionID is known; otherwise
	// mint a placeholder id and let the provider replace it on session/new.
	if f.reg.Def(t.ID) == nil {
		// 🎯T474: a bare thread.Thread{ID:name} Launch after a concurrent
		// reap deleted the rotated row must recover identity from the
		// pending handover — not invent purpose=aside / fresh uuid.
		if recovered, ok := f.mintFromPendingHandover(t.ID); ok {
			if err := f.reg.Register(recovered); err != nil {
				return fmt.Errorf("register recovered agent %q: %w", t.ID, err)
			}
			if t.SessionID == "" {
				t.SessionID = recovered.SessionID
			}
			slog.Info("agent mint recovered from pending handover",
				"name", t.ID, "purpose", recovered.Purpose,
				"workdir", recovered.WorkDir, "parent", recovered.Parent,
				"target_id", recovered.TargetID, "session", recovered.SessionID)
			return nil
		}
		sid := t.SessionID
		if sid == "" {
			sid = uuid.New().String()
		}
		prov := providerForLaunch("", threadProv, f.defaultProvider)
		// 🎯T324: session-truth model — pin or provider default for this SessionID.
		if err := f.reg.Register(claudia.AgentDef{
			Name:                 t.ID,
			WorkDir:              t.WorkDir,
			Model:                cli.BindSessionModel(t.Model, prov),
			Provider:             prov,
			SessionID:            sid,
			AutoStart:            true,
			Parent:               t.Parent,
			Purpose:              purpose,
			SandboxMode:          CodexWorkSandbox(prov, purpose, ""),
			SandboxWritableRoots: codexRoots(prov, purpose),
			SandboxNetworkAccess: codexNetwork(prov, purpose),
			SandboxGitWrite:      CodexWorkGitWrite(prov, purpose, ""),
			Goal:                 WorkSessionGoal(purpose, "", t.Description, true),
			MCPServers:           f.SessionMCPServers(prov, t.WorkDir),
			MCPExclusive:         mcpattach.Exclusive,
		}); err != nil {
			return fmt.Errorf("register agent %q: %w", t.ID, err)
		}
		if t.SessionID == "" {
			t.SessionID = sid
		}
		return nil
	}

	def := f.reg.Def(t.ID)
	if def == nil {
		return nil
	}
	dirty := false
	// Backfill empty provider only — never overwrite a stored choice.
	if def.Provider == "" {
		def.Provider = providerForLaunch("", threadProv, f.defaultProvider)
		dirty = true
	}
	// 🎯T324: bind provider default when the row has no model pin yet
	// (cold Grok agents must not stay mark-only forever). Explicit pin
	// from the thread wins when supplied.
	if pin := strings.TrimSpace(t.Model); pin != "" && def.Model != pin {
		def.Model = pin
		dirty = true
	} else if strings.TrimSpace(def.Model) == "" {
		if bound := cli.BindSessionModel("", def.Provider); bound != "" {
			def.Model = bound
			dirty = true
		}
	}
	// Backfill empty parent when the spawn path now knows the creator.
	if def.Parent == "" && t.Parent != "" {
		def.Parent = t.Parent
		dirty = true
	}
	// Backfill purpose for legacy dual-write rows (🎯T114) — but only from a
	// thread that actually carries one. The aside default above belongs to
	// the MINT branch, where "no purpose" really does mean a new side chat;
	// applied to an EXISTING row it is a guess written to durable state.
	//
	// jevons_agent_migrate relaunches a rotated agent through a bare
	// thread.Thread{ID: name}, so that guess landed on every row minted
	// before Purpose existed — rewriting a product owner to aside (🎯T301).
	// Observed 2026-08-08: bullseye-po was the only PO in the grok→claude
	// batch whose row had no explicit purpose, and the only one that turned
	// into a 💡 in the fleet tree. Left empty, /api/agents reads it as work,
	// which is what it was.
	if def.Purpose == "" && threadPurpose != "" {
		def.Purpose = threadPurpose
		dirty = true
	}
	if f.mcp.URL != "" {
		want := f.SessionMCPServers(def.Provider, def.WorkDir)
		if !mcpServersEqual(def.MCPServers, want) {
			def.MCPServers = want
			dirty = true
		}
	}
	if !def.MCPExclusive {
		def.MCPExclusive = mcpattach.Exclusive
		dirty = true
	}
	if dirty {
		if err := f.reg.Register(*def); err != nil {
			return fmt.Errorf("update agent %q: %w", t.ID, err)
		}
	}
	return nil
}

// Launch ensures a live, ready process for the thread. If the thread's
// session already exists, claudia resumes it (Claude --resume, Grok
// session/load, Codex app-server thread/resume as of v0.23.0). The
// resume/summary menu is auto-cleared by claudia's readiness handshake
// (T24). It populates t.SessionID with the live process's session so
// the thread can be rehydrated later.
//
// Dual-write (🎯T114): every thread Launch registers or updates the
// agent registry row with Parent + Purpose so threads and agents share
// one id space. Parent lineage (🎯T111.3) is taken from the thread.
// Provider (🎯T148) is set on mint or backfilled when empty; never forced
// to Grok on resume when a stored provider exists.
func (f *Claudia) Launch(t *thread.Thread) error {
	if err := f.ensureRegistered(t); err != nil {
		return err
	}

	// 🎯T709: answer Claude Code's per-workdir trust question before the
	// process reads its config, so the seat cannot be born behind a modal
	// no tmux pane can clear. Never fails the launch — see preflightTrust.
	f.preflightTrust(t.ID)

	// 🎯T426: a rotation replaces the process object while the name, the
	// registry row and the workdir all stay put, so nothing downstream can
	// tell that this is a different conversation. The host is bracketed
	// around the whole launch — told that one is running before the registry
	// carries the new process, and told it is done before the caller seeds
	// the successor, because the seed's own turn end is the first boundary
	// that must be observed.
	defer f.launching(t.ID)()

	ag, err := LaunchRecovering(f.reg, t.ID)
	if err != nil {
		return fmt.Errorf("launch agent %q: %w", t.ID, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), f.readyTimeout)
	defer cancel()
	if err := ag.WaitReady(ctx); err != nil {
		return fmt.Errorf("agent %q not ready: %w", t.ID, err)
	}

	if sid := ag.SessionID(); sid != "" {
		t.SessionID = sid
	}
	return nil
}

// Send delivers a turn to the thread's live process and waits for its
// reply. It requires a live process (call Launch first).
func (f *Claudia) Send(id, text string) (string, error) {
	ag := f.reg.Get(id)
	if ag == nil || !(seatstate.ReadRegistry(f.reg, id).Alive == seatstate.Yes) {
		return "", fmt.Errorf("no live process for thread %q", id)
	}
	if err := f.allowTurn(id); err != nil {
		return "", fmt.Errorf("send %q: %w", id, err)
	}
	f.mu.Lock()
	record := f.recordRequest
	f.mu.Unlock()
	if record != nil {
		if err := record(id, text); err != nil {
			return "", err
		}
	}
	reply, err := f.awaitReply(ag, f.providerOf(id), text)
	if err != nil {
		return "", fmt.Errorf("direct turn to %q: %w", id, err)
	}
	return reply, nil
}

// Alive reports whether a live process currently exists for the thread.
func (f *Claudia) Alive(id string) bool {
	ag := f.reg.Get(id)
	return ag != nil && (seatstate.ReadRegistry(f.reg, id).Alive == seatstate.Yes)
}

// Stop stops the thread's process resumably; the registry retains its
// definition (and session id) so a later Launch rehydrates it.
func (f *Claudia) Stop(id string) {
	f.reg.Stop(id)
	f.drainOnStop(id)
	if f.seats != nil {
		f.seats.FromClaudia(seatstate.SeatReport{Name: id, Alive: false, Known: true}, time.Now())
	}
}

// SetSeats hands the fleet the daemon's one seat-state authority (🎯T766.2).
func (f *Claudia) SetSeats(a *seatstate.Authority) {
	if f != nil {
		f.seats = a
		a.ObserveRegistry(f.reg)
	}
}

// Remove stops the process and drops the registry definition entirely, so
// it won't auto-restart. The underlying Grok session on disk is left
// intact (only jevons's ownership is dropped).
func (f *Claudia) Remove(id string) {
	f.reg.Stop(id)
	if f.seats != nil {
		// A removed seat has no identity left to answer for.
		defer f.seats.Forget(id)
	}
	// Drain while the definition still names a workdir; removals.Remove is
	// about to drop it.
	f.drainOnStop(id)
	// 🎯T435: the drop is accounted for. A thread with no registry def
	// (observe-only) is a normal no-op and not a registry diff, so the
	// chokepoint emits nothing for it.
	if _, err := f.removals.Remove(f.reg, id, fleetlog.Removal{
		Reason: fleetlog.ReasonThreadRemove,
		Detail: "thread removed by name",
	}); err != nil {
		return
	}
}

// drainOnStop empties the shared index of the stopping agent's repo, saving
// what it removed first (🎯T466): an entry left staged in a shared clone is a
// pending contribution to whatever the next worker commits (🎯T457), so no
// stop path may leave one behind.
func (f *Claudia) drainOnStop(id string) {
	if f == nil || f.reg == nil {
		return
	}
	if d := f.reg.Def(id); d != nil {
		attrib.DrainOnStop(d.WorkDir, d.SessionID, id)
	}
}

// Exists reports whether a fleet agent is registered (butler.Participants).
func (f *Claudia) Exists(id string) bool {
	if f == nil || f.reg == nil || id == "" {
		return false
	}
	return f.reg.Def(id) != nil
}

// Deliver rehydrates a registered agent if needed and sends text,
// waiting for a reply (butler.Participants — 🎯T114 / 🎯T111.2).
func (f *Claudia) Deliver(id, text string) (string, error) {
	if f == nil || f.reg == nil {
		return "", fmt.Errorf("no agent registry")
	}
	if f.reg.Def(id) == nil {
		return "", fmt.Errorf("no agent %q", id)
	}
	if err := f.allowTurn(id); err != nil {
		return "", fmt.Errorf("deliver %q: %w", id, err)
	}
	ag := f.reg.Get(id)
	if ag != nil && seatstate.ReadRegistry(f.reg, id).Alive == seatstate.Unknown {
		return "", fmt.Errorf("deliver %q: liveness unknown; awaiting observation", id)
	}
	if ag == nil || seatstate.ReadRegistry(f.reg, id).Alive == seatstate.No {
		// 🎯T426: rehydrate is a launch road too. Ended as soon as the process
		// is ready rather than deferred to the end of this function, because
		// the turn that follows can run for minutes and a launch that is
		// "in flight" for all of it would mute the sweep for all of it.
		endLaunch := f.launching(id)
		launched, err := LaunchReconciled(f.reg, id)
		if err != nil {
			endLaunch()
			return "", fmt.Errorf("could not rehydrate agent %q: %w", id, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), f.readyTimeout)
		defer cancel()
		err = launched.WaitReady(ctx)
		endLaunch()
		if err != nil {
			return "", fmt.Errorf("agent %q not ready: %w", id, err)
		}
		ag = launched
	}
	reply, err := f.awaitReply(ag, f.providerOf(id), text)
	if err != nil {
		return "", fmt.Errorf("deliver turn to agent %q: %w", id, err)
	}
	return reply, nil
}

// codexRoots / codexNetwork are the AgentDef-shaped halves of
// CodexWorkSandboxTuning (🎯T598), so every mint path that sets
// SandboxMode sets the dimensions with it and none can drift.
func codexRoots(prov claudia.Provider, purpose string) []string {
	roots, _ := CodexWorkSandboxTuning(prov, purpose, "")
	return roots
}

func codexNetwork(prov claudia.Provider, purpose string) bool {
	_, network := CodexWorkSandboxTuning(prov, purpose, "")
	return network
}
