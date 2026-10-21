// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"log/slog"
	"net/http"

	"github.com/marcelocantos/jevons/internal/ownercockpit"
)

func (s *Server) authorizeOwnerSurfaceHTTP(r *http.Request, origin string, seat string) error {
	s.mu.RLock()
	g := s.ownerCockpit
	s.mu.RUnlock()
	if g == nil {
		return nil
	}
	if err := g.AuthorizeOwnerSend(r, origin); err != nil {
		slog.Warn("owner_cockpit_send_refused",
			"component", "owner_cockpit",
			"seat", seat,
			"origin", origin,
			"fleet_browser", ownercockpit.FleetBrowser(r),
			"err", err.Error(),
		)
		return err
	}
	return nil
}

// handleOwnerCockpitSession GET /api/owner-cockpit/session — mint the HttpOnly
// cookie a real owner browser needs for owner-origin sends (🎯T1129).
func (s *Server) handleOwnerCockpitSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	g := s.ownerCockpit
	s.mu.RUnlock()
	if g == nil {
		http.Error(w, "owner cockpit gate unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := g.IssueSessionCookie(w, r); err != nil {
		slog.Warn("owner_cockpit_bootstrap_refused",
			"component", "owner_cockpit",
			"fleet_browser", ownercockpit.FleetBrowser(r),
			"err", err.Error(),
		)
		writeJSONError(w, http.StatusForbidden, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
