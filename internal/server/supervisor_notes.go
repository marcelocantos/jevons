// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/marcelocantos/jevons/internal/supervisormem"
)

type supervisorNotesResponse struct {
	Path         string `json:"path"`
	Content      string `json:"content"`
	RollingLines int    `json:"rolling_lines"`
}

type supervisorRollingRequest struct {
	Block string `json:"block"`
}

type supervisorRollingResponse struct {
	Path         string `json:"path"`
	RollingLines int    `json:"rolling_lines"`
}

// handleSupervisorNotes GET /api/supervisor/notes — full notes.md (🎯T704).
func (s *Server) handleSupervisorNotes(w http.ResponseWriter, r *http.Request) {
	store, ok := s.supervisorNotesStore(w)
	if !ok {
		return
	}
	body, err := store.Read()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lines, _ := supervisormem.RollingLineCount(body)
	writeJSON(w, supervisorNotesResponse{
		Path:         store.Path,
		Content:      body,
		RollingLines: lines,
	})
	_ = r
}

// handleSupervisorNotesRolling POST /api/supervisor/notes/rolling — append one
// dated ### block without filesystem access from the caller (🎯T704).
func (s *Server) handleSupervisorNotesRolling(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	store, ok := s.supervisorNotesStore(w)
	if !ok {
		return
	}
	var req supervisorRollingRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	block := strings.TrimSpace(req.Block)
	if block == "" {
		writeJSONError(w, http.StatusBadRequest, "block required")
		return
	}
	if err := store.AppendRollingBlock(block); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	body, err := store.Read()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lines, _ := supervisormem.RollingLineCount(body)
	writeJSON(w, supervisorRollingResponse{Path: store.Path, RollingLines: lines})
}

func (s *Server) supervisorNotesStore(w http.ResponseWriter) (*supervisormem.Store, bool) {
	s.mu.RLock()
	dir := s.stateDir
	s.mu.RUnlock()
	if dir == "" {
		writeJSONError(w, http.StatusInternalServerError, "supervisor notes: stateDir not configured")
		return nil, false
	}
	store, err := supervisormem.Open(dir)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	return store, true
}

type supervisorEscalateRequest struct {
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Severity string `json:"severity"`
	Key      string `json:"key"`
}

type supervisorEscalateResponse struct {
	Spool string `json:"spool"`
}

// handleSupervisorEscalate POST /api/supervisor/escalate — blurter from the
// daemon process so a sandboxed non-interactive seat need not run blurter
// through Bash (🎯T704 / 🎯T700).
func (s *Server) handleSupervisorEscalate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req supervisorEscalateRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	spool, err := supervisormem.Escalate(req.Subject, req.Body, req.Severity, req.Key, nil)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, supervisorEscalateResponse{Spool: spool})
}
