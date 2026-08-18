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
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpscope"
)

// incidentConfig writes a ~/.claude.json in the shape the 2026-08-18 incident
// left it: user scope carrying a dead throwaway port for jevonsmcp while the
// jevons project scope carries the correct daily endpoint. Every Claude mint
// outside the repo inherited the dead row and warned about it.
func incidentConfig(t *testing.T) string {
	t.Helper()
	doc := map[string]any{
		"numStartups": 41,
		"mcpServers": map[string]any{
			"bullseye":          map[string]any{"command": "/opt/homebrew/bin/mcpbridge"},
			mcpscope.ServerName: map[string]any{"type": "http", "url": "http://127.0.0.1:54558/mcp"},
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

// stubExecMCPRegister replaces the provider-CLI process boundary for the
// duration of a test, recording every invocation. This is the 🎯T419 seam:
// the tests below drive the PRODUCTION registration chain
// (registerMCPEndpointsAt → ensureOverseerMCPServer → ensure*MCPServer), and
// a regressed guard shows up here as a recorded exec instead of as a write to
// the owner's real ~/.claude.json or ~/.grok/config.toml.
func stubExecMCPRegister(t *testing.T) *[][]string {
	t.Helper()
	var calls [][]string
	orig := execMCPRegister
	execMCPRegister = func(bin string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{bin}, args...))
		return nil, nil
	}
	t.Cleanup(func() { execMCPRegister = orig })
	return &calls
}

// TestT503DailyBootCorrectsStaleUserScope replays the leak through the
// production call site: user scope is seeded with a DEAD throwaway port, and
// a daily boot must correct it to the live served endpoint — not merely fill
// absent entries. The 🎯T464 mechanism could always do this; what 🎯T503 adds
// is that boot actually reaches it, so the assertion drives
// registerMCPEndpointsAt, the function main() calls, not the helper the old
// suite tested green while nothing called it.
func TestT503DailyBootCorrectsStaleUserScope(t *testing.T) {
	stubExecMCPRegister(t)
	path := incidentConfig(t)
	cfg := config.Default() // daily state dir, daily MCP name

	// Grok overseer: the daily universe's real provider, whose ensure path
	// never touches ~/.claude.json — which is exactly why the stale row
	// survived every daily bounce before the fleet ensure was wired in.
	registerMCPEndpointsAt(cfg, "127.0.0.1", 13705, claudia.ProviderGrok, path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	scope, entry, err := mcpscope.FindScope(data, mcpscope.ServerName, "/anywhere")
	if err != nil {
		t.Fatalf("FindScope: %v", err)
	}
	if scope != mcpscope.ScopeUser {
		t.Fatalf("after daily boot, scope = %q, want %q", scope, mcpscope.ScopeUser)
	}
	if entry.URL != mcpscope.DefaultEndpoint {
		t.Errorf("after daily boot, user scope registers %q, want %q — the stale row was not corrected (🎯T503)",
			entry.URL, mcpscope.DefaultEndpoint)
	}

	// Steady state: a second boot must not rewrite the hot file (🎯T376).
	before, _ := os.ReadFile(path)
	registerMCPEndpointsAt(cfg, "127.0.0.1", 13705, claudia.ProviderGrok, path)
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("second daily boot rewrote an already-correct config")
	}
}

// TestT503IsolateBootNeverTouchesUserScope is the control (🎯T379): a
// throwaway daemon booting through the same production chain — including with
// a CLAUDE overseer, the unguarded path that leaked 54558 in the first place —
// leaves user scope byte-identical and never reaches the provider CLI.
func TestT503IsolateBootNeverTouchesUserScope(t *testing.T) {
	calls := stubExecMCPRegister(t)
	path := incidentConfig(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	isolate := config.Default()
	isolate.StateDir = t.TempDir() // what the journey suite asserts about itself
	isolate.MCPServerName = "jevonsmcp-journey"

	registerMCPEndpointsAt(isolate, "127.0.0.1", 54999, claudia.ProviderClaude, path)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(after) != string(before) {
		t.Fatal("an isolate boot wrote the shared Claude config (🎯T503/🎯T379)")
	}
	if len(*calls) != 0 {
		t.Fatalf("an isolate's Claude overseer ensure reached the provider CLI: %v — this is the exec that wrote jevonsmcp=54558 into user scope", *calls)
	}

	// Control for the control: the same chain from the daily state dir DOES
	// repair, so the assertions above are about the guards, not a no-op.
	daily := config.Default()
	registerMCPEndpointsAt(daily, "127.0.0.1", 13705, claudia.ProviderGrok, path)
	repaired, _ := os.ReadFile(path)
	if _, entry, _ := mcpscope.FindScope(repaired, mcpscope.ServerName, "/anywhere"); entry.URL != mcpscope.DefaultEndpoint {
		t.Fatalf("control: daily boot left user scope at %q, want %q", entry.URL, mcpscope.DefaultEndpoint)
	}
}

// TestT503ClaudeOverseerEnsureRefusesIsolate pins the guard at the leaking
// function itself: ensureClaudeMCPServer from a non-daily state dir refuses
// before the process boundary, with an error that names why.
func TestT503ClaudeOverseerEnsureRefusesIsolate(t *testing.T) {
	calls := stubExecMCPRegister(t)
	isolate := config.Default()
	isolate.StateDir = t.TempDir()

	err := ensureClaudeMCPServer(isolate, "127.0.0.1", 54999)
	if err == nil {
		t.Fatal("ensureClaudeMCPServer from an isolate state dir succeeded; it must refuse (🎯T503/🎯T379)")
	}
	if len(*calls) != 0 {
		t.Fatalf("the refusal came after reaching the provider CLI: %v", *calls)
	}
}

// TestT503BootWiresRegisterMCPEndpoints is the reachability attestation
// (🎯T419): main() itself contains the call to registerMCPEndpoints. The
// 🎯T464 ensure was built, tested green, and never called from boot — the
// tests attested the function while production never ran it, and the stale
// user-scope row survived months of daily bounces. This test fails the moment
// the wiring is removed again, however green the helpers stay.
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
			t.Fatal("main() does not call registerMCPEndpoints: the user-scope fleet registration is unreachable from boot — the exact 🎯T503 regression")
		}
		return
	}
	t.Fatal("no func main in main.go")
}
