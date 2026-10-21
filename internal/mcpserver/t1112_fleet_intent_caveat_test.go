// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
)

// 🎯T1112 acceptance 3: jevons_fleet_intent on a stopped row says what will
// happen next, so an operator clearing stale intents is not surprised by a
// fleet six minutes later.
func TestHandleFleetIntentNamesRevivalCaveatForStoppedRow(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Registered but never launched: Get(name) is nil, which this harness's
	// seatState reads as not alive — the same shape a 🎯T560-preserved
	// supervisor-killed row leaves behind.
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t1112-stopped", WorkDir: dir, SessionID: "s1", Provider: "grok", AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": "jv-t1112-stopped", "state": "working", "actor": "owner",
	}
	res, err := s.handleFleetIntent(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("handleFleetIntent error: %s", toolText(res))
	}
	got := toolText(res)
	if !strings.Contains(got, "stays stopped until spawned or sent to") {
		t.Fatalf("response missing T1112 revival caveat for a stopped row: %q", got)
	}
}

// A non-working intent (e.g. parked) never needs the revival caveat: the
// row isn't about to come back, so the sentence would be noise.
func TestHandleFleetIntentNoCaveatForNonWorkingState(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t1112-stopped2", WorkDir: dir, SessionID: "s1", Provider: "grok", AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": "jv-t1112-stopped2", "state": "parked", "actor": "owner", "reason": "test",
	}
	res, err := s.handleFleetIntent(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("handleFleetIntent error: %s", toolText(res))
	}
	got := toolText(res)
	if strings.Contains(got, "stays stopped until spawned or sent to") {
		t.Fatalf("non-working intent should not carry the revival caveat: %q", got)
	}
}
