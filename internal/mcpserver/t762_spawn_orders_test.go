// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/spawnorder"
)

// t762Server is a server journalling to a real events.jsonl, with the
// reconciler reading that same file back — the production shape.
func t762Server(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	path := eventlog.DefaultPath(dir)
	j, err := eventlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	s := &Server{stateDir: dir}
	s.SetEventJournal(j)
	s.eventLogTail = func(opt eventlog.TailOptions) ([]eventlog.Event, string, error) {
		ev, err := eventlog.Tail(path, opt)
		return ev, path, err
	}
	return s
}

func t762Call(t *testing.T, s *Server, args map[string]any) string {
	t.Helper()
	var req mcp.CallToolRequest
	req.Params.Arguments = args
	res, err := s.handleSpawnOrder(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].(mcp.TextContent).Text
	if res.IsError {
		t.Fatalf("jevons_spawn_order %v: %s", args, text)
	}
	return text
}

// t762Start drives a start through the production observer, with h standing
// in for (or being) the start handler.
func t762Start(t *testing.T, s *Server, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) *mcp.CallToolResult {
	t.Helper()
	var req mcp.CallToolRequest
	req.Params.Arguments = args
	res, err := s.observeSpawnOrderStart(h)(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// mintOK stands in for a start whose Launch succeeded (a real Launch needs a
// provider process, which a hermetic test cannot have).
func mintOK(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText("started"), nil
}

// 🎯T762 acceptance 3 (hermetic): a PO-shaped order naming a claude seat and
// three grok seats; only the claude mint path succeeds. Starts pass through
// the production jevons_agent_start observer. One grok seat is refused by the
// real start handler with the order id; one is started without the order id
// (unattributable); one is never attempted at all — the 2026-09-21 incident.
// The surviving record and the PO's /api/agents decoration name each.
func TestT762HalfExecutedOrderNamesTheDroppedSeats(t *testing.T) {
	s := t762Server(t)
	out := t762Call(t, s, map[string]any{
		"action": "declare", "id": "o-0635", "parent": "jevons-po", "actor": "supervisor",
		"seats": "jv-t749-delivery:claude:T749, jv-t760-attrib:grok:T760, jv-t755-loose:grok:T755, jv-t759-dropped:grok:T759",
	})
	if !strings.Contains(out, "must pass order_id=o-0635") {
		t.Fatalf("declare does not tell the issuer how to attribute starts: %s", out)
	}

	t762Start(t, s, mintOK, map[string]any{"name": "jv-t749-delivery", "provider": "claude", "target_id": "T749", "order_id": "o-0635"})
	if res := t762Start(t, s, s.handleAgentStart, map[string]any{"name": "jv-t760-attrib", "provider": "grok", "target_id": "T760", "order_id": "o-0635"}); !res.IsError {
		t.Fatal("start without workdir must be refused")
	}
	t762Start(t, s, mintOK, map[string]any{"name": "jv-t755-loose", "provider": "grok", "target_id": "T755"})
	s.logLifecycle(compAgentLifecycle, "start", "ok", map[string]any{"name": "jv-t755-loose", "provider": "grok", "target_id": "T755"})

	out = t762Call(t, s, map[string]any{"action": "status", "id": "o-0635"})
	for _, frag := range []string{
		"order o-0635: 1/4 minted (INCOMPLETE)",
		"jv-t760-attrib (grok) refused: name and workdir are required",
		"jv-t755-loose (grok) unknown: a daemon start for this name was observed",
		"jv-t759-dropped (grok) not_attempted: no matching daemon start observed",
	} {
		if !strings.Contains(out, frag) {
			t.Errorf("status lacks %q:\n%s", frag, out)
		}
	}

	// Acceptance 4: a cold server with only the stored order and the journal
	// gives the panel the same reading.
	cold := &Server{stateDir: s.stateDir, eventLogTail: s.eventLogTail}
	lines, err := cold.SpawnOrderLines("jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "INCOMPLETE") || !strings.Contains(lines[0], "jv-t759-dropped (grok) not_attempted") {
		t.Fatalf("panel lines = %q", lines)
	}
	if got, err := cold.SpawnOrderLines("someone-else"); got != nil || err != nil {
		t.Fatalf("another parent's row decorated: %q", got)
	}

	// Closing the order retires it from the panel.
	t762Call(t, s, map[string]any{"action": "close", "id": "o-0635", "note": "grok half re-ordered"})
	if got, err := cold.SpawnOrderLines("jevons-po"); got != nil || err != nil {
		t.Fatalf("closed order still on the panel: %q", got)
	}
}

// Acceptance 2 converse: an order whose every seat minted with its order id
// reads complete, so the panel distinguishes it from the half-done one above.
func TestT762CompleteOrderReadsComplete(t *testing.T) {
	s := t762Server(t)
	t762Call(t, s, map[string]any{"action": "declare", "id": "o-full", "parent": "jevons-po", "seats": "a:claude, b:grok"})
	t762Start(t, s, mintOK, map[string]any{"name": "a", "provider": "claude", "order_id": "o-full"})
	t762Start(t, s, mintOK, map[string]any{"name": "b", "provider": "grok", "order_id": "o-full"})
	lines, err := s.SpawnOrderLines("jevons-po")
	if err != nil || len(lines) != 1 || lines[0] != "order o-full: 2/2 minted (complete)" {
		t.Fatalf("lines = %q", lines)
	}
}

// A start without order_id passes through the observer untouched and
// journals nothing correlated.
func TestT762ObserverIgnoresUnorderedStarts(t *testing.T) {
	s := t762Server(t)
	t762Start(t, s, mintOK, map[string]any{"name": "plain"})
	ev, _, err := s.eventLogTail(eventlog.TailOptions{Component: spawnorder.JournalComponent, Decision: spawnorder.JournalStart})
	if err != nil || len(ev) != 0 {
		t.Fatalf("unordered start journalled as correlated: %+v %v", ev, err)
	}
}

// The declaration itself is journalled, so the eventlog names every ordered
// seat even before any start is attempted.
func TestT762DeclareIsJournalled(t *testing.T) {
	s := t762Server(t)
	t762Call(t, s, map[string]any{"action": "declare", "id": "o-j", "parent": "jevons-po", "seats": "x:grok"})
	ev, _, err := s.eventLogTail(eventlog.TailOptions{Component: "spawn_order", Decision: "declare"})
	if err != nil || len(ev) != 1 || ev[0].Fields["seats"] != "x:grok" {
		t.Fatalf("journal = %+v %v", ev, err)
	}
}

// Review finding 1: concurrent jevons_spawn_order handlers, each opening its
// own Store on the one state file, lose no declaration and no closure.
func TestT762ConcurrentHandlersPersistEveryOrder(t *testing.T) {
	s := t762Server(t)
	const n = 30
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("o-c%02d", i)
			var req mcp.CallToolRequest
			req.Params.Arguments = map[string]any{"action": "declare", "id": id, "parent": "jevons-po", "seats": fmt.Sprintf("jv-c%02d:grok", i)}
			if res, err := s.handleSpawnOrder(context.Background(), req); err != nil || res.IsError {
				t.Errorf("declare %s: %v %+v", id, err, res)
				return
			}
			if i%3 == 0 {
				req.Params.Arguments = map[string]any{"action": "close", "id": id}
				if res, err := s.handleSpawnOrder(context.Background(), req); err != nil || res.IsError {
					t.Errorf("close %s: %v %+v", id, err, res)
				}
			}
		}(i)
	}
	wg.Wait()
	store, err := spawnorder.Open(spawnorder.DefaultPath(s.stateDir))
	if err != nil {
		t.Fatal(err)
	}
	orders, err := store.Orders()
	if err != nil {
		t.Fatal(err)
	}
	closed := 0
	for _, o := range orders {
		if o.Closed {
			closed++
		}
	}
	if len(orders) != n || closed != n/3 {
		t.Fatalf("orders = %d (closed %d), want %d (closed %d)", len(orders), closed, n, n/3)
	}
	lines, err := s.SpawnOrderLines("jevons-po")
	if err != nil || len(lines) != n-n/3 {
		t.Fatalf("panel lines = %d, err %v; want %d", len(lines), err, n-n/3)
	}
}

