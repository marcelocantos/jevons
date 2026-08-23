// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpattach"
	"github.com/marcelocantos/jevons/internal/mcpscope"
)

func attachFixtures(t *testing.T, name, url string) mcpattach.Args {
	t.Helper()
	dir := t.TempDir()
	return mcpattach.Args{
		Name:       name,
		URL:        url,
		ClaudeJSON: filepath.Join(dir, "claude.json"),
		GrokTOML:   filepath.Join(dir, "grok.toml"),
		CodexTOML:  filepath.Join(dir, "codex.toml"),
		CursorJSON: filepath.Join(dir, "cursor.json"),
	}
}

func TestT464DailyDaemonDoesNotWriteProviderConfigs(t *testing.T) {
	cfg := config.Default()
	a := attachFixtures(t, mcpscope.ServerName, mcpscope.DefaultEndpoint)
	if err := os.WriteFile(a.ClaudeJSON, []byte(`{"mcpServers":{"jevonsmcp":{"url":"http://127.0.0.1:13705/mcp"},"bullseye":{"command":"x"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := registerMCPEndpointsAt(cfg, "127.0.0.1", 13705, a)
	raw, err := os.ReadFile(got.ClaudeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "jevonsmcp") {
		t.Fatalf("daily boot left jevonsmcp in user-scope: %s", raw)
	}
	if !strings.Contains(string(raw), "bullseye") {
		t.Fatalf("daily scrub dropped an unrelated server: %s", raw)
	}
	for _, p := range []string{got.GrokTOML, got.CodexTOML, got.CursorJSON} {
		if _, err := os.Stat(p); err == nil {
			b, _ := os.ReadFile(p)
			if strings.Contains(string(b), "jevonsmcp") {
				t.Fatalf("%s still has jevonsmcp: %s", p, b)
			}
		}
	}
}

func TestT464IsolateNeverWritesTheDailyRegistration(t *testing.T) {
	daily := attachFixtures(t, mcpscope.ServerName, mcpscope.DefaultEndpoint)
	if err := os.WriteFile(daily.ClaudeJSON, []byte(`{"mcpServers":{"jevonsmcp":{"url":"http://127.0.0.1:13705/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(daily.ClaudeJSON)

	isolate := config.Default()
	isolate.StateDir = t.TempDir()
	isolate.MCPServerName = "jevonsmcp-journey"
	a := fleetMCPAttach(isolate, "127.0.0.1", 13799)
	if !a.Isolate || a.ClaudeJSON == daily.ClaudeJSON {
		t.Fatalf("isolate attach must use state_dir files, got %+v", a)
	}
	registerMCPEndpointsAt(isolate, "127.0.0.1", 13799, a)

	after, _ := os.ReadFile(daily.ClaudeJSON)
	if string(after) != string(before) {
		t.Fatal("a journey isolate wrote the daily Claude config")
	}
	if _, err := os.Stat(a.ClaudeJSON); err == nil {
		t.Fatal("isolate boot must not write state_dir/mcp")
	}
	list := mcpattach.SessionServers(a, "claude", "")
	if len(list) != 1 || list[0].Name != "jevonsmcp-journey" {
		t.Fatalf("isolate SessionServers = %+v", list)
	}
}

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
		if got := config.IsDailyStateDir(c.dir); got != c.want {
			t.Errorf("IsDailyStateDir(%q) = %v, want %v", c.dir, got, c.want)
		}
	}
}
