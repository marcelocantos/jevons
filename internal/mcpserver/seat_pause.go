// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/jevons/internal/seatstate"
)

// OwnerHoldPaused is the hold reason when frontier auto-hold and dead-handle
// sweeps must leave an owner-paused seat alone (🎯T981).
const OwnerHoldPaused = "owner_paused"

// SeatPauseResumeText is injected once on unpause before any held owner text.
const SeatPauseResumeText = "You were paused by the owner; continue where you left off."

type seatPauseEntry struct {
	held []string
}

// SeatOwnerPaused reports whether the owner paused this seat (🎯T981).
func (s *Server) SeatOwnerPaused(name string) bool {
	if s == nil {
		return false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seatOwnerPause != nil && s.seatOwnerPause[name] != nil
}

func (s *Server) seatPauseEntryLocked(name string) *seatPauseEntry {
	if s.seatOwnerPause == nil {
		s.seatOwnerPause = map[string]*seatPauseEntry{}
	}
	e := s.seatOwnerPause[name]
	if e == nil {
		e = &seatPauseEntry{}
		s.seatOwnerPause[name] = e
	}
	return e
}

// PauseSeatOwner interrupts the seat's current turn and marks it paused.
func (s *Server) PauseSeatOwner(name, actor string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("agent name is required")
	}
	if s == nil || s.registry == nil {
		return fmt.Errorf("agent registry not available")
	}
	if s.registry.Def(name) == nil {
		return fmt.Errorf("agent %q is not registered", name)
	}
	if s.isOverseerAgent(name) {
		return fmt.Errorf("the overseer is not paused per-seat (🎯T981)")
	}
	s.mu.Lock()
	s.seatPauseEntryLocked(name)
	s.mu.Unlock()
	_ = s.interruptSeatTurn(name)
	slogPause := strings.TrimSpace(actor)
	if slogPause == "" {
		slogPause = "owner"
	}
	s.logLifecycle(compAgentLifecycle, "seat_pause", "ok", map[string]any{
		"name": name, "actor": slogPause,
	})
	return nil
}

// UnpauseSeatOwner clears the paused mark and delivers the resume instruction
// once, then any owner text held during the pause.
func (s *Server) UnpauseSeatOwner(name, actor string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("agent name is required")
	}
	if s == nil || s.registry == nil {
		return fmt.Errorf("agent registry not available")
	}
	if s.registry.Def(name) == nil {
		return fmt.Errorf("agent %q is not registered", name)
	}
	s.mu.Lock()
	var held []string
	if s.seatOwnerPause != nil {
		if e := s.seatOwnerPause[name]; e != nil {
			held = append(held, e.held...)
		}
		delete(s.seatOwnerPause, name)
	}
	s.mu.Unlock()
	slogUnpause := strings.TrimSpace(actor)
	if slogUnpause == "" {
		slogUnpause = "owner"
	}
	s.logLifecycle(compAgentLifecycle, "seat_unpause", "ok", map[string]any{
		"name": name, "actor": slogUnpause, "held": len(held),
	})
	if _, err := s.deliverByName(name, SeatPauseResumeText, OriginOwner, false); err != nil {
		return fmt.Errorf("resume deliver: %w", err)
	}
	for _, text := range held {
		if strings.TrimSpace(text) == "" {
			continue
		}
		if _, err := s.deliverByName(name, text, OriginOwner, false); err != nil {
			return fmt.Errorf("held deliver: %w", err)
		}
	}
	return nil
}

func (s *Server) holdOwnerPauseMessage(name, text string) agentSendResult {
	name = strings.TrimSpace(name)
	text = strings.TrimSpace(text)
	s.mu.Lock()
	e := s.seatPauseEntryLocked(name)
	e.held = append(e.held, text)
	n := len(e.held)
	s.mu.Unlock()
	return agentSendResult{
		Status:  "paused_held",
		Message: fmt.Sprintf("agent %q is paused by the owner; message held (%d pending) — unpause to deliver (🎯T981)", name, n),
		Queued:  n,
	}
}

func (s *Server) interruptSeatTurn(name string) error {
	if s == nil || s.registry == nil {
		return nil
	}
	proc := s.registry.Get(name)
	if proc == nil || s.seatState(name).Alive != seatstate.Yes {
		return nil
	}
	return readoptDeliver(name, proc, func(a agentSender) error { return a.Interrupt() })
}

// ownerPauseDeliberateStop is shared by idle-nudge and fleet-recover classifiers.
func ownerPauseDeliberateStop(running, autoStart, ownerPaused bool) bool {
	if ownerPaused {
		return true
	}
	return !running && !autoStart
}
