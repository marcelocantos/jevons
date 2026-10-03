// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/seatstate"

	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/roles"
)

// NoteCapacity folds the current plan readings into the capacity watch and,
// when a plan has just become admissible again, tells the overseer and every
// running product owner so work paused for capacity resumes (🎯T977). It
// returns the agents told.
//
// The notice starts no process, so no fleet intent withholds it; under a
// provider hard block the turn it prompts is also the provider call whose
// success clears the block (🎯T406). Without it, a fleet that stood down for
// capacity waited for an accidental call: on 2026-10-01 it sat quiet for six
// hours with headroom on the plan.
func (s *Server) NoteCapacity() []string {
	if s == nil {
		return nil
	}
	snap, _, now, th, ok := s.planPolicyInputs()
	if !ok {
		return nil
	}
	restored := s.capacityWatch.Observe(snap, now, th)
	if len(restored) == 0 {
		return nil
	}
	parts := make([]string, 0, len(restored))
	for _, be := range restored {
		parts = append(parts, be.Provider+capacityHeadroom(be))
	}
	notice := "[Capacity restored] Plan headroom is back: " + strings.Join(parts, ", ") +
		". If you paused work, stood a worker down, or said you would continue once capacity allows, resume it now: re-engage the workers that were waiting. (🎯T977)"
	deliver := s.capacityDeliver
	if deliver == nil {
		deliver = func(name, text string) error {
			_, err := s.deliverByName(name, text, OriginAgent, false)
			return err
		}
	}
	told := []string{}
	for _, name := range s.capacityNoticeRecipients() {
		if err := deliver(name, notice); err != nil {
			slog.Warn("capacity-restored notice undelivered", "agent", name, "err", err)
			continue
		}
		told = append(told, name)
	}
	slog.Info("capacity restored; agents told to resume", "plans", parts, "told", told)
	return told
}

// capacityHeadroom is " (session 97% left, weekly 58% left)" for a plan's
// windows that report remaining, or "".
func capacityHeadroom(be planusage.Backend) string {
	var out []string
	for _, w := range be.Windows {
		if w.RemainingPercent != nil {
			out = append(out, fmt.Sprintf("%s %.0f%% left", w.Name, *w.RemainingPercent))
		}
	}
	if len(out) == 0 {
		return ""
	}
	return " (" + strings.Join(out, ", ") + ")"
}

// capacityNoticeRecipients is the overseer and every running product owner.
// Workers are left to their product owner, who knows which were waiting.
func (s *Server) capacityNoticeRecipients() []string {
	out := []string{s.overseerName()}
	if s.registry == nil {
		return out
	}
	for _, d := range s.registry.List() {
		if d.Name == "" || d.Name == s.overseerName() {
			continue
		}
		if roles.Normalize(s.agentRole(d.Name, d.Purpose)) != roles.ProductOwner {
			continue
		}
		if p := s.registry.Get(d.Name); p == nil || !(s.seatState(d.Name).Alive == seatstate.Yes) {
			continue
		}
		out = append(out, d.Name)
	}
	return out
}
