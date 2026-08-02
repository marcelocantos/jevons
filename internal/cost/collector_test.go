// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var testNow = time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)

// assistantLine builds a realistic Claude Code assistant JSONL line with
// usage, as written to ~/.claude/projects/<proj>/<session>.jsonl.
func assistantLine(session, reqID, model string, ts time.Time, u Usage) string {
	return fmt.Sprintf(`{"type":"assistant","sessionId":%q,"requestId":%q,"timestamp":%q,"message":{"id":"msg_%s","model":%q,"role":"assistant","usage":{"input_tokens":%d,"output_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d},"content":[{"type":"text","text":"ok"}]}}`,
		session, reqID, ts.Format(time.RFC3339Nano), reqID, model,
		u.Input, u.Output, u.CacheCreate, u.CacheRead)
}

func TestParseLine(t *testing.T) {
	u := Usage{Input: 100, Output: 200, CacheCreate: 1000, CacheRead: 5000}
	line := assistantLine("sess-1", "req_1", "claude-opus-4-8", testNow, u)
	e := ParseLine([]byte(line), "fallback", testNow)
	if e == nil {
		t.Fatal("assistant line with usage not parsed")
	}
	if e.SessionID != "sess-1" || e.RequestID != "req_1" || e.Model != "claude-opus-4-8" {
		t.Fatalf("bad identity fields: %+v", e)
	}
	// opus: in $15, out $75, cache write $18.75, cache read $1.50 per MTok
	want := (100*15 + 200*75 + 1000*18.75 + 5000*1.5) / 1e6
	if math.Abs(e.CostUSD-want) > 1e-12 {
		t.Fatalf("estimated cost = %v, want %v", e.CostUSD, want)
	}

	// Precomputed costUSD is preferred over the table.
	withCost := `{"type":"assistant","costUSD":0.42,"message":{"id":"m1","model":"claude-opus-4-8","usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`
	e = ParseLine([]byte(withCost), "fb", testNow)
	if e == nil || e.CostUSD != 0.42 {
		t.Fatalf("costUSD not preferred: %+v", e)
	}
	if e.SessionID != "fb" {
		t.Fatalf("fallback session not applied: %q", e.SessionID)
	}
	if e.RequestID != "m1" {
		t.Fatalf("message.id not used as dedup fallback: %q", e.RequestID)
	}
	if !e.Timestamp.Equal(testNow) {
		t.Fatalf("missing timestamp not defaulted to now: %v", e.Timestamp)
	}

	for name, line := range map[string]string{
		"user turn":  `{"type":"user","message":{"role":"user","content":"hi"}}`,
		"zero usage": `{"type":"assistant","message":{"model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
		"corrupt":    `{"type":"assistant","message":{`,
		// T36.1 / Fable F1: adversarial transcript values must not parse.
		"neg costUSD": `{"type":"assistant","sessionId":"attacker","requestId":"x1","costUSD":-100000,"message":{"id":"m","model":"claude-opus-4-8","usage":{"input_tokens":0,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
		"neg tokens":  `{"type":"assistant","sessionId":"attacker","requestId":"x2","message":{"id":"m2","model":"claude-opus-4-8","usage":{"input_tokens":-1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
	} {
		if e := ParseLine([]byte(line), "fb", testNow); e != nil {
			t.Fatalf("%s parsed as billable: %+v", name, e)
		}
	}
}

// TestNegativeCostCannotDeflateSpend is the T36.1 / Fable F1 oracle: a
// fabricated negative-cost row must not cancel real attributed spend in
// SpentUSD / Burning aggregates (belt-and-suspenders at the store layer
// even if a pre-clamp Event is InsertEvents'd directly).
func TestNegativeCostCannotDeflateSpend(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer s.Close()

	// Real burn that must remain visible to the monitor/enforcer.
	real := &Event{
		Timestamp: testNow.Add(-time.Minute),
		SessionID: "real-runaway",
		Worker:    "fleet-w",
		Model:     "claude-opus-4-8",
		Usage:     Usage{Output: 1},
		CostUSD:   50.0,
		RequestID: "real-1",
	}
	// Adversarial unattributed line of the form the audit describes.
	// ParseLine rejects it; InsertEvents also clamps if force-fed.
	poisonLine := `{"type":"assistant","sessionId":"attacker","requestId":"x1","costUSD":-100000,"message":{"id":"m","model":"claude-opus-4-8","usage":{"input_tokens":0,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`
	if e := ParseLine([]byte(poisonLine), "attacker", testNow); e != nil {
		t.Fatalf("ParseLine accepted negative costUSD: %+v", e)
	}
	// Force-insert a poisoned Event the way a buggy path might have.
	poison := &Event{
		Timestamp: testNow.Add(-30 * time.Second),
		SessionID: "attacker",
		Model:     "claude-opus-4-8",
		Usage:     Usage{Output: 1},
		CostUSD:   -100000,
		RequestID: "poison-1",
	}
	if _, err := s.InsertEvents([]*Event{real, poison}); err != nil {
		t.Fatalf("InsertEvents: %v", err)
	}

	from, to := testNow.Add(-time.Hour), testNow.Add(time.Hour)
	spent, err := s.SpentUSD(from, to)
	if err != nil {
		t.Fatalf("SpentUSD: %v", err)
	}
	if spent < 50.0-1e-9 {
		t.Fatalf("SpentUSD deflated to %v; want ≥ 50 (poison must not cancel real spend)", spent)
	}

	burning, err := s.Burning(from, to)
	if err != nil {
		t.Fatalf("Burning: %v", err)
	}
	var realRow, poisonRow *BurnRow
	for i := range burning {
		switch burning[i].SessionID {
		case "real-runaway":
			realRow = &burning[i]
		case "attacker":
			poisonRow = &burning[i]
		}
	}
	if realRow == nil || realRow.CostUSD < 50.0-1e-9 {
		t.Fatalf("real session missing or deflated in Burning: %+v", burning)
	}
	if poisonRow != nil && poisonRow.CostUSD < 0 {
		t.Fatalf("poison session still contributes negative cost in Burning: %+v", poisonRow)
	}
}

func TestPricingFamilies(t *testing.T) {
	u := Usage{Input: 1e6} // 1 MTok input isolates the input rate
	for model, want := range map[string]float64{
		"claude-opus-4-8":            15,
		"claude-sonnet-5":            3,
		"claude-haiku-4-5-20251001":  1,
		"claude-fable-5-unknown-fam": 15, // unknown → conservative (opus) rate
		"grok-4.5-build":             15, // Grok fallback when ticks absent
	} {
		if got := EstimateCostUSD(model, u); math.Abs(got-want) > 1e-9 {
			t.Fatalf("EstimateCostUSD(%s, 1MTok in) = %v, want %v", model, got, want)
		}
	}
}

// grokTurnCompleted builds a realistic Grok Build updates.jsonl line
// (turn_completed with camelCase usage + costUsdTicks).
func grokTurnCompleted(session, promptID, model string, ts time.Time, u Usage, ticks float64) string {
	return fmt.Sprintf(`{"timestamp":%d,"method":"_x.ai/session/update","params":{"sessionId":%q,"update":{"sessionUpdate":"turn_completed","prompt_id":%q,"stop_reason":"end_turn","usage":{"inputTokens":%d,"outputTokens":%d,"cachedReadTokens":%d,"cacheCreationTokens":%d,"costUsdTicks":%g,"modelUsage":{%q:{"inputTokens":%d,"outputTokens":%d,"costUsdTicks":%g}}}}}}`,
		ts.Unix(), session, promptID,
		u.Input, u.Output, u.CacheRead, u.CacheCreate, ticks,
		model, u.Input, u.Output, ticks)
}

func TestParseGrokTurnCompleted(t *testing.T) {
	u := Usage{Input: 1000, Output: 50, CacheRead: 800, CacheCreate: 0}
	ticks := 1.5e9 // $1.50
	line := grokTurnCompleted("sess-g", "prompt-1", "grok-4.5-build", testNow, u, ticks)
	e := ParseLine([]byte(line), "fallback", testNow)
	if e == nil {
		t.Fatal("Grok turn_completed with usage not parsed")
	}
	if e.SessionID != "sess-g" || e.RequestID != "prompt-1" || e.Model != "grok-4.5-build" {
		t.Fatalf("bad identity fields: %+v", e)
	}
	if e.Usage != u {
		t.Fatalf("usage = %+v, want %+v", e.Usage, u)
	}
	if math.Abs(e.CostUSD-1.5) > 1e-12 {
		t.Fatalf("costUsdTicks not converted: got %v want 1.5", e.CostUSD)
	}
	if !e.Timestamp.Equal(time.Unix(testNow.Unix(), 0)) {
		t.Fatalf("unix timestamp not applied: %v", e.Timestamp)
	}

	// Without ticks, fall back to rate table (non-zero for non-zero tokens).
	noTicks := `{"timestamp":1785673000,"method":"_x.ai/session/update","params":{"sessionId":"s2","update":{"sessionUpdate":"turn_completed","prompt_id":"p2","usage":{"inputTokens":1000000,"outputTokens":0,"cachedReadTokens":0,"cacheCreationTokens":0,"modelUsage":{"grok-4.5-build":{}}}}}}`
	e = ParseLine([]byte(noTicks), "fb", testNow)
	if e == nil || e.CostUSD <= 0 {
		t.Fatalf("Grok without ticks must estimate non-zero cost: %+v", e)
	}

	for name, line := range map[string]string{
		"phase only":   `{"timestamp":1,"method":"_x.ai/session/update","params":{"update":{"sessionUpdate":"phase_changed"}}}`,
		"zero no cost": `{"timestamp":1,"method":"_x.ai/session/update","params":{"update":{"sessionUpdate":"turn_completed","prompt_id":"z","usage":{"inputTokens":0,"outputTokens":0,"cachedReadTokens":0,"cacheCreationTokens":0}}}}`,
		"neg ticks":    `{"timestamp":1,"method":"_x.ai/session/update","params":{"sessionId":"a","update":{"sessionUpdate":"turn_completed","prompt_id":"n","usage":{"inputTokens":1,"outputTokens":0,"costUsdTicks":-1e9}}}}`,
	} {
		if e := ParseLine([]byte(line), "fb", testNow); e != nil {
			t.Fatalf("%s parsed as billable: %+v", name, e)
		}
	}
}

func TestSessionIDFromPath(t *testing.T) {
	sid := "019fc265-e39b-7451-9f52-014c6d2ac507"
	got := sessionIDFromPath(filepath.Join("sessions", "enc-cwd", sid, "updates.jsonl"))
	if got != sid {
		t.Fatalf("Grok path session = %q, want %q", got, sid)
	}
	got = sessionIDFromPath(filepath.Join("projects", "-work", sid+".jsonl"))
	if got != sid {
		t.Fatalf("Claude path session = %q, want %q", got, sid)
	}
}

// TestGrokBurnProducesNonZeroRate is the 🎯T117 hermetic oracle: under
// synthetic Grok spend the monitor must report non-zero global+fleet
// USD/hr — permanent zero under real burn is the failure mode.
func TestGrokBurnProducesNonZeroRate(t *testing.T) {
	dir := t.TempDir()
	sid := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	sessDir := filepath.Join(dir, "projects", "enc-cwd", sid)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessDir, "updates.jsonl")
	// Also drop non-billable sidecars the collector must ignore.
	for _, name := range []string{"chat_history.jsonl", "events.jsonl"} {
		if err := os.WriteFile(filepath.Join(sessDir, name), []byte(`{"type":"noise"}\n`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// $3.00 of spend inside the monitor window.
	ticks := 3e9
	u := Usage{Input: 50000, Output: 2000}
	line := grokTurnCompleted(sid, "prompt-burn-1", "grok-4.5-build", testNow.Add(-30*time.Second), u, ticks)
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := OpenStore(filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	c := NewCollector(&CollectorArgs{
		Store:        store,
		ProjectsRoot: filepath.Join(dir, "projects"),
		Attribute: func(sessionID string) string {
			if sessionID == sid {
				return "fleet-worker"
			}
			return ""
		},
		Now: func() time.Time { return testNow },
	})
	// Force-active: ScanOnce uses real mtimes; our Now only affects
	// event stamping. Touch via rewrite already makes mtime ~now, which
	// is inside DefaultActiveWindow of wall clock.
	files, err := c.ScanOnce()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "updates.jsonl" {
		t.Fatalf("ScanOnce should only pick updates.jsonl, got %v", files)
	}
	n, err := c.PollOnce()
	if err != nil || n != 1 {
		t.Fatalf("PollOnce inserted %d, %v; want 1", n, err)
	}

	mon := NewMonitor(&MonitorArgs{
		Store: store,
		Config: func() *BudgetConfig {
			cfg := DefaultBudgetConfig()
			cfg.Window = Duration(time.Hour) // wide window so synthetic ts is in-range
			return cfg
		},
		Now: func() time.Time { return testNow },
	})
	snap, err := mon.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.GlobalUSDPerHour <= 0 {
		t.Fatalf("global_usd_per_hour = %v; synthetic Grok burn must not stay zero", snap.GlobalUSDPerHour)
	}
	if snap.FleetUSDPerHour <= 0 {
		t.Fatalf("fleet_usd_per_hour = %v; attributed burn must not stay zero", snap.FleetUSDPerHour)
	}
	// $3 in a 1h window → $3/hr.
	if math.Abs(snap.GlobalUSDPerHour-3.0) > 1e-9 {
		t.Fatalf("global_usd_per_hour = %v, want 3.0", snap.GlobalUSDPerHour)
	}
	if math.Abs(snap.FleetUSDPerHour-3.0) > 1e-9 {
		t.Fatalf("fleet_usd_per_hour = %v, want 3.0", snap.FleetUSDPerHour)
	}
}

func TestStoreDedupAndWindows(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer s.Close()

	mk := func(sess, req string, ts time.Time, cost float64) *Event {
		return &Event{Timestamp: ts, SessionID: sess, Model: "claude-opus-4-8",
			Usage: Usage{Input: 1}, CostUSD: cost, RequestID: req}
	}
	events := []*Event{
		mk("a", "r1", testNow.Add(-2*time.Minute), 1.0),
		mk("a", "r1", testNow.Add(-2*time.Minute), 1.0), // dup: same request id
		mk("a", "r2", testNow.Add(-1*time.Minute), 2.0),
		mk("b", "r3", testNow.Add(-30*time.Second), 4.0),
		mk("b", "r4", testNow.Add(-2*time.Hour), 100.0), // outside window
	}
	n, err := s.InsertEvents(events)
	if err != nil {
		t.Fatalf("InsertEvents: %v", err)
	}
	if n != 4 {
		t.Fatalf("inserted %d, want 4 (dup dropped)", n)
	}
	// Re-inserting the whole batch is a no-op (idempotent replay).
	if n, _ := s.InsertEvents(events); n != 0 {
		t.Fatalf("replay inserted %d, want 0", n)
	}

	spent, err := s.SpentUSD(testNow.Add(-5*time.Minute), testNow)
	if err != nil || math.Abs(spent-7.0) > 1e-9 {
		t.Fatalf("SpentUSD(5m) = %v, %v; want 7.0", spent, err)
	}

	burning, err := s.Burning(testNow.Add(-5*time.Minute), testNow)
	if err != nil || len(burning) != 2 {
		t.Fatalf("Burning = %+v, %v; want 2 rows", burning, err)
	}
	if burning[0].SessionID != "b" || math.Abs(burning[0].CostUSD-4.0) > 1e-9 {
		t.Fatalf("hottest session wrong: %+v", burning[0])
	}

	count, err := s.ActiveSessionCount(testNow.Add(-5*time.Minute), testNow)
	if err != nil || count != 2 {
		t.Fatalf("ActiveSessionCount = %d, %v; want 2", count, err)
	}
}

// TestCollectorEndToEnd drives scan → poll → append → poll → truncate →
// poll over real files, asserting incremental ingestion, attribution,
// and idempotent replay after truncation (the rewind case).
func TestCollectorEndToEnd(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "projects", "-work-repo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(proj, "11111111-2222-3333-4444-555555555555.jsonl")

	write := func(lines ...string) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for _, l := range lines {
			fmt.Fprintln(f, l)
		}
	}

	u := Usage{Input: 10, Output: 20}
	write(
		`{"type":"user","message":{"role":"user","content":"go"}}`,
		assistantLine("", "req_a", "claude-sonnet-5", testNow.Add(-time.Minute), u),
	)

	s, err := OpenStore(filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	c := NewCollector(&CollectorArgs{
		Store:        s,
		ProjectsRoot: filepath.Join(dir, "projects"),
		Attribute: func(sess string) string {
			if sess == "11111111-2222-3333-4444-555555555555" {
				return "po"
			}
			return ""
		},
		// Now stays the real clock: ScanOnce compares against real file
		// mtimes. Event timestamps come from the lines themselves.
	})

	files, err := c.ScanOnce()
	if err != nil || len(files) != 1 {
		t.Fatalf("ScanOnce = %v, %v; want the one active file", files, err)
	}
	if n, err := c.PollOnce(); err != nil || n != 1 {
		t.Fatalf("first poll inserted %d, %v; want 1", n, err)
	}

	// The filename supplied the session id; attribution mapped it to "po".
	rows, err := s.Burning(testNow.Add(-time.Hour), testNow.Add(time.Hour))
	if err != nil || len(rows) != 1 || rows[0].Worker != "po" {
		t.Fatalf("attribution missing: %+v, %v", rows, err)
	}

	// Appended lines are picked up incrementally, already-read ones not re-counted.
	write(assistantLine("", "req_b", "claude-sonnet-5", testNow, u))
	if n, err := c.PollOnce(); err != nil || n != 1 {
		t.Fatalf("incremental poll inserted %d, %v; want 1", n, err)
	}
	if n, err := c.PollOnce(); err != nil || n != 0 {
		t.Fatalf("idle poll inserted %d, %v; want 0", n, err)
	}

	// Truncation (session rewind) forces a replay; dedup keeps totals honest.
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, data[:len(data)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	write(assistantLine("", "req_a", "claude-sonnet-5", testNow.Add(-time.Minute), u)) // replayed line
	if n, err := c.PollOnce(); err != nil || n != 0 {
		t.Fatalf("post-truncation poll inserted %d, %v; want 0 (all dups)", n, err)
	}
	spent, _ := s.SpentUSD(testNow.Add(-time.Hour), testNow.Add(time.Hour))
	want := 2 * EstimateCostUSD("claude-sonnet-5", u)
	if math.Abs(spent-want) > 1e-12 {
		t.Fatalf("total spend %v, want %v (no double-counting)", spent, want)
	}
}
