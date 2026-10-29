// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// T755FleetTmuxProductionViolations scans mcpserver production sources for the
// pre-T459 / pre-T740 hazard: handleAgentList (or SweepOrphanPanes) reaching
// tmux kill-pane on the live fleet socket via paneIO's cli.AgentTmuxSocket
// default. Pane census was removed in T459; this ratchet pins the absence and
// the env-only socket contract if it returns.
func T755FleetTmuxProductionViolations(mcpserverDir string) ([]string, error) {
	var violations []string
	err := filepath.WalkDir(mcpserverDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if d.Name() == "t755_fleet_tmux_guard.go" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		body := string(raw)
		rel, _ := filepath.Rel(mcpserverDir, path)
		for _, v := range t755ViolationsInSource(rel, body) {
			violations = append(violations, v)
		}
		return nil
	})
	return violations, err
}

func t755ViolationsInSource(rel, body string) []string {
	var out []string
	if strings.Contains(body, "kill-pane") {
		out = append(out, rel+": production must not invoke tmux kill-pane (🎯T755)")
	}
	if strings.Contains(body, "SweepOrphanPanes") {
		out = append(out, rel+": SweepOrphanPanes was removed with 🎯T459; reintroduce only behind CLAUDIA_TMUX_SOCKET, not live defaults (🎯T755)")
	}
	if strings.Contains(body, "func (s *Server) paneIO") {
		out = append(out, rel+": paneIO must not fall back to the live fleet socket (🎯T755)")
	}
	if strings.Contains(body, "AgentTmuxSocket") {
		out = append(out, rel+": must not use AgentTmuxSocket for fleet pane I/O (🎯T755)")
	}
	return out
}
