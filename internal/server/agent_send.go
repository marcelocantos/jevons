// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T182: POST /api/agents/{name}/send — fire-and-forget deliver to a fleet
// agent (HTTP product path for frontier play → jevons-po kickoff). Matches
// MCP jevons_agent_send semantics: rehydrate if needed, do not wait for reply.
// Busy ("prompt already in flight") is returned as a clear error — no silent drop.

// agentSendRequest is the JSON body for POST /api/agents/{name}/send.
type agentSendRequest struct {
	Text string `json:"text"`
}

// agentSendResponse is returned on success.
type agentSendResponse struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // sent | rehydrated_sent
	Message string `json:"message,omitempty"`
}

// agentSendHook is an optional test seam: (name, text) → (status, error).
// When set, live registry Launch/Send is skipped.
func (s *Server) SetAgentSendHook(fn func(name, text string) (status string, err error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentSendHook = fn
}

// sendToNamedAgent rehydrates a registered fleet agent if needed and sends
// text fire-and-forget (no WaitForResponse). Returns status "sent" or
// "rehydrated_sent".
func (s *Server) sendToNamedAgent(name, text string) (string, error) {
	name = strings.TrimSpace(name)
	text = strings.TrimSpace(text)
	if name == "" || text == "" {
		return "", fmt.Errorf("name and text are required")
	}

	s.mu.RLock()
	hook := s.agentSendHook
	reg := s.registry
	s.mu.RUnlock()

	if hook != nil {
		return hook(name, text)
	}
	if reg == nil {
		return "", fmt.Errorf("agent registry not available")
	}
	if reg.Def(name) == nil {
		return "", fmt.Errorf("agent %q is not registered", name)
	}

	rehydrated := false
	proc := reg.Get(name)
	if proc == nil || !proc.Alive() {
		launched, err := reg.Launch(name)
		if err != nil {
			return "", fmt.Errorf("agent %q rehydrate failed: %w", name, err)
		}
		proc = launched
		rehydrated = true
	}
	if err := proc.Send(text); err != nil {
		return "", err
	}
	if rehydrated {
		return "rehydrated_sent", nil
	}
	return "sent", nil
}

// handleAgentSend POST /api/agents/{name}/send
func (s *Server) handleAgentSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "name is required")
		return
	}
	var req agentSendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeJSONError(w, http.StatusBadRequest, "text is required")
		return
	}

	status, err := s.sendToNamedAgent(name, text)
	if err != nil {
		slog.Warn("agent_send_http",
			"component", "agent_send",
			"name", name,
			"err", err.Error(),
		)
		// Map common cases to status codes.
		msg := err.Error()
		code := http.StatusBadGateway
		if strings.Contains(msg, "not registered") || strings.Contains(msg, "not available") {
			code = http.StatusNotFound
		} else if strings.Contains(msg, "required") {
			code = http.StatusBadRequest
		} else if agenterr.IsPromptBusy(err) {
			code = http.StatusConflict
		}
		writeJSONError(w, code, msg)
		return
	}

	slog.Info("agent_send_http",
		"component", "agent_send",
		"name", name,
		"status", status,
	)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(agentSendResponse{
		Name:    name,
		Status:  status,
		Message: fmt.Sprintf("Message delivered to %q (%s)", name, status),
	})
}

// writeJSONError writes {"error": msg} with the given status.
func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
