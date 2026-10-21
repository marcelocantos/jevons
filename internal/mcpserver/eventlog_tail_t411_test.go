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

// hydrateBurstFixture builds the 🎯T411 incident shape: sparse server
// decisions interleaved with pagination chatter, with the hydrate burst
// NEWEST — so an unfiltered default tail is consumed by browser debug
// (the overseer saw 40 consecutive hydrate_page rows and count=0 for
// q=jevons-po). Production jevons_logs_tail defaults source=server and
// Collect-walks Page, so those older server rows stay in the window.
func hydrateBurstFixture() []eventlog.Event {
	servers := []eventlog.Event{
		{TS: "2026-09-27T10:00:00Z", Source: "server", Component: "sentinel", Decision: "cycle", Msg: "sentinel cycle ok", Level: "info"},
		{TS: "2026-09-27T10:01:00Z", Source: "server", Component: "idle_nudge", Decision: "impatience", Msg: "idle_nudge outcome=nudged", Level: "info"},
		{TS: "2026-09-27T10:02:00Z", Source: "server", Component: "agent_lifecycle", Decision: "start", Msg: "agent_lifecycle start jv-t411-worker", Level: "info"},
		{TS: "2026-09-27T10:03:00Z", Source: "server", Component: "route", Decision: "send", Msg: "route send jevons-po delivered", Level: "info"},
	}
	var events []eventlog.Event
	burst := 80
	for i, srv := range servers {
		events = append(events, srv)
		for j := 0; j < burst; j++ {
			events = append(events, eventlog.Event{
				TS:        fmt.Sprintf("2026-09-27T10:%02d:%02dZ", 4+i, j%60),
				Source:    "browser",
				Component: "history",
				Decision:  "hydrate_page",
				Level:     "debug",
				Msg:       "hydrate page",
			})
		}
	}
	// Newest rows: another burst so unfiltered limit=40 is 40 hydrate_page.
	for j := 0; j < 200; j++ {
		events = append(events, eventlog.Event{
			TS:        fmt.Sprintf("2026-09-27T10:10:%02dZ", j%60),
			Source:    "browser",
			Component: "history",
			Decision:  "hydrate_page",
			Level:     "debug",
			Msg:       "hydrate page",
		})
	}
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

// productionLogsTailer is the daemon wiring: Collect, not a full-file Tail.
func productionLogsTailer(path string) EventLogTailFunc {
	return func(opt eventlog.TailOptions) ([]eventlog.Event, string, error) {
		evs, err := eventlog.Collect(path, opt)
		return evs, path, err
	}
}

func decodeLogsTail(t *testing.T, res *mcp.CallToolResult) (int, []eventlog.Event) {
	t.Helper()
	var payload struct {
		Count  int              `json:"count"`
		Events []eventlog.Event `json:"events"`
	}
	if err := json.Unmarshal([]byte(toolText(res)), &payload); err != nil {
		t.Fatalf("decode: %v body=%s", err, toolText(res))
	}
	return payload.Count, payload.Events
}

// 🎯T411 clause 1/2: a default jevons_logs_tail call (no source filter, the
// production limit=100) must surface the sparse server decisions rather than
// being consumed by the browser hydrate burst that dwarfs them numerically.
func TestJevonsLogsTailDefaultSourceServerUnderHydrateBurst(t *testing.T) {
	fixture := hydrateBurstFixture()
	path := writeEventlogFixture(t, fixture)

	// Red against the pre-fix tree: unfiltered Collect (no source=server
	// default) of the newest 40 rows is the incident — all hydrate_page.
	raw, err := eventlog.Collect(path, eventlog.TailOptions{Limit: 40})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 40 {
		t.Fatalf("unfiltered newest-40 count=%d", len(raw))
	}
	for _, ev := range raw {
		if ev.Source != "browser" || ev.Decision != "hydrate_page" {
			t.Fatalf("fixture does not reproduce the incident (newest rows must be hydrate): %+v", ev)
		}
	}

	s := New(t.TempDir(), nil, nil)
	s.SetEventLogTailer(productionLogsTailer(path))

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"limit": float64(100)} // no source= — the default call
	res, err := s.handleLogsTail(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("handleLogsTail: err=%v res=%v", err, toolText(res))
	}
	count, events := decodeLogsTail(t, res)
	if count == 0 {
		t.Fatalf("default tail returned zero events; window consumed by browser chatter")
	}
	sawSentinel, sawIdleNudge, sawLifecycle, sawRoute := false, false, false, false
	for _, ev := range events {
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
			sawSentinel, sawIdleNudge, sawLifecycle, sawRoute, events)
	}
}

// 🎯T411 clause 3: q= filtering still finds a server row inside the retained
// window instead of returning count=0 because the window was consumed by
// browser chatter — the concrete failure the overseer hit trying to confirm
// a jevons-po delivery.
func TestJevonsLogsTailFilterFindsServerRowInsideRetainedWindow(t *testing.T) {
	path := writeEventlogFixture(t, hydrateBurstFixture())

	s := New(t.TempDir(), nil, nil)
	s.SetEventLogTailer(productionLogsTailer(path))

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"q": "jevons-po"}
	res, err := s.handleLogsTail(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("handleLogsTail: err=%v res=%v", err, toolText(res))
	}
	count, events := decodeLogsTail(t, res)
	if count != 1 || !strings.Contains(events[0].Msg, "jevons-po") {
		t.Fatalf("q=jevons-po filter: count=%d events=%+v", count, events)
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
	s.SetEventLogTailer(productionLogsTailer(path))

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"source": "browser", "limit": float64(2000)}
	res, err := s.handleLogsTail(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("handleLogsTail: err=%v res=%v", err, toolText(res))
	}
	count, _ := decodeLogsTail(t, res)
	wantBrowser := 0
	for _, ev := range fixture {
		if ev.Source == "browser" {
			wantBrowser++
		}
	}
	if count != wantBrowser {
		t.Fatalf("source=browser count=%d want %d — browser telemetry must remain queryable, not dropped", count, wantBrowser)
	}

	req2 := mcp.CallToolRequest{}
	req2.Params.Arguments = map[string]any{"source": "all", "limit": float64(2000)}
	res2, err := s.handleLogsTail(context.Background(), req2)
	if err != nil || res2.IsError {
		t.Fatalf("handleLogsTail(all): err=%v res=%v", err, toolText(res2))
	}
	count2, _ := decodeLogsTail(t, res2)
	if count2 != len(fixture) {
		t.Fatalf("source=all count=%d want %d", count2, len(fixture))
	}
}
