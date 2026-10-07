// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/ownerquestions"
	"github.com/marcelocantos/jevons/internal/seatstate"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/upgrade"
)

// Cockpit convergence (🎯T204): desired state is a *usable* owner chat and
// a live fleet — not process liveness alone.
//
// Dimensions:
//  1. Overseer Alive + AttachOverseer (chat stream wired)
//  2. Overseer turn-usable: not stuck-busy (prompt in flight / waiting
//     with no ACP progress beyond StuckBusyTimeout)
//  3. Fleet dead-handle recovery (hook → recoverDeadHandles)
//  4. Idle mission-worker nudge (hook → T207 SweepIdleNudges)
//
// Boot StartAll and passive waitForOverseer are not enough.

const (
	// DefaultCockpitInterval is how often the reconciler re-observes.
	DefaultCockpitInterval = 3 * time.Second
	// DefaultCockpitMaxAttempts caps Launch attempts per down-streak
	// before permanent degraded (reset when back to desired).
	DefaultCockpitMaxAttempts = 8
	// DefaultStuckBusyTimeout: no ACP progress while prompt in flight
	// or server waiting → interrupt + settle. Long healthy turns with
	// tool/stream events keep lastEvent fresh and are not unstuck.
	DefaultStuckBusyTimeout = 90 * time.Second
	// DefaultFleetHookEvery runs fleet health+nudge hooks every N ticks
	// (3s * 10 = 30s) so idle workers are pressured without a separate
	// 1m-only loop owning the product path.
	DefaultFleetHookEvery = 10
	// DefaultOverseerPageAfter is how long the overseer may be down before
	// the owner is paged out of band, once per outage (🎯T775).
	DefaultOverseerPageAfter = 5 * time.Minute
	// DefaultCockpitRearmAfter is how long a launch-exhausted give-up waits
	// before the streak re-arms: the cause (load, a store holder) is
	// presumed to have moved (🎯T775).
	DefaultCockpitRearmAfter = 5 * time.Minute
)

// cockpitPhase is the pure next action for one observation.
type cockpitPhase int

const (
	cockpitOK cockpitPhase = iota
	cockpitAttach
	cockpitUnstickBusy
	cockpitLaunch
	cockpitGiveUp
	cockpitWaitObservation
)

// cockpitObs is a snapshot of overseer + chat attach + busy state.
type cockpitObs struct {
	Unknown      bool // a present handle has no fresh condition observation
	Registered   bool
	ProcAlive    bool
	ChatAttached bool
	// PromptInFlight from claudia (Grok ACP); false when unknown.
	PromptInFlight bool
	// Waiting is the server-side owner-turn flag (HandleUserMessage path
	// / notify delivery that set waiting).
	Waiting bool
	// SinceProgress is time since last overseer ACP event or successful
	// send; 0 when never.
	SinceProgress time.Duration
	// QueueDepth is pending notify/owner notes not yet delivered.
	QueueDepth int
	// PaneWorking is the pane's own account of itself: a frame showing a
	// turn in progress (🎯T601). Events arriving is not the only evidence
	// of work — a long tool call or plain model thinking emits none — so
	// silence alone must not convict. False when the pane cannot be read,
	// which keeps a genuinely dead session convictable.
	PaneWorking bool
	// ResumeDenied is a latched Cursor session/load fail-closed
	// (🎯T541.1). Further Launch attempts would stack store.db writers.
	ResumeDenied bool
}

