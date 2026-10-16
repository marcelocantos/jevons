// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"

	"github.com/marcelocantos/jevons/internal/repowatch"
)

// handleRepoWatch GET /api/repowatch — config, run state, and per-repo
// watermarks for the PO GitHub watch cycle (🎯T1022.2).
func (s *Server) handleRepoWatch(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	dir := s.stateDir
	s.mu.RUnlock()
	if dir == "" {
		writeJSONError(w, http.StatusInternalServerError, "repowatch: stateDir not configured")
		return
	}
	cfg, err := repowatch.LoadConfig(dir)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	st, err := repowatch.LoadState(dir)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	repos, err := repowatch.ListRepoStates(dir)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, repowatch.StatusSnapshot{Config: cfg, State: st, Repos: repos})
	_ = r
}
