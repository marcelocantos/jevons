// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T934: a seat that launched without a required MCP server is named at
// mint — in the spawn result and to its parent — before its brief goes out.
// 🎯T797's transcript read only noticed it after a five-minute grace, by
// which time the seat had been working without bullseye or jevons_* tools.
// Claudia reports the servers that listed no tools at launch
// (Agent.HostMCPUnavailable); only the fleet-critical ones this seat is
// configured with count (RequiredServers), as in 🎯T797.

// mintMCPMissing is the required servers the seat started without.
func mintMCPMissing(d claudia.AgentDef, unavailable []string) []string {
	// Every server answered: nothing to read, not even the seat's
	// registrations, on the start path.
	if len(unavailable) == 0 {
		return nil
	}
	var out []string
	for _, name := range requiredSeatServers(d) {
		if slices.Contains(unavailable, name) {
			out = append(out, name)
		}
	}
	return out
}

// FormatMintMCPNotice is the parent message and the spawn-result line.
func FormatMintMCPNotice(d claudia.AgentDef, missing []string) string {
	return fmt.Sprintf(
		"mcp-unavailable-at-mint: %s (session=%s) started without required MCP server(s) %s — they listed no tools at launch, so their tools are absent; re-mint the seat once the server answers",
		d.Name, strings.TrimSpace(d.SessionID), strings.Join(missing, ", "))
}

// noteMintMCPUnavailable tells the parent once and returns the text for the
// spawn result, or "" when the seat has every required server.
func (s *Server) noteMintMCPUnavailable(d claudia.AgentDef, unavailable []string) string {
	missing := mintMCPMissing(d, unavailable)
	if len(missing) == 0 {
		return ""
	}
	text := FormatMintMCPNotice(d, missing)
	slog.Error("seat started without a required MCP server", "agent", d.Name, "missing", missing)
	key := "mcp-mint:" + d.Name + ":" + strings.TrimSpace(d.SessionID) + ":" + strings.Join(missing, ",")
	now := time.Now()
	parent := strings.TrimSpace(d.Parent)
	if parent == "" {
		parent = s.overseerName()
	}
	if parent != "" && parent != d.Name && !s.noticeAlreadySubmitted(key) {
		res, err := s.deliverByName(parent, text, OriginAgent, false)
		if noticeSubmitted(res.Status, err) {
			s.markNoticeOutcome(key, true, nil, now)
		} else {
			if err == nil {
				err = fmt.Errorf("notice status %q is not submitted", res.Status)
			}
			s.markNoticeOutcome(key, false, err, now)
		}
	}
	return " [" + text + "]"
}