// planCockpit is the hermetic policy (oracle for 🎯T204).
// attempts counts failed Launch tries in the current down-streak.
// stuckTimeout 0 → DefaultStuckBusyTimeout.
func planCockpit(o cockpitObs, attempts, maxAttempts int, stuckTimeout time.Duration) cockpitPhase {
	if maxAttempts < 1 {
		maxAttempts = DefaultCockpitMaxAttempts
	}
	if stuckTimeout <= 0 {
		stuckTimeout = DefaultStuckBusyTimeout
	}
	if o.Unknown {
		return cockpitWaitObservation
	}
	if !o.Registered {
		return cockpitGiveUp
	}
	if o.ResumeDenied {
		return cockpitGiveUp
	}
	if !o.ProcAlive {
		if attempts >= maxAttempts {
			return cockpitGiveUp
		}
		return cockpitLaunch
	}
	if !o.ChatAttached {
		return cockpitAttach
	}
	// Turn-usable: stuck busy with no progress → unstick before declaring OK.
	//
	// 🎯T601: "no events for a while" is not "no work". The overseer was
	// declared stuck at since_progress=7m14s while its pane read
	// "Calling jevonsmcp… ✽ Orbiting… esc to interrupt", and the
	// recovery that followed interrupted real work. A pane that says it
	// is working is progress; only silence the pane does not contradict
	// convicts.
	if o.SinceProgress >= stuckTimeout && !o.PaneWorking &&
		(o.PromptInFlight || o.Waiting || o.QueueDepth > 0) {
		return cockpitUnstickBusy
	}
	return cockpitOK
}

// cockpitShouldRearm is the pure re-arm policy (🎯T775): a give-up is
// never terminal. A resume-denied latch re-arms the moment it clears; a
// launch-exhausted streak re-arms after rearmAfter.
func cockpitShouldRearm(obs cockpitObs, attempts, maxAttempts int, gaveUpAt time.Time, gaveUpResumeDenied bool, now time.Time, rearmAfter time.Duration) bool {
	if maxAttempts < 1 {
		maxAttempts = DefaultCockpitMaxAttempts
	}
	if attempts < maxAttempts || obs.ResumeDenied || !obs.Registered {
		return false
	}
	if gaveUpResumeDenied {
		return true // the latch has cleared
	}
	return !gaveUpAt.IsZero() && now.Sub(gaveUpAt) >= rearmAfter
}

// overseerPageDecision is the pure paging policy (🎯T775): page once per
// outage after the threshold; send one recovery when it ends.
type overseerPageAction int

const (
	pageNone overseerPageAction = iota
	pageDown
	pageRecovered
)

func planOverseerPage(reason string, downSince time.Time, paged bool, now time.Time, after time.Duration) overseerPageAction {
	if reason == "" {
		if paged {
			return pageRecovered
		}
		return pageNone
	}
	if !paged && !downSince.IsZero() && now.Sub(downSince) >= after {
		return pageDown
	}
	return pageNone
}

// SetOverseerPager overrides the out-of-band pager (tests; default blurter).
func (s *Server) SetOverseerPager(f func(subject, body, key string, recovered bool)) {
	s.mu.Lock()
	s.overseerPager = f
	s.mu.Unlock()
}

// reconcileOverseerPage pages the owner out of band when the overseer has
// been down past the threshold (🎯T775). Independent of any agent.
func (s *Server) reconcileOverseerPage(now time.Time) {
	s.mu.Lock()
	reason, since, paged, pager := s.overseerDownReason, s.overseerDownSince, s.overseerPaged, s.overseerPager
	act := planOverseerPage(reason, since, paged, now, DefaultOverseerPageAfter)
	switch act {
	case pageDown:
		s.overseerPaged = true
	case pageRecovered:
		s.overseerPaged = false
		s.overseerDownSince = time.Time{}
	}
	if reason == "" {
		s.overseerDownSince = time.Time{}
	}
	s.mu.Unlock()
	if pager == nil {
		pager = blurterPage
	}
	switch act {
	case pageDown:
		pager("jevons: overseer down for "+now.Sub(since).Round(time.Minute).String(), reason, "jevons-overseer-down", false)
	case pageRecovered:
		pager("jevons: overseer is back", "the overseer recovered", "jevons-overseer-down", true)
	}
}

func blurterPage(subject, body, key string, recovered bool) {
	sev := "problem"
	if recovered {
		sev = "ok"
	}
	if err := exec.Command("blurter", "send", "--app", "jevons", "--severity", sev,
		"--subject", subject, "--body", body, "--key", key).Run(); err != nil {
		slog.Warn("overseer page: blurter send failed", "err", err)
	}
}

// clearConnectEndpoint zeros durable serve fields on a def copy so Launch
// cannot reattach to a killed endpoint after intentional stop/rotate.
func clearConnectEndpoint(def claudia.AgentDef) claudia.AgentDef {
	def.ConnectURL = ""
	def.ConnectPID = 0
	return def
}

