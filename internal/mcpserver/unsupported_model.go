// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

const unsupportedModelActor = "product:unsupported_model"

// observeUnsupportedModel is only called with transport/native event errors.
// Never lift this hold from a successful turn: only an explicit owner/parent
// intent change can authorize a retry (and possibly a model change).
func (s *Server) observeUnsupportedModel(name string, class agenterr.Class, raw string) {
	if s == nil || class != agenterr.ClassUnsupportedModel || !agenterr.IsUnsupportedModel(raw) || name == "" {
		return
	}
	// Do not overwrite an owner park, a completed seat, or another explicit
	// hold. This is a seat-specific compatibility failure, not fleet policy.
	state := s.fleetIntent().AgentState(name)
	if state != fleetintent.Working {
		return
	}
	reason := "pinned model unsupported by Codex ChatGPT account: " + truncate(strings.TrimSpace(raw), 220)
	if err := s.SetAgentIntent(name, fleetintent.BlockedProvider, unsupportedModelActor, reason); err != nil {
		slog.Warn("unsupported model intent failed", "agent", name, "err", err)
	}
}
