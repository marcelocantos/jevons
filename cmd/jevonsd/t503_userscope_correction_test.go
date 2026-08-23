// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpattach"
	"github.com/marcelocantos/jevons/internal/mcpscope"
)

func incidentAttach(t *testing.T) mcpattach.Args {
	t.Helper()
	dir := t.TempDir()
	doc := map[string]any{
		"numStartups": 41,
		"mcpServers": map[string]any{
			"bullseye":          map[string]any{"command": "/opt/homebrew/bin/mcpbridge"},
			mcpscope.ServerName: map[string]any{"type": "http", "url": "http://127.0.0.1:54558/mcp"},
		},
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(dir, "claude.json")
	if err := os.WriteFile(claude, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return mcpattach.Args{
		Name:       mcpscope.ServerName,
		URL:        mcpscope.DefaultEndpoint,
		ClaudeJSON: claude,
		GrokTOML:   filepath.Join(dir, "grok.toml"),
		CodexTOML:  filepath.Join(dir, "codex.toml"),
		CursorJSON: filepath.Join(dir, "cursor.json"),
	}
}

func TestT503DailyBootScrubsUserScope(t *testing.T) {
	cfg := config.Default()
	a := incidentAttach(t)
	registerMCPEndpointsAt(cfg, "127.0.0.1", 13705, a)
	data, err := os.ReadFile(a.ClaudeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "54558") || strings.Contains(string(data), mcpscope.ServerName) {
		t.Fatalf("stale jevonsmcp survived daily scrub: %s", data)
	}
	scope, _, err := mcpscope.FindScope(data, mcpscope.ServerName, "/anywhere")
	if err != nil {
		t.Fatal(err)
	}
	if scope != mcpscope.ScopeNone {
		t.Fatalf("scope=%q want none", scope)
	}
	if !strings.Contains(string(data), "bullseye") {
		t.Fatalf("scrub dropped unrelated server: %s", data)
	}

	before, _ := os.ReadFile(a.ClaudeJSON)
	registerMCPEndpointsAt(cfg, "127.0.0.1", 13705, a)
	after, _ := os.ReadFile(a.ClaudeJSON)
	if string(before) != string(after) {
		t.Error("second daily boot rewrote an already-clean config")
	}
}

func TestT503IsolateBootNeverTouchesUserScope(t *testing.T) {
	daily := incidentAttach(t)
	before, _ := os.ReadFile(daily.ClaudeJSON)

	isolate := config.Default()
	isolate.StateDir = t.TempDir()
	isolate.MCPServerName = "jevonsmcp-journey"
	a := fleetMCPAttach(isolate, "127.0.0.1", 54999)
	registerMCPEndpointsAt(isolate, "127.0.0.1", 54999, a)

	after, _ := os.ReadFile(daily.ClaudeJSON)
	if string(after) != string(before) {
		t.Fatal("an isolate boot wrote the shared Claude config (🎯T503/🎯T379)")
	}
	if _, err := os.Stat(a.ClaudeJSON); err == nil {
		t.Fatal("isolate boot must not write state_dir/mcp")
	}
}

func TestT503BootWiresRegisterMCPEndpoints(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "main" || fn.Recv != nil {
			continue
		}
		found := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "registerMCPEndpoints" {
					found = true
				}
			}
			return !found
		})
		if !found {
			t.Fatal("main() does not call registerMCPEndpoints")
		}
		return
	}
	t.Fatal("no func main in main.go")
}
