// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bytes"
	"strings"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/seatactivity"
	"github.com/marcelocantos/jevons/internal/spool"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// DefaultSessionRoots is the on-disk pair the development daemon reads: Grok
// sessions (ordinary, exclusive-MCP, and durable claudia grok-homes) and
// Claude projects. Tests override via SessionPhase. Omitting exclusive-MCP
// homes made Grok absence unprovable for those seats (🎯T679.1). Omitting
// grok-homes marked a live Grok PO born-stuck while updates.jsonl grew
// (🎯T694).
func DefaultSessionRoots() discovery.Roots {
	return seatactivity.DefaultRoots()
}

// AgentTranscriptPath is the current session file for d — registry session
// id at the moment of the read, not a stale id (🎯T423 clause 6).
func AgentTranscriptPath(d claudia.AgentDef, roots discovery.Roots) string {
	if spool.SidecarProvider(string(d.Provider)) && spool.SeatHasHistory(spool.Dir(), d.Name) {
		// Sidecar seats have no vendor JSONL. Callers that need bytes
		// use classifyAgentSessionPhase / spool.ReadSeat (🎯T866.4).
		return ""
	}
	sid := strings.TrimSpace(d.SessionID)
	if sid == "" {
		return ""
	}
	if p := discovery.TranscriptPath(roots, sid); p != "" {
		return p
	}
	if roots.ClaudeProjects != "" && strings.TrimSpace(d.WorkDir) != "" {
		if p := discovery.ClaudeJSONLPathForWorkDir(roots.ClaudeProjects, d.WorkDir, sid); p != "" {
			return p
		}
	}
	return ""
}

// classifyAgentSessionPhase is the product idle/repair reading: T422
// Decode via ClassifyPhase on the agent's current session. Missing or
// unreadable is unknown, never idle.
func classifyAgentSessionPhase(d claudia.AgentDef, roots discovery.Roots) turnev.Phase {
	if spool.SidecarProvider(string(d.Provider)) && spool.SeatHasHistory(spool.Dir(), d.Name) {
		recs, err := spool.ReadSeat(spool.Dir(), d.Name)
		if err != nil || len(recs) == 0 {
			return turnev.PhaseUnknown
		}
		return turnev.ClassifyPhase(turnev.DecodeAll(bytes.NewReader(spool.AsJSONL(recs))))
	}
	return turnev.ClassifyPhaseFile(AgentTranscriptPath(d, roots))
}
