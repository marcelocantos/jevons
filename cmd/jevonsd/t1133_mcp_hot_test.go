// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpattach"
)

// 🎯T1133: stdio arg edits on a watched owner map apply on the next mint
// without a config bounce (watchMCPOwnerMap never calls bounceForConfig).
func TestT1133MCPOwnerMapArgEditHotWithoutBounce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")
	body := `{"mcpServers":{"tool-a":{"command":"/bin/echo","args":["one"]}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	load := &claudia.LoadMCPArgs{ClaudeJSON: path, WorkDir: dir}
	inv, err := claudia.LoadMCP(load)
	if err != nil {
		t.Fatal(err)
	}
	w := config.NewWatcher(nil)
	skip := map[string]bool{"jevonsmcp": true}
	watchMCPOwnerMap(w, load, inv, nil, skip, nil)

	t574write(t, path, `{"mcpServers":{"tool-a":{"command":"/bin/echo","args":["two"]}}}`)
	w.Poll()

	list := mcpattach.SessionServers(mcpattach.Args{
		Name:        "jevonsmcp",
		URL:         "http://127.0.0.1:13705/mcp",
		ClaudeJSON:  path,
	}, dir)
	var got []string
	for _, s := range list {
		if s.Name == "tool-a" {
			got = s.Args
			break
		}
	}
	if len(got) != 1 || got[0] != "two" {
		t.Fatalf("mint after arg edit = %v, want [two]", got)
	}
}

func TestT1133MCPOwnerMapChangeSummary(t *testing.T) {
	skip := map[string]bool{"jevonsmcp": true}
	prev := &claudia.MCPInventory{Servers: []claudia.MCPServer{
		{Name: "a", Type: "stdio", Command: "c", Args: []string{"1"}},
		{Name: "jevonsmcp", Type: "http", URL: "http://old"},
	}}
	next := &claudia.MCPInventory{Servers: []claudia.MCPServer{
		{Name: "a", Type: "stdio", Command: "c", Args: []string{"2"}},
		{Name: "b", Type: "http", URL: "http://new"},
		{Name: "jevonsmcp", Type: "http", URL: "http://new-fleet"},
	}}
	ch := mcpMapDiff(prev, next, skip)
	if ch.summary() != "added b; changed a" && ch.summary() != "changed a; added b" {
		t.Fatalf("summary = %q", ch.summary())
	}
	if strings.Contains(ch.summary(), "jevonsmcp") {
		t.Fatalf("jevonsmcp must be excluded from owner diff: %q", ch.summary())
	}
}