// cockpitState tracks per-streak Launch failures and unstick attempts.
type cockpitState struct {
	mu           sync.Mutex
	attempts     int
	lastErr      string
	lastPhase    cockpitPhase
	unstickCount int
	tick         int
	lastUnstick  time.Time
	// gaveUpAt / gaveUpResumeDenied record the current give-up (🎯T775) so
	// it can re-arm when its cause clears.
	gaveUpAt           time.Time
	gaveUpResumeDenied bool
	// maxUnstickPerHour soft-cap before escalate-to-relaunch only.
	maxUnstickBurst int
}

// CockpitHooks are optional fleet actuators registered from main so
// package server does not import mcpserver (🎯T204 fleet dimensions).
type CockpitHooks struct {
	// FleetHealth rehydrates/clears dead worker handles (recoverDeadHandles).
	// Reconcile runs the one fleet pass (🎯T766.3) on the cockpit tick.
	Reconcile func()
}

// SetCockpitHooks registers fleet health/nudge actuators for the converge loop.
func (s *Server) SetCockpitHooks(h CockpitHooks) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cockpitHooks = h
}

// NoteOverseerProgress records ACP/activity for stuck-busy detection.
// Safe from any goroutine.
func (s *Server) NoteOverseerProgress() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noteOverseerProgressLocked()
}

// ObserveCockpit reads registry + chat attach + busy for the overseer.
func (s *Server) ObserveCockpit() cockpitObs {
	s.mu.RLock()
	reg := s.registry
	name := s.overseerName
	chat := s.proc
	waiting := s.waiting
	lastProg := s.seatState(name).LastActivity
	qdepth := s.seatState(s.overseerName).OwnerQueueDepth
	s.mu.RUnlock()

	o := cockpitObs{Waiting: waiting, QueueDepth: qdepth}
	if reg == nil || name == "" {
		return o
	}
	if reg.Def(name) == nil {
		return o
	}
	o.Registered = true
	if err := reg.ResumeDenied(name); err != nil {
		o.ResumeDenied = true
	}
	proc := reg.Get(name)
	st := s.seatState(name)
	o.Unknown = proc != nil && (!st.Alive.Known() || !st.InFlight.Known())
	if proc != nil && (seatstate.ReadRegistry(reg, name).Alive == seatstate.Yes) {
		o.ProcAlive = true
		o.PromptInFlight = s.seatInFlight(name, proc)
		// 🎯T601: ask the pane, not only the event stream — but only
		// where the answer IS the pane. For a tmux Claude session
		// PromptInFlight is now read from the frame (claudia
		// claudeAgentOps), so a long tool call or plain thinking still
		// reports work with no ACP event in minutes.
		//
		// Not for ACP providers. There PromptInFlight means "the client
		// says a prompt is outstanding", which is exactly the condition a
		// wedge also satisfies; treating it as evidence of work would
		// make the stuck-busy case 🎯T204 exists for unconvictable.
		if st.Provider == string(claudia.ProviderClaude) {
			o.PaneWorking = o.PromptInFlight
		}
	}
	if chat != nil && (seatstate.ReadRegistry(reg, name).Alive == seatstate.Yes) && proc != nil && chat == proc {
		o.ChatAttached = true
	} else if chat != nil && (seatstate.ReadRegistry(reg, name).Alive == seatstate.Yes) && o.ProcAlive {
		o.ChatAttached = false
	}
	if !lastProg.IsZero() {
		o.SinceProgress = time.Since(lastProg)
	}
	return o
}

