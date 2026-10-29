// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// unsupportedModelActor stamps seat-local blocked_provider when a pinned model
// is refused by the native provider (🎯T1052). This is not fleet-wide 🎯T406
// hard-block — other seats keep working on supported models.
const unsupportedModelActor = "product:unsupported_model"

// latchUnsupportedModelPin records that this seat must not be idle-nudged,
// fleet-recovered, or repressured until an owner/parent clears intent. Registry
// row, session, and rollout are untouched.
func (s *Server) latchUnsupportedModelPin(name, detail string) {
	if s == nil || name == "" || !modelNotSupported(detail) {
		return
	}
	state := s.fleetIntent().AgentState(name)
	if state != fleetintent.Working && state != fleetintent.Unknown {
		return
	}
	reason := "pinned model unsupported by provider account: " + truncate(strings.TrimSpace(detail), 220)
	if err := s.SetAgentIntent(name, fleetintent.BlockedProvider, unsupportedModelActor, reason); err != nil {
		slog.Warn("unsupported model intent failed", "agent", name, "err", err)
	}
}

// noteUnsupportedModelFromAgentEvent handles native Codex turn/failed system
// IsError events that never become assistant terminal stops (jv-t1046 probe).
func (s *Server) noteUnsupportedModelFromAgentEvent(name string, errored bool, reason, terminal string) {
	combined := strings.TrimSpace(reason) + " " + strings.TrimSpace(terminal)
	if !modelNotSupported(combined) {
		return
	}
	detail := strings.TrimSpace(reason)
	if detail == "" {
		detail = strings.TrimSpace(terminal)
	}
	s.latchUnsupportedModelPin(name, detail)
}
