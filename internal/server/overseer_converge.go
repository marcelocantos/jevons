// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
)

// Cockpit convergence (🎯T204): desired state is overseer Alive and
// chat-attached. Boot StartAll and passive waitForOverseer are not enough;
// mid-session stop/rewind/launch failure must re-enter this loop.

const (
	// DefaultCockpitInterval is how often the reconciler re-observes.
	DefaultCockpitInterval = 3 * time.Second
	// DefaultCockpitMaxAttempts caps Launch attempts per down-streak
	// before permanent degraded (reset when back to desired).
	DefaultCockpitMaxAttempts = 8
)

// cockpitPhase is the pure next action for one observation.
type cockpitPhase int

const (
	cockpitOK cockpitPhase = iota
	cockpitAttach
	cockpitLaunch
	cockpitGiveUp
)

// cockpitObs is a snapshot of overseer + chat attach state.
type cockpitObs struct {
	Registered   bool
	ProcAlive    bool
	ChatAttached bool
}

// planCockpit is the hermetic policy (oracle for 🎯T204).
// attempts counts failed Launch tries in the current down-streak.
func planCockpit(o cockpitObs, attempts, maxAttempts int) cockpitPhase {
	if maxAttempts < 1 {
		maxAttempts = DefaultCockpitMaxAttempts
	}
	if !o.Registered {
		return cockpitGiveUp
	}
	if o.ProcAlive && o.ChatAttached {
		return cockpitOK
	}
	if o.ProcAlive {
		return cockpitAttach
	}
	if attempts >= maxAttempts {
		return cockpitGiveUp
	}
	return cockpitLaunch
}

// clearConnectEndpoint zeros durable serve fields on a def copy so Launch
// cannot reattach to a killed endpoint after intentional stop/rotate.
func clearConnectEndpoint(def claudia.AgentDef) claudia.AgentDef {
	def.ConnectURL = ""
	def.ConnectPID = 0
	return def
}

// cockpitState tracks per-streak Launch failures for the running reconciler.
type cockpitState struct {
	mu       sync.Mutex
	attempts int
	lastErr  string
	// lastPhase for tests / diagnostics.
	lastPhase cockpitPhase
}

// ObserveCockpit reads registry + chat attach for the configured overseer.
func (s *Server) ObserveCockpit() cockpitObs {
	s.mu.RLock()
	reg := s.registry
	name := s.overseerName
	chat := s.proc
	s.mu.RUnlock()

	o := cockpitObs{}
	if reg == nil || name == "" {
		return o
	}
	if reg.Def(name) == nil {
		return o
	}
	o.Registered = true
	proc := reg.Get(name)
	if proc != nil && proc.Alive() {
		o.ProcAlive = true
	}
	if chat != nil && chat.Alive() && proc != nil && chat == proc {
		o.ChatAttached = true
	} else if chat != nil && chat.Alive() && o.ProcAlive {
		// Same liveness, pointer may differ after re-Launch if attach
		// used registry Get — require attach to the registry handle.
		o.ChatAttached = false
	}
	return o
}

// EnsureOverseer runs one reconcile step toward Alive+AttachOverseer.
// Safe from any goroutine. Returns nil when desired state holds or attach
// succeeded; returns an error on launch/give-up (caller may retry later).
func (s *Server) EnsureOverseer(state *cockpitState) error {
	if state == nil {
		state = &cockpitState{}
	}
	obs := s.ObserveCockpit()
	state.mu.Lock()
	attempts := state.attempts
	state.mu.Unlock()

	phase := planCockpit(obs, attempts, DefaultCockpitMaxAttempts)
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
		slog.Info("cockpit: overseer re-attached to chat", "name", name)
		s.SetOverseerDownReason("")
		state.mu.Lock()
		state.attempts = 0
		state.lastErr = ""
		state.mu.Unlock()
		s.NotifyAgentsChanged()
		return nil
	case cockpitGiveUp:
		reason := "overseer recovery gave up after repeated launch failures"
		state.mu.Lock()
		if state.lastErr != "" {
			reason = reason + ": " + state.lastErr
		}
		state.mu.Unlock()
		if !obs.Registered {
			reason = "overseer is not registered in the agent registry"
		}
		s.SetOverseerDownReason(reason)
		return fmt.Errorf("cockpit: %s", reason)
	case cockpitLaunch:
		return s.cockpitLaunch(state)
	default:
		return fmt.Errorf("cockpit: unknown phase %d", phase)
	}
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
	// reattach to a serve killed by Stop/rewind. Connect-mode still spawns
	// a new serve when ConnectURL is empty (CLAUDIA_GROK_CONNECT).
	cleared := clearConnectEndpoint(*def)
	if cleared.ConnectURL != def.ConnectURL || cleared.ConnectPID != def.ConnectPID {
		if err := reg.Register(cleared); err != nil {
			return fmt.Errorf("cockpit: clear connect endpoint: %w", err)
		}
	}

	agent, err := reg.Launch(name)
	if err != nil {
		// One more try after force-clear (def may have been re-written).
		_ = reg.Register(clearConnectEndpoint(cleared))
		agent, err = reg.Launch(name)
	}
	if err != nil {
		state.mu.Lock()
		state.attempts++
		state.lastErr = err.Error()
		n := state.attempts
		state.mu.Unlock()
		reason := fmt.Sprintf("overseer launch failed (attempt %d/%d): %v", n, DefaultCockpitMaxAttempts, err)
		s.SetOverseerDownReason(reason)
		slog.Warn("cockpit: overseer launch failed", "name", name, "attempt", n, "err", err)
		return fmt.Errorf("cockpit: %w", err)
	}
	s.AttachOverseer(agent)
	s.SetOverseerDownReason("")
	state.mu.Lock()
	state.attempts = 0
	state.lastErr = ""
	state.mu.Unlock()
	slog.Info("cockpit: overseer launched and attached", "name", name, "session", agent.SessionID())
	s.NotifyAgentsChanged()
	return nil
}

// StartCockpitConverge runs EnsureOverseer on interval until ctx is done.
// Call once after SetRegistry (and ideally after first boot StartAll).
func (s *Server) StartCockpitConverge(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultCockpitInterval
	}
	state := &cockpitState{}
	go func() {
		// Immediate pass: boot may have failed StartAll for the overseer.
		if err := s.EnsureOverseer(state); err != nil {
			slog.Debug("cockpit: initial ensure", "err", err)
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := s.EnsureOverseer(state); err != nil {
					// Noise-control: only Warn when we are still launching.
					state.mu.Lock()
					phase := state.lastPhase
					state.mu.Unlock()
					if phase == cockpitLaunch || phase == cockpitGiveUp {
						slog.Debug("cockpit: ensure tick", "err", err)
					}
				}
			}
		}
	}()
	slog.Info("cockpit: overseer converge loop started", "interval", interval.String())
}
