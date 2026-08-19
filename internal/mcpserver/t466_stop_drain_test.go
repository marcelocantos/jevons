// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/attrib"
)

// TestT466AgentStopDrainsSharedIndex is the daemon-path half of the 🎯T466
// acceptance: after jevons_agent_stop, `git diff --cached --name-only` in the
// stopped agent's repo is clean, and what was staged is saved and attributed
// rather than destroyed.
func TestT466AgentStopDrainsSharedIndex(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := attrib.Git(repo, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "master")
	git("config", "user.email", "t466@test")
	git("config", "user.name", "t466")
	if err := os.WriteFile(filepath.Join(repo, "f.go"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "f.go")
	git("commit", "-q", "-m", "base")
	// The 🎯T457 hazard, armed: a worker leaves an edit staged and stops.
	if err := os.WriteFile(filepath.Join(repo, "f.go"), []byte("staged, unowned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "f.go")

	attribRoot := t.TempDir()
	t.Setenv(attrib.StoreDirEnv, attribRoot)

	regDir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(regDir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t466-fixture", WorkDir: repo, SessionID: "s-fixture",
		Materialized: true, Provider: "grok", Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}

	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "jv-t466-fixture", "actor": "jevons"}
	res, err := s.handleAgentStop(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("stop: %s", toolText(res))
	}

	clean, err := attrib.IndexClean(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Fatal("shared index must be empty after an agent stops (🎯T466)")
	}
	// The content is not destroyed and the drain is on record, under the
	// stopping agent's name.
	b, err := os.ReadFile(filepath.Join(repo, "f.go"))
	if err != nil || string(b) != "staged, unowned\n" {
		t.Fatalf("working tree changed by drain: %q, %v", b, err)
	}
	drains, err := filepath.Glob(filepath.Join(attribRoot, "drains", "*-jv-t466-fixture"))
	if err != nil || len(drains) != 1 {
		t.Fatalf("want one drain dir named for the agent, got %v (%v)", drains, err)
	}
	if _, err := os.Stat(filepath.Join(drains[0], "index.patch")); err != nil {
		t.Fatalf("drain saved no index patch: %v", err)
	}
}
