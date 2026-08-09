// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"strings"

	"github.com/marcelocantos/jevons/internal/research"
)

// Ambient research output surface (🎯T356): the durable notes the standing
// cycle writes are readable over the product API, not only through MCP chat.

type researchNoteSummary struct {
	ID              string `json:"id"`
	Topic           string `json:"topic"`
	Title           string `json:"title"`
	UpdatedAt       string `json:"updated_at"`
	CheckedAt       string `json:"checked_at,omitempty"`
	Revisions       int    `json:"revisions"`
	CurrentFindings int    `json:"current_findings"`
	Path            string `json:"path"`
	LastTrigger     string `json:"last_trigger,omitempty"`
}

type researchNotesResponse struct {
	Notes []researchNoteSummary `json:"notes"`
	Count int                   `json:"count"`
	Dir   string                `json:"dir,omitempty"`
}

// handleResearchNotes GET /api/research/notes — the listable note index.
// An empty store is an honest empty list, never an error.
func (s *Server) handleResearchNotes(w http.ResponseWriter, r *http.Request) {
	store, ok := s.researchStore(w)
	if !ok {
		return
	}
	notes, err := store.List()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]researchNoteSummary, 0, len(notes))
	for _, n := range notes {
		out = append(out, researchNoteSummary{
			ID:              n.ID,
			Topic:           n.Topic,
			Title:           n.Title,
			UpdatedAt:       n.UpdatedAt,
			CheckedAt:       n.CheckedAt,
			Revisions:       len(n.Revisions),
			CurrentFindings: len(n.CurrentFindings()),
			Path:            store.NotePath(n.ID),
			LastTrigger:     n.LatestRevision().Trigger,
		})
	}
	writeJSON(w, researchNotesResponse{Notes: out, Count: len(out), Dir: store.Dir()})
	_ = r
}

type researchNoteResponse struct {
	Note     research.Note `json:"note"`
	Path     string        `json:"path"`
	Markdown string        `json:"markdown"`
}

// handleResearchNote GET /api/research/notes/{id} — one note with its full
// revision history, superseded conclusions included.
func (s *Server) handleResearchNote(w http.ResponseWriter, r *http.Request) {
	store, ok := s.researchStore(w)
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "note id required")
		return
	}
	note, err := store.Get(id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, researchNoteResponse{
		Note:     note,
		Path:     store.NotePath(note.ID),
		Markdown: research.RenderNote(note),
	})
}

// researchStore opens the note store for the configured state dir, writing the
// error response itself when it cannot.
func (s *Server) researchStore(w http.ResponseWriter) (*research.Store, bool) {
	s.mu.RLock()
	dir := s.stateDir
	s.mu.RUnlock()
	if dir == "" {
		writeJSONError(w, http.StatusInternalServerError, "research notes: stateDir not configured")
		return nil, false
	}
	store, err := research.Open(dir)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	return store, true
}
