// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/marcelocantos/jevons/internal/idea"
)

// Idea intake HTTP surface (🎯T325.3): durable listable ledger so owner
// sparks are not scrollback-only. Triage dispositions: inbox|file|park|hold|drop.

func (s *Server) openIdeaStore() (*idea.Store, error) {
	s.mu.RLock()
	dir := s.stateDir
	s.mu.RUnlock()
	if dir == "" {
		return nil, fmt.Errorf("idea: stateDir not configured")
	}
	// Per-server store (tests create many Servers); avoid process singleton.
	return idea.Open(dir)
}

type captureIdeaRequest struct {
	Text    string `json:"text"`
	Source  string `json:"source,omitempty"`
	Domain  string `json:"domain,omitempty"`
	AsideID string `json:"aside_id,omitempty"`
}

type triageIdeaRequest struct {
	Disposition string `json:"disposition"`
	Note        string `json:"note,omitempty"`
	Domain      string `json:"domain,omitempty"`
	TargetID    string `json:"target_id,omitempty"`
}

type ideaListResponse struct {
	Ideas []idea.Record `json:"ideas"`
	Count int           `json:"count"`
}

// handleCaptureIdea POST /api/ideas — durable intake.
func (s *Server) handleCaptureIdea(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req captureIdeaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	st, err := s.openIdeaStore()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rec, err := st.Capture(idea.CaptureArgs{
		Text:    req.Text,
		Source:  idea.NormalizeSource(req.Source),
		Domain:  req.Domain,
		AsideID: req.AsideID,
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	slog.Info("idea captured", "id", rec.ID, "source", rec.Source, "disposition", rec.Disposition)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, rec)
}

// handleListIdeas GET /api/ideas — listable surface for triage / oracle.
func (s *Server) handleListIdeas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	st, err := s.openIdeaStore()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	disp := r.URL.Query().Get("disposition")
	list, err := st.List(disp)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []idea.Record{}
	}
	writeJSON(w, ideaListResponse{Ideas: list, Count: len(list)})
}

// handleTriageIdea PATCH /api/ideas/{id} — triage ceremony.
func (s *Server) handleTriageIdea(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "PATCH or POST")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "id is required")
		return
	}
	var req triageIdeaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	st, err := s.openIdeaStore()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rec, err := st.Triage(idea.TriageArgs{
		ID:          id,
		Disposition: idea.Disposition(req.Disposition),
		Note:        req.Note,
		Domain:      req.Domain,
		TargetID:    req.TargetID,
	})
	if err != nil {
		msg := err.Error()
		code := http.StatusBadRequest
		if strings.Contains(msg, "not found") {
			code = http.StatusNotFound
		}
		writeJSONError(w, code, msg)
		return
	}
	slog.Info("idea triaged",
		"id", rec.ID,
		"disposition", rec.Disposition,
		"ceremony", idea.NextCeremony(rec.Disposition),
	)
	// Enrich response with next-ceremony hint for agents/UI.
	type triageResp struct {
		idea.Record
		NextCeremony string `json:"next_ceremony"`
	}
	writeJSON(w, triageResp{Record: rec, NextCeremony: idea.NextCeremony(rec.Disposition)})
}
