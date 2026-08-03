// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/marcelocantos/jevons/internal/transcript"
)

// Soft-empty reasons for GET /api/agents/{name}/transcript (🎯T128.2).
// Operators rg empty_reason= after an empty RHS pane.
const (
	emptyReasonNoSession = "no_session"
	emptyReasonReadError = "read_error"
	emptyReasonZeroTurns = "zero_turns"
)

// SetTranscriptReader attaches the multi-provider transcript reader used by
// GET /api/agents/{name}/transcript (🎯T124) and inspect wire history (🎯T209).
// Roots cover Grok sessions and Claude projects (🎯T213).
func (s *Server) SetTranscriptReader(r *transcript.Reader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transcriptReader = r
}

// logTranscriptEmpty fingerprints a soft-empty transcript response so operators
// can reconstruct why the RHS pane was empty without re-running (🎯T128.2).
// Dual-writes via LogEvent when the journal is open (🎯T128.4 helper).
func (s *Server) logTranscriptEmpty(reason, name, sessionID, errMsg string) {
	fields := map[string]any{
		"empty_reason": reason,
		"name":         name,
		"msg":          "agent_transcript empty",
	}
	if sessionID != "" {
		fields["session_id"] = sessionID
	}
	if errMsg != "" {
		fields["err"] = errMsg
	}
	s.LogEvent("agent_transcript", "empty", fields)
}

// handleAgentTranscript serves one fleet agent's conversation turns for debug
// / export residual (🎯T124). Product RHS inspect primary path is /ws/chat
// inspect_subscribe (🎯T209); this HTTP endpoint is not required while selected.
func (s *Server) handleAgentTranscript(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
		return
	}

	payload, ok := s.buildAgentTranscriptPayload(name)
	if !ok {
		http.Error(w, `{"error":"agent not found"}`, http.StatusNotFound)
		return
	}
	// HTTP residual omits wire envelope fields clients only need on WS.
	delete(payload, "type")
	delete(payload, "kind")
	delete(payload, "denied")
	if errMsg, _ := payload["error"].(string); errMsg == "no agent registry" {
		http.Error(w, `{"error":"no agent registry"}`, http.StatusServiceUnavailable)
		return
	}
	if errMsg, _ := payload["error"].(string); errMsg == "transcript reader unavailable" {
		http.Error(w, `{"error":"transcript reader unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	// Overseer denied on wire path; HTTP returns empty soft state for parity.
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
