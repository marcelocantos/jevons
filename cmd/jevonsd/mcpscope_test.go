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
	}
}

func TestT464DailyDaemonEnsuresAllThreeBackends(t *testing.T) {
	cfg := config.Default()
	a := attachFixtures(t, mcpscope.ServerName, mcpscope.DefaultEndpoint)
	got := registerMCPEndpointsAt(cfg, "127.0.0.1", 13705, a)
	raw, err := os.ReadFile(got.ClaudeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "13705/mcp") {
		t.Fatalf("claude config missing live url: %s", raw)
	}
	for _, p := range []string{got.GrokTOML, got.CodexTOML} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "13705/mcp") {
			t.Fatalf("%s missing live url: %s", p, b)
		}
	}
}

func TestT464IsolateNeverWritesTheDailyRegistration(t *testing.T) {
	daily := attachFixtures(t, mcpscope.ServerName, mcpscope.DefaultEndpoint)
	if err := mcpattach.Ensure(daily); err != nil {
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
	iso, err := os.ReadFile(a.ClaudeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(iso), "jevonsmcp-journey") {
		t.Fatalf("isolate should ensure its own name: %s", iso)
	}
	if strings.Contains(string(iso), "jevonsmcp-journey") && strings.Contains(string(after), "jevonsmcp-journey") {
		t.Fatal("isolate name leaked into daily claude json")
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
