// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/marcelocantos/jevons/internal/transcript"
)

// SetTranscriptReader attaches the Grok sessions transcript reader used by
// GET /api/agents/{name}/transcript (🎯T124).
func (s *Server) SetTranscriptReader(r *transcript.Reader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transcriptReader = r
}

// handleAgentTranscript serves one fleet agent's conversation turns for the
// RHS inspect pane — out of band from main owner↔overseer chat.
// GET /api/agents/{name}/transcript
func (s *Server) handleAgentTranscript(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	reg := s.registry
	tr := s.transcriptReader
	s.mu.RUnlock()
	if reg == nil {
		http.Error(w, `{"error":"no agent registry"}`, http.StatusServiceUnavailable)
		return
	}
	if tr == nil {
		http.Error(w, `{"error":"transcript reader unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	var sessionID, purpose, workdir string
	found := false
	for _, d := range reg.List() {
		if d.Name == name {
			found = true
			sessionID = d.SessionID
			purpose = d.Purpose
			workdir = d.WorkDir
			break
		}
	}
	if !found {
		http.Error(w, `{"error":"agent not found"}`, http.StatusNotFound)
		return
	}
	if sessionID == "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":       name,
			"purpose":    purpose,
			"workdir":    workdir,
			"session_id": "",
			"turns":      []any{},
			"empty":      true,
			"note":       "no session_id yet",
		})
		return
	}

	turns, err := tr.Read(sessionID)
	if err != nil {
		// Not found / unreadable — still 200 with empty turns so the pane
		// can show a calm empty state (new agent, mint not materialized).
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":       name,
			"purpose":    purpose,
			"workdir":    workdir,
			"session_id": sessionID,
			"turns":      []any{},
			"empty":      true,
			"error":      err.Error(),
		})
		return
	}
	if turns == nil {
		turns = []map[string]any{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name":       name,
		"purpose":    purpose,
		"workdir":    workdir,
		"session_id": sessionID,
		"turns":      turns,
		"empty":      len(turns) == 0,
	})
}
