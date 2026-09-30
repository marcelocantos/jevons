// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// t969Hub is a jevons MCP server on /mcp with a fleet-intent store and the
// agent tools registered, as the daemon runs it.
func t969Hub(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	s := New(dir, nil, nil)
	store, err := fleetintent.Open(filepath.Join(dir, "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	s.SetRegistry(reg)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return s, ts
}

// t969Call posts one JSON-RPC request with the given User-Agent ("" sends
// none) over client, and returns the response body.
func t969Call(t *testing.T, client *http.Client, url, ua string, msg map[string]any) string {
	t.Helper()
	msg["jsonrpc"] = "2.0"
	if _, ok := msg["id"]; !ok {
		msg["id"] = 1
	}
	body, _ := json.Marshal(msg)
	req, err := http.NewRequest(http.MethodPost, url+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("User-Agent", ua)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%v: HTTP %d: %s", msg["method"], resp.StatusCode, out)
	}
	return string(out)
}

func t969SetFleet(args map[string]any) map[string]any {
	return map[string]any{"method": "tools/call", "params": map[string]any{
		"name": "jevons_fleet_intent", "arguments": args,
	}}
}

// 🎯T969: on 2026-09-30 the owner's Claude Code session un-paused the fleet
// without passing actor, the change was recorded "by jevons", and jevons-po
// confessed in five stop reasons to an act nobody in the fleet had taken.
// An actor-less change is recorded as the caller the request identifies, or
// "unattributed" — never as the overseer.
func TestT969ActorlessIntentIsNeverRecordedAsTheOverseer(t *testing.T) {
	s, ts := t969Hub(t)
	by := func() string { return s.fleetIntent().Fleet.By }

	// An MCP client that introduced itself at initialize.
	client := &http.Client{}
	t969Call(t, client, ts.URL, "claude-code/2.0.14", map[string]any{"method": "initialize", "params": map[string]any{
		"protocolVersion": "2025-03-26", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "claude-code", "version": "2.0.14"},
	}})
	out := t969Call(t, client, ts.URL, "claude-code/2.0.14", t969SetFleet(map[string]any{"state": "parked", "reason": "owner paused"}))
	if got := by(); got != "client:claude-code" {
		t.Fatalf("recorded by %q, want client:claude-code\n%s", got, out)
	}

	// A client known only by its User-Agent.
	t969Call(t, &http.Client{}, ts.URL, "curl/8.7.1", t969SetFleet(map[string]any{"state": "working", "reason": "resume"}))
	if got := by(); got != "client:curl" {
		t.Fatalf("recorded by %q, want client:curl", got)
	}

	// Nothing identifies the caller.
	t969Call(t, &http.Client{}, ts.URL, "", t969SetFleet(map[string]any{"state": "parked", "reason": "who?"}))
	if got := by(); got != UnattributedActor {
		t.Fatalf("recorded by %q, want %s", got, UnattributedActor)
	}

	// An explicit actor is recorded verbatim.
	t969Call(t, &http.Client{}, ts.URL, "", t969SetFleet(map[string]any{"state": "working", "actor": "owner", "reason": "owner resumed"}))
	if got := by(); got != "owner" {
		t.Fatalf("recorded by %q, want owner", got)
	}
	if s.overseerName() == "owner" || s.overseerName() == UnattributedActor {
		t.Fatal("fixture cannot tell the overseer apart")
	}
}
