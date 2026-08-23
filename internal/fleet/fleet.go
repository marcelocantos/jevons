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

	// inFlight counts turns currently awaiting a reply, per agent id.
	// Idle-derived reaping consults it (Busy) so a worker mid-turn is
	// never stopped out from under the caller — see Busy (🎯T282).
	mu       sync.Mutex
	inFlight map[string]int

	// Provider migration (🎯T285): session roots resolve a predecessor's
	// transcript, and handovers persists the pointer across the rotation
	// that destroys it. Both optional — without them Launch behaves as
	// before and migration is unavailable rather than silently cold.
	roots     discovery.Roots
	handovers *handover.Store
	rotations *handover.RotationStore

	// seedDeliver overrides how a handover seed reaches its successor
	// (🎯T416). Nil is the product path, Deliver. Test seam: the handover
	// dispatcher's fail-closed arm is one of clause 9's two exercised
	// instruments, and asserting on it must not need a live tmux agent.
	seedDeliver func(name, seed string) (string, error)

	// seedTranscript resolves the successor's transcript, which is what
	// decides whether a seed arrived (🎯T416). Nil reads the live claudia
	// process. Test seam for the same reason as seedDeliver: the predicate
	// this arm now uses is a file on the RECEIVER's disk, and a fixture must
	// be able to write that file without launching a provider.
	seedTranscript func(name string) string

	// selfBrief / compactBrief are T285.1 gather hooks. Nil is the
	// product path (try the live outgoing session; throwaway compact
	// on the new provider). Tests inject dead/live/thin fixtures.
	selfBrief    func(p handover.Pending) (string, error)
	compactBrief func(p handover.Pending) (sessionID, text string, err error)

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
}

// NewClaudia wraps a registry as a Fleet. Default provider resolves from
// env / Grok (🎯T148); main should call SetDefaultProvider with the
// config-resolved value.
func NewClaudia(reg *claudia.Registry) *Claudia {
	return &Claudia{
		reg:             reg,
		defaultProvider: cli.ResolveProvider("", ""),
		readyTimeout:    defaultReadyTimeout,
		replyTimeout:    defaultReplyTimeout,
	}
}

// SetRemovalAccount installs the accounted-removal chokepoint (🎯T435). The
// daemon builds one Account for the process and gives every removal path the
// same one, so a row leaving here is explained on the surfaces read elsewhere.
func (f *Claudia) SetRemovalAccount(a *fleetlog.Account) {
	if f == nil {
		return
	}
	f.removals = a
}

// SetLaunchHook installs the per-launch host callback (🎯T426). Nil clears it.
func (f *Claudia) SetLaunchHook(fn func(name string) func()) {
	if f == nil {
		return
	}
	f.onLaunch = fn
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
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].URL != b[i].URL || a[i].Type != b[i].Type {
			return false
		}
	}
	return true
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
	return "workspace-write"
}

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
			Name:         t.ID,
			WorkDir:      t.WorkDir,
			Model:        cli.BindSessionModel(t.Model, prov),
			Provider:     prov,
			SessionID:    sid,
			AutoStart:    true,
			Parent:       t.Parent,
			Purpose:      purpose,
			SandboxMode:  CodexWorkSandbox(prov, purpose, ""),
			Goal:         WorkSessionGoal(purpose, "", t.Description, true),
			MCPServers:   f.SessionMCPServers(prov, t.WorkDir),
			MCPExclusive: mcpattach.Exclusive,
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

	// 🎯T426: a rotation replaces the process object while the name, the
	// registry row and the workdir all stay put, so nothing downstream can
	// tell that this is a different conversation. The host is bracketed
	// around the whole launch — told that one is running before the registry
	// carries the new process, and told it is done before the caller seeds
	// the successor, because the seed's own turn end is the first boundary
	// that must be observed.
	defer f.launching(t.ID)()

	ag, err := f.reg.Launch(t.ID)
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
	if ag == nil || !ag.Alive() {
		return "", fmt.Errorf("no live process for thread %q", id)
	}
	defer f.enterTurn(id)()
	reply, err := f.awaitReply(ag, f.providerOf(id), text)
	if err != nil {
		return "", fmt.Errorf("direct turn to %q: %w", id, err)
	}
	return reply, nil
}

// enterTurn marks a turn in flight for id and returns the function that
// clears it. Turn state is tracked here rather than derived from the
// transcript because the transcript is a lagging, provider-specific
// signal: a Claude worker writes no JSONL until its first turn produces
// output, so a freshly directed worker looks idle to DeriveStatus and
// the process-as-cache sweep would stop it mid-turn (🎯T282).
func (f *Claudia) enterTurn(id string) func() {
	f.mu.Lock()
	if f.inFlight == nil {
		f.inFlight = map[string]int{}
	}
	f.inFlight[id]++
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if n := f.inFlight[id] - 1; n > 0 {
			f.inFlight[id] = n
		} else {
			delete(f.inFlight, id)
		}
	}
}

// Busy reports whether a directed turn is currently awaiting a reply for
// id. The idle sweep uses it to leave working agents alone.
func (f *Claudia) Busy(id string) bool {
	if f == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inFlight[id] > 0
}

// Alive reports whether a live process currently exists for the thread.
func (f *Claudia) Alive(id string) bool {
	ag := f.reg.Get(id)
	return ag != nil && ag.Alive()
}

// Stop stops the thread's process resumably; the registry retains its
// definition (and session id) so a later Launch rehydrates it.
func (f *Claudia) Stop(id string) {
	f.reg.Stop(id)
	f.drainOnStop(id)
}

// Remove stops the process and drops the registry definition entirely, so
// it won't auto-restart. The underlying Grok session on disk is left
// intact (only jevons's ownership is dropped).
func (f *Claudia) Remove(id string) {
	f.reg.Stop(id)
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
	// Count the turn from before the rehydrate: a launch + first turn is
	// exactly the window in which the idle sweep must not intervene.
	defer f.enterTurn(id)()
	ag := f.reg.Get(id)
	if ag == nil || !ag.Alive() {
		// 🎯T426: rehydrate is a launch road too. Ended as soon as the process
		// is ready rather than deferred to the end of this function, because
		// the turn that follows can run for minutes and a launch that is
		// "in flight" for all of it would mute the sweep for all of it.
		endLaunch := f.launching(id)
		launched, err := f.reg.Launch(id)
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
