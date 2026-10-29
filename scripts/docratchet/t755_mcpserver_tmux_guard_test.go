// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestT755McpserverNeverKillsLiveFleetPanes ratchets 🎯T755 at repo level:
// mcpserver production sources must not reintroduce paneIO / SweepOrphanPanes /
// kill-pane on the live fleet tmux socket (exit-137 incident; T459 removed the
// sweep; T740 TestMain alone did not fix paneIO's default).
func TestT755McpserverNeverKillsLiveFleetPanes(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "internal", "mcpserver")
	var violations []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
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
		rel, _ := filepath.Rel(root, path)
		for _, needle := range []string{"kill-pane", "SweepOrphanPanes", "AgentTmuxSocket", "func (s *Server) paneIO"} {
			if strings.Contains(body, needle) {
				violations = append(violations, rel+": contains "+needle)
			}
		}
		return nil
	})
	if len(violations) > 0 {
		t.Fatalf("🎯T755 docratchet:\n%s", strings.Join(violations, "\n"))
	}
}
