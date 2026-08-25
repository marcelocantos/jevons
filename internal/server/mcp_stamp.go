// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"

	"github.com/marcelocantos/jevons/internal/muxwin"
)

// ObserveMCPToolCall stamps a real tools/call name onto the oldest generic
// "MCP: tool" step in the overseer transcript (🎯T64.2). Cursor ACP titles
// those rows MCP: tool with empty input; the HTTP MCP path is the name we
// actually have. Worker calls while no overseer turn is open are ignored so
// they do not invent owner-chat steps.
func (s *Server) ObserveMCPToolCall(name string, args map[string]any) {
	if s == nil {
		return
	}
	name = cleanMCPToolName(name)
	if name == "" || genericToolTitle(name) {
		return
	}
	st := muxwin.ToolStamp{Name: name, Input: args}
	if s.mux == nil {
		s.mux = newMuxHub()
	}
	agent := s.overseerAgentName()
	if folds, ok := s.mux.applyStampNow(agent, st); ok {
		s.statedbUpsertFolds(agent, folds)
		if line := chatToolStampLine(name, args); line != "" {
			s.persistChatJSONL(line)
		}
		return
	}
	if s.overseerTurnOpen() {
		s.mux.queueToolStamp(agent, st)
	}
}

func (s *Server) overseerTurnOpen() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.overseerOwnerTurn || strings.TrimSpace(s.overseerStreamID) != ""
}