// EnsureOverseer runs one reconcile step (liveness + turn-usable).
// Safe from any goroutine.
func (s *Server) EnsureOverseer(state *cockpitState) error {
	if state == nil {
		state = &cockpitState{}
	}
	obs := s.ObserveCockpit()
	now := time.Now()
	state.mu.Lock()
	if cockpitShouldRearm(obs, state.attempts, DefaultCockpitMaxAttempts, state.gaveUpAt, state.gaveUpResumeDenied, now, DefaultCockpitRearmAfter) {
		slog.Info("cockpit: give-up re-armed; cause cleared", "resume_denied_cleared", state.gaveUpResumeDenied)
		state.attempts = 0
		state.gaveUpAt = time.Time{}
		state.gaveUpResumeDenied = false
	}
	attempts := state.attempts
	state.mu.Unlock()
	defer s.reconcileOverseerPage(now)
	defer s.reconcileOwnerQuestionDigest(now)

	phase := planCockpit(obs, attempts, DefaultCockpitMaxAttempts, s.stuckBusyTimeout())
	state.mu.Lock()
	state.lastPhase = phase
	state.mu.Unlock()

	switch phase {
	case cockpitWaitObservation:
		return nil
	case cockpitOK:
		state.mu.Lock()
		state.attempts = 0
		state.lastErr = ""
		state.mu.Unlock()
		s.SetOverseerDownReason("")
		return nil
	case cockpitAttach:
		return s.cockpitAttach(state)
	case cockpitUnstickBusy:
		return s.cockpitUnstickBusy(state, obs)
	case cockpitGiveUp:
		reason := "overseer recovery gave up after repeated launch failures"
		state.mu.Lock()
		if state.lastErr != "" {
			reason = reason + ": " + state.lastErr
		}
		if state.gaveUpAt.IsZero() {
			state.gaveUpAt = now
		}
		state.gaveUpResumeDenied = obs.ResumeDenied
		retryAt := state.gaveUpAt.Add(DefaultCockpitRearmAfter)
		state.mu.Unlock()
		reason += "; retrying automatically at " + retryAt.Format("15:04")
		if !obs.Registered {
			reason = "overseer is not registered in the agent registry"
		} else if obs.ResumeDenied {
			reason = "overseer session exists but could not be loaded; refusing to mint a replacement (clears when the session loads again)"
			s.mu.RLock()
			reg := s.registry
			name := s.overseerName
			s.mu.RUnlock()
			if reg != nil {
				if err := reg.ResumeDenied(name); err != nil {
					reason = reason + ": " + err.Error()
				}
			}
		}
		s.SetOverseerDownReason(reason)
		return fmt.Errorf("cockpit: %s", reason)
	case cockpitLaunch:
		return s.cockpitLaunch(state)
	default:
		return fmt.Errorf("cockpit: unknown phase %d", phase)
	}
}

func (s *Server) cockpitAttach(state *cockpitState) error {
	s.mu.RLock()
	reg := s.registry
	name := s.overseerName
	s.mu.RUnlock()
	if reg == nil {
		return fmt.Errorf("cockpit: no registry")
	}
	proc := reg.Get(name)
	if proc == nil || !(seatstate.ReadRegistry(reg, name).Alive == seatstate.Yes) {
		return fmt.Errorf("cockpit: attach raced; process gone")
	}
	s.AttachOverseer(proc)
	s.broadcastCockpitReady("overseer is back")
	// A migration that rotated the row but never got its seed out is only
	// visible here (🎯T285); no-op otherwise.
	s.ResumePendingHandover()
	slog.Info("cockpit: overseer re-attached to chat", "name", name)
	s.SetOverseerDownReason("")
	state.mu.Lock()
	state.attempts = 0
	state.lastErr = ""
	state.mu.Unlock()
	s.NotifyAgentsChanged()
	return nil
}

