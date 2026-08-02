// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/transcript"
)

// Soft-empty reasons for GET /api/agents/{name}/transcript (🎯T128.2).
// Operators rg empty_reason= after an empty RHS pane.
const (
	emptyReasonNoSession = "no_session"
	emptyReasonReadError = "read_error"
	emptyReasonZeroTurns = "zero_turns"
)

// SetTranscriptReader attaches the Grok sessions transcript reader used by
// GET /api/agents/{name}/transcript (🎯T124).
func (s *Server) SetTranscriptReader(r *transcript.Reader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transcriptReader = r
}

// logTranscriptEmpty fingerprints a soft-empty transcript response so operators
// can reconstruct why the RHS pane was empty without re-running (🎯T128.2).
// slog is the minimum; dual-write to the event journal when open (T128.4 helper
// can replace the inline append later).
func (s *Server) logTranscriptEmpty(reason, name, sessionID, errMsg string) {
	args := []any{
		"component", "agent_transcript",
		"empty_reason", reason,
		"name", name,
	}
	if sessionID != "" {
		args = append(args, "session_id", sessionID)
	}
	if errMsg != "" {
		args = append(args, "err", errMsg)
	}
	slog.Info("agent_transcript empty", args...)

	if j := s.eventJournal(); j != nil {
		fields := map[string]any{
			"empty_reason": reason,
			"name":         name,
		}
		if sessionID != "" {
			fields["session_id"] = sessionID
		}
		if errMsg != "" {
			fields["err"] = errMsg
		}
		if err := j.Append(eventlog.Event{
			TS:        time.Now().UTC().Format(time.RFC3339Nano),
			Source:    "server",
			Level:     "info",
			Msg:       "agent_transcript empty",
			Component: "agent_transcript",
			Fields:    fields,
		}); err != nil {
			slog.Warn("eventlog: agent_transcript append failed", "err", err, "path", j.Path())
		}
	}
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
		s.logTranscriptEmpty(emptyReasonNoSession, name, "", "")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":         name,
			"purpose":      purpose,
			"workdir":      workdir,
			"session_id":   "",
			"turns":        []any{},
			"empty":        true,
			"empty_reason": emptyReasonNoSession,
			"note":         "no session_id yet",
		})
		return
	}

	turns, err := tr.Read(sessionID)
	if err != nil {
		// Not found / unreadable — still 200 with empty turns so the pane
		// can show a calm empty state (new agent, mint not materialized).
		s.logTranscriptEmpty(emptyReasonReadError, name, sessionID, err.Error())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":         name,
			"purpose":      purpose,
			"workdir":      workdir,
			"session_id":   sessionID,
			"turns":        []any{},
			"empty":        true,
			"empty_reason": emptyReasonReadError,
			"error":        err.Error(),
		})
		return
	}
	if turns == nil {
		turns = []map[string]any{}
	}
	empty := len(turns) == 0
	resp := map[string]any{
		"name":       name,
		"purpose":    purpose,
		"workdir":    workdir,
		"session_id": sessionID,
		"turns":      turns,
		"empty":      empty,
	}
	if empty {
		s.logTranscriptEmpty(emptyReasonZeroTurns, name, sessionID, "")
		resp["empty_reason"] = emptyReasonZeroTurns
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
