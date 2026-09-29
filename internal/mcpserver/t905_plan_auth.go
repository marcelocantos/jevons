// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "github.com/marcelocantos/jevons/internal/agenterr"

// 🎯T905: a seat that is still running but whose turns the provider refuses
// on its plan login (a revoked token) needs the owner's Reauth as much as a
// stopped one. These are the fleet layer's half: which running seats that
// is, and forgetting it once the owner has repaired the login.

// PlanAuthFailed reports that name's latest turn was refused on its login.
// The backend must have reported the turn as failed: a reply that talks
// about a 401 is a seat whose login works (🎯T945).
func (s *Server) PlanAuthFailed(name string) bool {
	s.mu.Lock()
	tracker := s.idleActivity
	s.mu.Unlock()
	act := tracker.Get(name)
	return act.FailureClass == agenterr.ClassAuth && act.TurnErrored
}

// ClearPlanAuthFailures forgets the login refusals of the named seats after
// the owner repaired their plan: the refusal is answered, and a Reauth left
// showing would read as still broken. A seat that is refused again latches
// it again on that turn.
func (s *Server) ClearPlanAuthFailures(names []string) {
	s.mu.Lock()
	tracker := s.idleActivity
	s.mu.Unlock()
	if tracker == nil {
		return
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	for _, name := range names {
		act, ok := tracker.by[name]
		if !ok || act.FailureClass != agenterr.ClassAuth {
			continue
		}
		act.FailureClass = agenterr.ClassNone
		tracker.by[name] = act
	}
}
