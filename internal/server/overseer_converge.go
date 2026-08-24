// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
)

// Cockpit convergence (🎯T204): desired state is a *usable* owner chat and
// a live fleet — not process liveness alone.
//
// Dimensions:
//  1. Overseer Alive + AttachOverseer (chat stream wired)
//  2. Overseer turn-usable: not stuck-busy (prompt in flight / waiting
//     with no ACP progress beyond StuckBusyTimeout)
//  3. Fleet dead-handle recovery (hook → SweepDeadAgents)
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
)

// cockpitPhase is the pure next action for one observation.
type cockpitPhase int

const (
	cockpitOK cockpitPhase = iota
	cockpitAttach
	cockpitUnstickBusy
	cockpitLaunch
	cockpitGiveUp
)

// cockpitObs is a snapshot of overseer + chat attach + busy state.
type cockpitObs struct {
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
	if o.SinceProgress >= stuckTimeout && (o.PromptInFlight || o.Waiting || o.QueueDepth > 0) {
		return cockpitUnstickBusy
	}
	return cockpitOK
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
	// maxUnstickPerHour soft-cap before escalate-to-relaunch only.
	maxUnstickBurst int
}

// CockpitHooks are optional fleet actuators registered from main so
// package server does not import mcpserver (🎯T204 fleet dimensions).
type CockpitHooks struct {
	// FleetHealth rehydrates/clears dead worker handles (SweepDeadAgents).
	FleetHealth func()
	// FleetNudge runs one idle-nudge sweep (T207 policy, not post-restart).
	FleetNudge func()
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
	s.overseerLastProgress = time.Now()
}

// ObserveCockpit reads registry + chat attach + busy for the overseer.
func (s *Server) ObserveCockpit() cockpitObs {
	s.mu.RLock()
	reg := s.registry
	name := s.overseerName
	chat := s.proc
	waiting := s.waiting
	lastProg := s.overseerLastProgress
	qdepth := len(s.notifyQueue)
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
	if proc != nil && proc.Alive() {
		o.ProcAlive = true
		o.PromptInFlight = proc.PromptInFlight()
	}
	if chat != nil && chat.Alive() && proc != nil && chat == proc {
		o.ChatAttached = true
	} else if chat != nil && chat.Alive() && o.ProcAlive {
		o.ChatAttached = false
	}
	if !lastProg.IsZero() {
		o.SinceProgress = time.Since(lastProg)
	} else if o.PromptInFlight || waiting || qdepth > 0 {
		// Never seen progress while something claims work — treat as aged.
		o.SinceProgress = DefaultStuckBusyTimeout + time.Second
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
	state.mu.Lock()
	attempts := state.attempts
	state.mu.Unlock()

	phase := planCockpit(obs, attempts, DefaultCockpitMaxAttempts, DefaultStuckBusyTimeout)
	state.mu.Lock()
	state.lastPhase = phase
	state.mu.Unlock()

	switch phase {
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
		state.mu.Unlock()
		if !obs.Registered {
			reason = "overseer is not registered in the agent registry"
		} else if obs.ResumeDenied {
			reason = "overseer session exists but could not be loaded; refusing to mint a replacement"
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
	if proc == nil || !proc.Alive() {
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

	slog.Warn("cockpit: stuck-busy detected; recovering",
		"name", name,
		"since_progress", obs.SinceProgress.String(),
		"prompt_in_flight", obs.PromptInFlight,
		"waiting", obs.Waiting,
		"queue_depth", obs.QueueDepth,
		"attempt", n,
		"escalate", escalate,
	)

	if proc != nil && proc.Alive() && !escalate {
		if err := proc.Interrupt(); err != nil {
			slog.Warn("cockpit: interrupt failed", "err", err)
		}
		// Settle server + clients even if interrupt is racy.
		s.mu.Lock()
		s.waiting = false
		s.overseerOwnerTurn = false // 🎯T291
		s.turnBuf = ""
		s.overseerLastProgress = time.Now()
		s.mu.Unlock()
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
	s.broadcastCockpitReady("overseer is back")
	s.drainOverseerNotes()
	return nil
}

// broadcastCockpitReady tells clients to clear degraded + working chrome
// and accept sends again (🎯T204 / T94 client half).
func (s *Server) broadcastCockpitReady(text string) {
	if text == "" {
		text = "overseer is back"
	}
	// Wire shape used by web: type=status text=… (overseer is back regex)
	// and state=idle for thinking indicator.
	payload, err := json.Marshal(map[string]string{"type": "status", "text": text})
	if err == nil {
		s.BroadcastChat(string(payload))
	}
	s.Broadcast(map[string]any{"type": "status", "state": "idle", "text": text})
	s.NoteOverseerProgress()
	// 🎯T355: recovery chrome is idle chrome. The owner's unanswered turn is
	// deliberately NOT given a residual here — a relaunch mid-turn is exactly
	// the case the requeue actuator exists to re-inject.
	s.noteChromePublished(false)
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
		state.attempts++
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
	s.overseerLastProgress = time.Now()
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
	if h.FleetHealth != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("cockpit: fleet health panic", "recover", r)
				}
			}()
			h.FleetHealth()
		}()
	}
	if h.FleetNudge != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("cockpit: fleet nudge panic", "recover", r)
				}
			}()
			h.FleetNudge()
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
