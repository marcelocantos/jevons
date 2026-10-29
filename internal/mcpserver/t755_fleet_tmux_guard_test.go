// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// 🎯T755: an unguarded handleAgentList must not exec tmux kill-pane. The
// pre-T740 default paneIO path did; T459 removed pane census entirely. This
// traps PATH so any reintroduced sweep is caught without touching live tmux.
func TestT755HandleAgentListDoesNotExecTmuxKillPane(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "tmux-invocations.log")
	installT755TmuxRecorder(t, logPath)

	s, _ := t401Server(t)
	t401RegisterWork(t, s, "jv-t755-probe")

	_, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return
	}
	if strings.Contains(string(raw), "kill-pane") {
		t.Fatalf("handleAgentList must not run tmux kill-pane; log:\n%s", raw)
	}
}

func TestT755McpserverProductionHasNoFleetTmuxKillPath(t *testing.T) {
	dir := "."
	if _, err := os.Stat("t755_fleet_tmux_guard.go"); err != nil {
		dir = filepath.Join("..", "mcpserver")
	}
	violations, err := T755FleetTmuxProductionViolations(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("🎯T755 violations:\n%s", strings.Join(violations, "\n"))
	}
}

// Acceptance: restoring the live-socket default (pre-T740 paneIO) goes RED.
func TestT755MutationLiveSocketPaneSweepGoesRed(t *testing.T) {
	const mutant = `
package mcpserver
func defaultKillFleetPane(id string) error {
	sock := cli.AgentTmuxSocket()
	return exec.Command("tmux", "-S", sock, "kill-pane", "-t", id).Run()
}
func (s *Server) paneIO() {}
func (s *Server) SweepOrphanPanes() int { return 0 }
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mutant.go"), []byte(mutant), 0644); err != nil {
		t.Fatal(err)
	}
	violations, err := T755FleetTmuxProductionViolations(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) < 3 {
		t.Fatalf("mutation must trip multiple T755 rules, got %v", violations)
	}
}

func installT755TmuxRecorder(t *testing.T, logPath string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "tmux")
	content := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\nexit 1\n", logPath)
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
