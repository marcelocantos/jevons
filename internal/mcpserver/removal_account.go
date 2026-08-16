// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "github.com/marcelocantos/jevons/internal/fleetlog"

// SetRemovalAccount attaches the accounted-removal chokepoint (🎯T435). The
// daemon builds one Account for the whole process and hands the same one to
// the HTTP server, so an achieve-reap decided over there is legible on the
// fleet surfaces read over here.
func (s *Server) SetRemovalAccount(a *fleetlog.Account) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removals = a
}

// RemovalAccount returns the wired Account, falling back to one bound to this
// server's own event sink. Safe when nil: the removal still happens and still
// accounts for itself through slog, because silence is the failure mode.
func (s *Server) RemovalAccount() *fleetlog.Account {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.removals == nil {
		s.removals = fleetlog.New(s.LogEvent)
	}
	a := s.removals
	s.mu.Unlock()
	return a
}