func (s *Server) cockpitUnstickBusy(state *cockpitState, obs cockpitObs) error {
	s.mu.RLock()
	name := s.overseerName
	proc := s.proc
	s.mu.RUnlock()

	state.mu.Lock()
	state.unstickCount++
	n := state.unstickCount
	// Escalate to relaunch after repeated unsticks in a short window.
	escalate := n >= 3
	state.mu.Unlock()

	s.markOverseerStuck() // 🎯T555.1 jevons-minted stuck frame
	slog.Warn("cockpit: stuck-busy detected; recovering",
		"name", name,
		"since_progress", obs.SinceProgress.String(),
		"prompt_in_flight", obs.PromptInFlight,
		"waiting", obs.Waiting,
		"queue_depth", obs.QueueDepth,
		"attempt", n,
		"escalate", escalate,
	)

	if proc != nil && s.seatState(name).Alive == seatstate.Yes && !escalate {
		if err := proc.Interrupt(); err != nil {
			slog.Warn("cockpit: interrupt failed", "err", err)
		}
		// Settle server + clients even if interrupt is racy. The strip
		// was just set to stuck; leave it there and the queued follow-up
		// never sends, because busy stays true.
		s.settleOverseerAfterUnstick()
		s.broadcastCockpitReady("overseer is back")
		// Flush deferred owner/notify notes now that local busy is cleared.
		s.drainOverseerNotes()
		s.SetOverseerDownReason("")
		return nil
	}

	// Escalate: clean relaunch (same as Launch path).
	state.mu.Lock()
	state.unstickCount = 0
	state.mu.Unlock()
	s.SetOverseerDownReason("overseer stuck-busy; relaunching")
	if err := s.cockpitLaunch(state); err != nil {
		return err
	}
	s.settleOverseerAfterUnstick()
	s.broadcastCockpitReady("overseer is back")
	s.drainOverseerNotes()
	return nil
}

// stuckBusyTimeout is DefaultStuckBusyTimeout stretched by host load
// (🎯T567): a seat that is slow because the box is at 3×/core is not
// stuck, and interrupting it only to re-attach is what spammed the
// owner with "overseer is back".
func (s *Server) stuckBusyTimeout() time.Duration {
	s.mu.RLock()
	f := s.hostLoad
	s.mu.RUnlock()
	if f == nil {
		return DefaultStuckBusyTimeout
	}
	load1, cores := f()
	return time.Duration(float64(DefaultStuckBusyTimeout) * capacity.StuckBusyScale(load1, cores))
}

// closeOverseerOutage reports whether an outage was open and closes it
// (🎯T567). The caller emits recovery chrome only on true.
func (s *Server) closeOverseerOutage() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	open := s.overseerOutageOpen
	s.overseerOutageOpen = false
	return open
}

// broadcastCockpitReady tells clients to clear degraded + working chrome
// and accept sends again (🎯T204 / T94 client half).
//
// 🎯T567: the "overseer is back" line answers an outage, not a reconcile
// tick. A re-attach with no degraded/down/stuck state broadcast since
// the last recovery emits nothing — across a daemon bounce the converge
// loop re-attaches on every tick and each one used to print a line.
func (s *Server) broadcastCockpitReady(text string) {
	if text == "" {
		text = "overseer is back"
	}
	if s.closeOverseerOutage() {
		// Wire shape used by web: type=status text=… (overseer is back regex)
		// and state=idle for thinking indicator. Live-only — not a journaled
		// turn (🎯T555.5 / T355).
		payload, err := json.Marshal(map[string]string{"type": "status", "text": text})
		if err == nil {
			s.broadcastChatLive(stampConversationName(string(payload), s.overseerAgentName()))
		}
		s.Broadcast(map[string]any{"type": "status", "state": "idle", "text": text})
	}
	s.NoteOverseerProgress()
	// 🎯T355: recovery chrome is idle chrome. The owner's unanswered turn is
	// deliberately NOT given a residual here — a relaunch mid-turn is exactly
	// the case the requeue actuator exists to re-inject.
	s.noteChromePublished(false)
	s.muxFanOverseerLevel()
}

