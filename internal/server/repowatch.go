// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"

	"github.com/marcelocantos/jevons/internal/repowatch"
)

// Standing PO repo-watch surface (🎯T1022.2): the durable config and
// per-repo watermark state the daemon cycle maintains, readable over the
// product API — not only through the jevons_repo_watch MCP tool.

type repoWatchStateJSON struct {
	Repo      string          `json:"repo"`
	Watermark string          `json:"watermark,omitempty"`
	CheckedAt string          `json:"checked_at,omitempty"`
	LastItems []repowatch.Item `json:"last_items,omitempty"`
}

type repoWatchResponse struct {
	Config repowatch.Config     `json:"config"`
	Repos  []repoWatchStateJSON `json:"repos"`
}

// handleRepoWatch GET /api/repowatch — current config plus every watched
// repo's persisted watermark state. An empty/never-run store is an honest
// empty list, never an error.
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
	states, err := repowatch.ListStates(dir)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]repoWatchStateJSON, 0, len(states))
	for _, st := range states {
		wm := ""
		if !st.Watermark.IsZero() {
			wm = st.Watermark.Format("2006-01-02T15:04:05Z")
		}
		out = append(out, repoWatchStateJSON{
			Repo:      st.Repo,
			Watermark: wm,
			CheckedAt: st.CheckedAt,
			LastItems: st.LastItems,
		})
	}
	writeJSON(w, repoWatchResponse{Config: cfg, Repos: out})
	_ = r
}
