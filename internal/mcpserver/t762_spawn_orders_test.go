// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/eventlog"
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

// 🎯T762 acceptance 3 (hermetic): a PO-shaped order naming a claude seat and
// two grok seats; only the claude mint path succeeds. One grok seat's start
// is refused through the real jevons_agent_start error path, the other is
// never attempted at all — the 2026-09-21 incident. The surviving record, the
// PO's /api/agents decoration, names both seats that never appeared and why.
func TestT762HalfExecutedOrderNamesTheDroppedSeats(t *testing.T) {
	s := t762Server(t)
	t762Call(t, s, map[string]any{
		"action": "declare", "id": "o-0635", "parent": "jevons-po", "actor": "supervisor",
		"seats": "jv-t749-delivery:claude:T749, jv-t760-attrib:grok:T760, jv-t759-dropped:grok:T759",
	})

	// The claude half mints: the daemon journals the ok start it would.
	s.logLifecycle(compAgentLifecycle, "start", "ok", map[string]any{"name": "jv-t749-delivery", "provider": "claude"})
	// One grok seat is attempted and refused by the real start handler.
	var req mcp.CallToolRequest
	req.Params.Arguments = map[string]any{"name": "jv-t760-attrib", "provider": "grok"}
	if res, _ := s.handleAgentStart(context.Background(), req); res == nil || !res.IsError {
		t.Fatal("start without workdir must be refused")
	}
	// jv-t759-dropped: nothing ever asks.

	out := t762Call(t, s, map[string]any{"action": "status", "id": "o-0635"})
	for _, frag := range []string{
		"order o-0635: 1/3 minted (INCOMPLETE)",
		"jv-t760-attrib (grok) refused: name and workdir are required",
		"jv-t759-dropped (grok) not_attempted",
	} {
		if !strings.Contains(out, frag) {
			t.Errorf("status lacks %q:\n%s", frag, out)
		}
	}

	// Acceptance 4: the panel line for the PO carries the same reading, with
	// no memory of the order text beyond what the daemon stored.
	cold := &Server{stateDir: s.stateDir, eventLogTail: s.eventLogTail}
	lines := cold.SpawnOrderLines("jevons-po")
	if len(lines) != 1 || !strings.Contains(lines[0], "INCOMPLETE") || !strings.Contains(lines[0], "jv-t759-dropped (grok) not_attempted") {
		t.Fatalf("panel lines = %q", lines)
	}
	if got := cold.SpawnOrderLines("someone-else"); got != nil {
		t.Fatalf("another parent's row decorated: %q", got)
	}

	// Closing the order retires it from the panel.
	t762Call(t, s, map[string]any{"action": "close", "id": "o-0635", "note": "grok half re-ordered"})
	if got := cold.SpawnOrderLines("jevons-po"); got != nil {
		t.Fatalf("closed order still on the panel: %q", got)
	}
}

// Acceptance 2 converse: an order whose every seat minted reads complete, so
// the panel distinguishes it from the half-done one above.
func TestT762CompleteOrderReadsComplete(t *testing.T) {
	s := t762Server(t)
	t762Call(t, s, map[string]any{"action": "declare", "id": "o-full", "parent": "jevons-po", "seats": "a:claude, b:grok"})
	s.logLifecycle(compAgentLifecycle, "start", "ok", map[string]any{"name": "a", "provider": "claude"})
	s.logLifecycle(compAgentLifecycle, "start", "ok", map[string]any{"name": "b", "provider": "grok"})
	lines := s.SpawnOrderLines("jevons-po")
	if len(lines) != 1 || lines[0] != "order o-full: 2/2 minted (complete)" {
		t.Fatalf("lines = %q", lines)
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
