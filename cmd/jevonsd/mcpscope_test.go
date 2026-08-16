// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpscope"
)

// fixtureConfig writes a ~/.claude.json in the shape the incident left it:
// jevonsmcp under one project key and nowhere else.
func fixtureConfig(t *testing.T) string {
	t.Helper()
	doc := map[string]any{
		"numStartups": 41,
		"mcpServers": map[string]any{
			"bullseye": map[string]any{"command": "/opt/homebrew/bin/mcpbridge"},
		},
		"projects": map[string]any{
			"/Users/marcelo/work/github.com/marcelocantos/jevons": map[string]any{
				"mcpServers": map[string]any{
					mcpscope.ServerName: map[string]any{"type": "http", "url": mcpscope.DefaultEndpoint},
				},
			},
		},
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// TestT464DailyDaemonRepairsUserScope is the plumbing half of 🎯T464 at the
// wiring level: the daily daemon, on boot, leaves the registration reachable
// from every working directory rather than from one repo.
func TestT464DailyDaemonRepairsUserScope(t *testing.T) {
	path := fixtureConfig(t)
	cfg := config.Default() // daily state dir, daily MCP name

	action, err := ensureFleetMCPUserScopeAt(cfg, "127.0.0.1", 13705, path)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if action != fleetMCPRepaired {
		t.Fatalf("first boot against the incident config: got %q, want %q", action, fleetMCPRepaired)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	for _, cwd := range []string{
		"/Users/marcelo/work/github.com/marcelocantos/claudia",
		"/Users/marcelo/.jevons/jevons",
		"/tmp",
	} {
		scope, entry, err := mcpscope.FindScope(data, mcpscope.ServerName, cwd)
		if err != nil {
			t.Fatalf("FindScope(%s): %v", cwd, err)
		}
		if scope != mcpscope.ScopeUser {
			t.Errorf("after boot, a session in %s sees scope %q, want %q — this is the 🎯T464 failure", cwd, scope, mcpscope.ScopeUser)
		}
		if entry.URL != mcpscope.DefaultEndpoint {
			t.Errorf("after boot, %s registers %q, want %q", cwd, entry.URL, mcpscope.DefaultEndpoint)
		}
	}

	// Second boot must not rewrite a file the whole fleet reads (🎯T376).
	action, err = ensureFleetMCPUserScopeAt(cfg, "127.0.0.1", 13705, path)
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if action != fleetMCPCurrent {
		t.Errorf("second boot: got %q, want %q — a correct config must not be rewritten", action, fleetMCPCurrent)
	}
}

// TestT464IsolateNeverWritesTheDailyRegistration is the control that keeps the
// fix from causing the fault it prevents. A journey isolate serves a throwaway
// port; if it wrote user scope, every Claude agent in the fleet would inherit
// a registration for a port that dies with the suite — 🎯T379's silent dead
// registration, introduced by 🎯T464's own repair.
func TestT464IsolateNeverWritesTheDailyRegistration(t *testing.T) {
	path := fixtureConfig(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	isolate := config.Default()
	isolate.StateDir = t.TempDir() // what the journey suite asserts about itself
	isolate.MCPServerName = "jevonsmcp-journey"

	action, err := ensureFleetMCPUserScopeAt(isolate, "127.0.0.1", 13799, path)
	if err != nil {
		t.Fatalf("ensure from isolate: %v", err)
	}
	if action != fleetMCPSkipped {
		t.Fatalf("isolate on a throwaway port: got %q, want %q", action, fleetMCPSkipped)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(after) != string(before) {
		t.Fatal("a journey isolate wrote the shared Claude config")
	}
	if scope, _, _ := mcpscope.FindScope(after, "jevonsmcp-journey", "/anywhere"); scope != mcpscope.ScopeNone {
		t.Errorf("isolate registered %q in user scope (%q); it would outlive the suite pointing at a dead port", "jevonsmcp-journey", scope)
	}

	// The control: the same call from the daily state dir DOES write, so the
	// assertion above is about the guard and not about a no-op function.
	daily := config.Default()
	if action, err := ensureFleetMCPUserScopeAt(daily, "127.0.0.1", 13705, path); err != nil || action != fleetMCPRepaired {
		t.Fatalf("control: daily boot got %q (err %v), want %q", action, err, fleetMCPRepaired)
	}
}

// TestT464DailyStateDirIsExact pins the discriminator. A sibling or a
// subdirectory of ~/.jevons is not the daily universe, and treating one as
// daily would let a sandbox rewrite the fleet's registration.
func TestT464DailyStateDirIsExact(t *testing.T) {
	daily := config.Default().StateDir
	cases := []struct {
		dir  string
		want bool
	}{
		{daily, true},
		{daily + "/", true},
		{filepath.Join(daily, "journey"), false},
		{daily + "-journey", false},
		{t.TempDir(), false},
		{"", false},
	}
	for _, c := range cases {
		if got := isDailyStateDir(c.dir); got != c.want {
			t.Errorf("isDailyStateDir(%q) = %v, want %v", c.dir, got, c.want)
		}
	}
}
