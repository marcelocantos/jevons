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

// 🎯T182 / 🎯T275: POST /api/agents/{name}/send — fire-and-forget deliver to a
// fleet agent (HTTP product path for sidebar Transcript, frontier play, asides).
// Production wires agentSendHook → mcpserver.DeliverAgentMessage so busy turns
// queue (same as MCP jevons_agent_send) instead of 409 dead-end. Without a
// hook, bare registry Send remains for hermetic tests; busy still surfaces as
// a clear error (no silent drop).

// agentSendRequest is the JSON body for POST /api/agents/{name}/send.
// Origin marks who is speaking (🎯T309.2): "owner" (default) is an owner turn,
// "agent" is an injected agent/system notification. It only changes overseer
// framing today — owner turns carry the userTurnPrefix marker and paint an
// owner bubble, exactly as the /ws/chat wire does (🎯T63).
type agentSendRequest struct {
	Text   string `json:"text"`
	Origin string `json:"origin,omitempty"`
}

// Send origins for agentSendRequest.Origin.
const (
	sendOriginOwner = "owner"
	sendOriginAgent = "agent"
)

// agentSendResponse is returned on success.
type agentSendResponse struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // sent | rehydrated_sent | queued | interrupted_*
	Message string `json:"message,omitempty"`
}

// agentSendHook is the product/test deliver seam: (name, text) → (status, error).
// Production: mcpserver.DeliverAgentMessage (queue-on-busy). Tests: stub.
func (s *Server) SetAgentSendHook(fn func(name, text string) (status string, err error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentSendHook = fn
}

// sendToNamedAgent rehydrates a registered fleet agent if needed and sends
// text fire-and-forget (no WaitForResponse). Returns status from the product
// hook (sent | queued | rehydrated_sent | …) or bare registry "sent".
// Origin defaults to owner (see agentSendRequest).
func (s *Server) sendToNamedAgent(name, text string) (string, error) {
	return s.sendToNamedAgentAs(name, text, sendOriginOwner)
}

// sendToNamedAgentAs is the agent-addressed send op of the 🎯T309.2 family:
// one call reaches ANY agent by name, overseer included. The overseer resolves
// to the same queue-on-busy delivery /ws/chat uses (never a silent drop), so
// no send capability is exclusive to the owner wire.
func (s *Server) sendToNamedAgentAs(name, text, origin string) (string, error) {
	name = strings.TrimSpace(name)
	text = strings.TrimSpace(text)
	if name == "" || text == "" {
		return "", fmt.Errorf("name and text are required")
	}

	if s.isOverseerAgent(name) {
		var err error
		if origin == sendOriginAgent {
			err = s.sendToOverseerAsAgent(text)
		} else {
			err = s.sendToOverseerAsOwner(text)
		}
		if err != nil {
			return "", err
		}
		return "sent", nil
	}

	// 🎯T367: journal the turn BEFORE delivery, the fleet mirror of the owner
	// wire's journal-then-send. A message the owner typed into the sidebar is
	// durable even if delivery fails, the provider never flushes its session,
	// or the daemon is bounced in the next second.
	s.journalAgentUserTurn(name, text)

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
		// No product hook: busy is a loud failure (not silent). Production
		// always sets the MCP deliver hook so busy queues instead (🎯T275).
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

	status, err := s.sendToNamedAgentAs(name, text, strings.TrimSpace(req.Origin))
	if err != nil {
		// 🎯T237: structured class for T236 recovery; owner copy beyond bare Internal error.
		class, ownerMsg := agenterr.ClassifyAndFormat(err)
		if !class.IsFailure() {
			ownerMsg = err.Error()
		}
		slog.Warn("agent_send_http",
			"component", "agent_send",
			"name", name,
			"failure_class", class.String(),
			"transient", class.IsTransient(),
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
		writeJSONErrorClass(w, code, ownerMsg, class)
		return
	}

	slog.Info("agent_send_http",
		"component", "agent_send",
		"name", name,
		"status", status,
	)
	msg := fmt.Sprintf("Message delivered to %q (%s)", name, status)
	if status == "queued" {
		msg = fmt.Sprintf("Message queued for %q (prompt in flight; will deliver when the turn ends)", name)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(agentSendResponse{
		Name:    name,
		Status:  status,
		Message: msg,
	})
}

// writeJSONError writes {"error": msg} with the given status.
func writeJSONError(w http.ResponseWriter, code int, msg string) {
	writeJSONErrorClass(w, code, msg, agenterr.ClassNone)
}

// writeJSONErrorClass writes {"error": msg, "failure_class": …} for 🎯T237.
func writeJSONErrorClass(w http.ResponseWriter, code int, msg string, class agenterr.Class) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	body := map[string]string{"error": msg}
	if class.IsFailure() {
		body["failure_class"] = class.String()
	}
	_ = json.NewEncoder(w).Encode(body)
}
