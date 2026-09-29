// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/eventlog"
)

// hydrateBurstFixture builds a realistic fixture: a long run of high-frequency
// browser/history/hydrate_page debug rows (as produced by scrollback
// pagination) interleaved with a handful of sparse, load-bearing server
// decisions (sentinel cycle, idle_nudge outcome, agent_lifecycle, send/route)
// — the exact shape from the 🎯T411 incident (98% pagination chatter, zero
// server rows in the default 100-row tail).
func hydrateBurstFixture() []eventlog.Event {
	events := []eventlog.Event{
		{TS: "2026-09-27T10:00:00Z", Source: "server", Component: "sentinel", Decision: "cycle", Msg: "sentinel cycle ok", Level: "info"},
		{TS: "2026-09-27T10:00:01Z", Source: "server", Component: "idle_nudge", Decision: "impatience", Msg: "idle_nudge outcome=nudged", Level: "info"},
	}
	// A big hydrate burst: hundreds of debug rows, one per back-page.
	for i := 0; i < 400; i++ {
		events = append(events, eventlog.Event{
			TS:        fmt.Sprintf("2026-09-27T10:00:%02dZ", 2+(i%57)),
			Source:    "browser",
			Component: "history",
			Decision:  "hydrate_page",
			Level:     "debug",
			Msg:       "hydrate page",
		})
	}
	events = append(events,
		eventlog.Event{TS: "2026-09-27T10:05:00Z", Source: "server", Component: "agent_lifecycle", Decision: "start", Msg: "agent_lifecycle start jv-t411-worker", Level: "info"},
		eventlog.Event{TS: "2026-09-27T10:05:01Z", Source: "server", Component: "route", Decision: "send", Msg: "route send jevons-po delivered", Level: "info"},
	)
	return events
}

func writeEventlogFixture(t *testing.T, events []eventlog.Event) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/events.jsonl"
	j, err := eventlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	for _, ev := range events {
		if err := j.Append(ev); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// 🎯T411 clause 1/2: a default jevons_logs_tail call (no source filter, the
// production limit=100) must surface the sparse server decisions rather than
// being consumed by the browser hydrate burst that dwarfs them numerically.
func TestJevonsLogsTailDefaultSourceServerUnderHydrateBurst(t *testing.T) {
	path := writeEventlogFixture(t, hydrateBurstFixture())

	s := New(t.TempDir(), nil, nil)
	s.SetEventLogTailer(func(opt eventlog.TailOptions) ([]eventlog.Event, string, error) {
		evs, err := eventlog.Tail(path, opt)
		return evs, path, err
	})

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"limit": float64(100)} // no source= — the default call
	res, err := s.handleLogsTail(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("handleLogsTail: err=%v res=%v", err, toolText(res))
	}
	var payload struct {
		Count  int              `json:"count"`
		Events []eventlog.Event `json:"events"`
	}
	if err := json.Unmarshal([]byte(toolText(res)), &payload); err != nil {
		t.Fatalf("decode: %v body=%s", err, toolText(res))
	}
	if payload.Count == 0 {
		t.Fatalf("default tail returned zero events; window consumed by browser chatter")
	}
	sawSentinel, sawIdleNudge, sawLifecycle, sawRoute := false, false, false, false
	for _, ev := range payload.Events {
		if ev.Source != "server" {
			t.Fatalf("default tail leaked a browser row: %+v", ev)
		}
		switch ev.Component {
		case "sentinel":
			sawSentinel = true
		case "idle_nudge":
			sawIdleNudge = true
		case "agent_lifecycle":
			sawLifecycle = true
		case "route":
			sawRoute = true
		}
	}
	if !sawSentinel || !sawIdleNudge || !sawLifecycle || !sawRoute {
		t.Fatalf("default tail missing server decisions: sentinel=%v idle_nudge=%v agent_lifecycle=%v route=%v events=%+v",
			sawSentinel, sawIdleNudge, sawLifecycle, sawRoute, payload.Events)
	}
}

// 🎯T411 clause 3: q= filtering still finds a server row inside the retained
// window instead of returning count=0 because the window was consumed by
// browser chatter — the concrete failure the overseer hit trying to confirm
// a jevons-po delivery.
func TestJevonsLogsTailFilterFindsServerRowInsideRetainedWindow(t *testing.T) {
	path := writeEventlogFixture(t, hydrateBurstFixture())

	s := New(t.TempDir(), nil, nil)
	s.SetEventLogTailer(func(opt eventlog.TailOptions) ([]eventlog.Event, string, error) {
		evs, err := eventlog.Tail(path, opt)
		return evs, path, err
	})

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"q": "jevons-po"}
	res, err := s.handleLogsTail(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("handleLogsTail: err=%v res=%v", err, toolText(res))
	}
	var payload struct {
		Count  int              `json:"count"`
		Events []eventlog.Event `json:"events"`
	}
	if err := json.Unmarshal([]byte(toolText(res)), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Count != 1 || !strings.Contains(payload.Events[0].Msg, "jevons-po") {
		t.Fatalf("q=jevons-po filter: count=%d events=%+v", payload.Count, payload.Events)
	}
}

// 🎯T411 clause 4 (over-broadness control): nothing an operator relies on
// stops being logged. source=browser and source=all must still surface the
// hydrate rows in full — a "fix" that simply drops browser telemetry from
// the durable journal entirely must fail this test even though it would
// also pass the default-tail test above.
func TestJevonsLogsTailBrowserRowsStillRetrievable(t *testing.T) {
	fixture := hydrateBurstFixture()
	path := writeEventlogFixture(t, fixture)

	s := New(t.TempDir(), nil, nil)
	s.SetEventLogTailer(func(opt eventlog.TailOptions) ([]eventlog.Event, string, error) {
		evs, err := eventlog.Tail(path, opt)
		return evs, path, err
	})

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"source": "browser", "limit": float64(2000)}
	res, err := s.handleLogsTail(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("handleLogsTail: err=%v res=%v", err, toolText(res))
	}
	var payload struct {
		Count  int              `json:"count"`
		Events []eventlog.Event `json:"events"`
	}
	if err := json.Unmarshal([]byte(toolText(res)), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	wantBrowser := 0
	for _, ev := range fixture {
		if ev.Source == "browser" {
			wantBrowser++
		}
	}
	if payload.Count != wantBrowser {
		t.Fatalf("source=browser count=%d want %d — browser telemetry must remain queryable, not dropped", payload.Count, wantBrowser)
	}

	// source=all sees everything, server and browser together.
	req2 := mcp.CallToolRequest{}
	req2.Params.Arguments = map[string]any{"source": "all", "limit": float64(2000)}
	res2, err := s.handleLogsTail(context.Background(), req2)
	if err != nil || res2.IsError {
		t.Fatalf("handleLogsTail(all): err=%v res=%v", err, toolText(res2))
	}
	var payload2 struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(toolText(res2)), &payload2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload2.Count != len(fixture) {
		t.Fatalf("source=all count=%d want %d", payload2.Count, len(fixture))
	}
}