// Review finding 2: malformed order state is a visible error on the panel
// path and the tool, never "no orders".
func TestT762MalformedStateIsAnError(t *testing.T) {
	s := t762Server(t)
	if err := os.WriteFile(spawnorder.DefaultPath(s.stateDir), []byte("{truncated"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := s.SpawnOrderLines("jevons-po")
	if err == nil || !strings.Contains(err.Error(), "malformed") || lines != nil {
		t.Fatalf("SpawnOrderLines on malformed state = %q, %v", lines, err)
	}
	var req mcp.CallToolRequest
	req.Params.Arguments = map[string]any{"action": "status"}
	res, _ := s.handleSpawnOrder(context.Background(), req)
	if res == nil || !res.IsError {
		t.Fatal("status on malformed state did not error")
	}
}

// Review finding 3: with no readable journal, an unstarted seat is unknown,
// not "no jevons_agent_start was made".
func TestT762NoJournalReadsUnknown(t *testing.T) {
	s := t762Server(t)
	t762Call(t, s, map[string]any{"action": "declare", "id": "o-nj", "parent": "jevons-po", "seats": "x:grok"})
	blind := &Server{stateDir: s.stateDir}
	lines, err := blind.SpawnOrderLines("jevons-po")
	if err != nil || len(lines) != 1 || !strings.Contains(lines[0], "x (grok) unknown: start journal unreadable") {
		t.Fatalf("lines = %q, err %v", lines, err)
	}
	failing := &Server{stateDir: s.stateDir, eventLogTail: func(eventlog.TailOptions) ([]eventlog.Event, string, error) {
		return nil, "", fmt.Errorf("disk on fire")
	}}
	lines, err = failing.SpawnOrderLines("jevons-po")
	if err != nil || len(lines) != 1 || !strings.Contains(lines[0], "unknown: start journal unreadable: disk on fire") {
		t.Fatalf("lines = %q, err %v", lines, err)
	}
}

func TestT762OrderIDThroughMCPDispatch(t *testing.T) {
	s := t762Server(t)
	s.mcpSrv = mcpserver.NewMCPServer("t762", "test")
	s.SetRegistry(nil) // Real registration and handler; no provider process.
	s.registerSpawnOrderTools()
	call := func(message string) map[string]any {
		t.Helper()
		wire, err := json.Marshal(s.mcpSrv.HandleMessage(context.Background(), json.RawMessage(message)))
		if err != nil {
			t.Fatal(err)
		}
		var response map[string]any
		if err := json.Unmarshal(wire, &response); err != nil {
			t.Fatal(err)
		}
		if response["error"] != nil {
			t.Fatalf("RPC error: %s", wire)
		}
		return response
	}
	listed := call(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	found := false
	for _, item := range listed["result"].(map[string]any)["tools"].([]any) {
		tool := item.(map[string]any)
		if tool["name"] != "jevons_agent_start" {
			continue
		}
		props := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
		arg, ok := props["order_id"].(map[string]any)
		if !ok || arg["type"] != "string" {
			t.Fatalf("order_id schema: %v", props["order_id"])
		}
		found = true
	}
	if !found {
		t.Fatal("start tool absent")
	}
	call(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"jevons_spawn_order","arguments":{"action":"declare","id":"o-wire","parent":"jevons-po","seats":"x:grok:T762"}}}`)
	response := call(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"jevons_agent_start","arguments":{"name":"x","workdir":"/tmp","target_id":"T762","order_id":"o-wire"}}}`)
	if response["result"].(map[string]any)["isError"] != true {
		t.Fatal("expected registry refusal")
	}
	status := call(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"jevons_spawn_order","arguments":{"action":"status","id":"o-wire"}}}`)
	wire, _ := json.Marshal(status)
	if !strings.Contains(string(wire), "refused") || !strings.Contains(string(wire), "no agent registry") {
		t.Fatalf("dispatch lost attribution: %s", wire)
	}
}