func (s *Server) cockpitLaunch(state *cockpitState) error {
	s.mu.RLock()
	reg := s.registry
	name := s.overseerName
	s.mu.RUnlock()
	if reg == nil {
		return fmt.Errorf("cockpit: no registry")
	}
	def := reg.Def(name)
	if def == nil {
		return fmt.Errorf("cockpit: overseer %q not registered", name)
	}

	// Prefer a clean Launch: clear durable connect endpoints so we do not
	// reattach to a serve killed by Stop/rewind.
	cleared := clearConnectEndpoint(*def)
	if cleared.ConnectURL != def.ConnectURL || cleared.ConnectPID != def.ConnectPID {
		if err := reg.Register(cleared); err != nil {
			return fmt.Errorf("cockpit: clear connect endpoint: %w", err)
		}
	}

	agent, err := fleet.LaunchRecovering(reg, name)
	if err != nil && !claudia.IsCursorResumeDenied(err) {
		_ = reg.Register(clearConnectEndpoint(cleared))
		agent, err = fleet.LaunchRecovering(reg, name)
	}
	if err != nil {
		state.mu.Lock()
		// A Launch refused only because the holder probe could not answer is
		// the host being slow, not the overseer failing to start. Counting it
		// spends the streak's attempts in 24s of load and lands in the same
		// permanent degraded the unlatched error exists to avoid.
		if !errors.Is(err, upgrade.ErrCursorStoreUnconfirmed) {
			state.attempts++
		}
		if claudia.IsCursorResumeDenied(err) {
			state.attempts = DefaultCockpitMaxAttempts
		}
		state.lastErr = err.Error()
		n := state.attempts
		state.mu.Unlock()
		reason := fmt.Sprintf("overseer launch failed (attempt %d/%d): %v", n, DefaultCockpitMaxAttempts, err)
		s.SetOverseerDownReason(reason)
		slog.Warn("cockpit: overseer launch failed", "name", name, "attempt", n, "err", err)
		return fmt.Errorf("cockpit: %w", err)
	}
	s.AttachOverseer(agent)
	s.mu.Lock()
	s.waiting = false
	s.overseerOwnerTurn = false // 🎯T291
	s.turnBuf = ""
	s.noteOverseerProgressLocked()
	s.mu.Unlock()
	s.SetOverseerDownReason("")
	state.mu.Lock()
	state.attempts = 0
	state.lastErr = ""
	state.mu.Unlock()
	slog.Info("cockpit: overseer launched and attached", "name", name, "session", agent.SessionID())
	// This is the retry a failed migration relaunch was waiting for (🎯T285).
	s.ResumePendingHandover()
	s.NotifyAgentsChanged()
	s.broadcastCockpitReady("overseer is back")
	return nil
}

// runFleetHooks invokes dead-agent recovery and idle nudge when due.
func (s *Server) runFleetHooks(state *cockpitState) {
	state.mu.Lock()
	state.tick++
	t := state.tick
	state.mu.Unlock()
	if t%DefaultFleetHookEvery != 0 {
		return
	}
	s.mu.RLock()
	h := s.cockpitHooks
	s.mu.RUnlock()
	if h.Reconcile != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("cockpit: reconcile panic", "recover", r)
				}
			}()
			h.Reconcile()
		}()
	}
}

// StartCockpitConverge runs EnsureOverseer + fleet hooks until ctx is done.
func (s *Server) StartCockpitConverge(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultCockpitInterval
	}
	state := &cockpitState{}
	go func() {
		if err := s.EnsureOverseer(state); err != nil {
			slog.Debug("cockpit: initial ensure", "err", err)
		}
		s.runFleetHooks(state)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				// 🎯T355: owner-interaction health rides the same tick as
				// process health — a live overseer is not a usable chat.
				s.ReconcileOwnerHealth(time.Now())
				if err := s.EnsureOverseer(state); err != nil {
					state.mu.Lock()
					phase := state.lastPhase
					state.mu.Unlock()
					if phase == cockpitLaunch || phase == cockpitGiveUp || phase == cockpitUnstickBusy {
						slog.Debug("cockpit: ensure tick", "err", err)
					}
				}
				s.runFleetHooks(state)
			}
		}
	}()
	slog.Info("cockpit: converge loop started",
		"interval", interval.String(),
		"stuck_busy", DefaultStuckBusyTimeout.String(),
	)
}

// reconcileOwnerQuestionDigest is a bounded standing reminder, independent of
// overseer liveness. A successful blurter invocation acknowledges only spool.
func (s *Server) reconcileOwnerQuestionDigest(now time.Time) {
	s.mu.Lock()
	if !s.ownerQuestionDigestCheck.IsZero() && now.Sub(s.ownerQuestionDigestCheck) < time.Minute {
		s.mu.Unlock()
		return
	}
	s.ownerQuestionDigestCheck = now
	dir := s.stateDir
	s.mu.Unlock()
	if dir == "" {
		return
	}
	store := ownerquestions.New(dir)
	if err := store.Remind(now); err != nil {
		slog.Warn("owner questions: digest spool failed", "err", err)
	}
}
