// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// DefaultSessionRoots is the on-disk pair the daily daemon reads: Grok
// sessions and Claude projects. Tests override via SessionPhase.
func DefaultSessionRoots() discovery.Roots {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return discovery.Roots{}
	}
	return discovery.Roots{
		GrokSessions:   filepath.Join(home, ".grok", "sessions"),
		ClaudeProjects: filepath.Join(home, ".claude", "projects"),
	}
}

// AgentTranscriptPath is the current session file for d — registry session
// id at the moment of the read, not a stale id (🎯T423 clause 6).
func AgentTranscriptPath(d claudia.AgentDef, roots discovery.Roots) string {
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

// ClassifyAgentSessionPhase is the product idle/repair reading: T422
// Decode via ClassifyPhase on the agent's current session. Missing or
// unreadable is unknown, never idle.
func ClassifyAgentSessionPhase(d claudia.AgentDef, roots discovery.Roots) turnev.Phase {
	return turnev.ClassifyPhaseFile(AgentTranscriptPath(d, roots))
}
