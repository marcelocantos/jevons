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
	} {
		if e := ParseLine([]byte(line), "fb", testNow); e != nil {
			t.Fatalf("%s parsed as billable: %+v", name, e)
		}
	}
}

func TestPricingFamilies(t *testing.T) {
	u := Usage{Input: 1e6} // 1 MTok input isolates the input rate
	for model, want := range map[string]float64{
		"claude-opus-4-8":            15,
		"claude-sonnet-5":            3,
		"claude-haiku-4-5-20251001":  1,
		"claude-fable-5-unknown-fam": 15, // unknown → conservative (opus) rate
	} {
		if got := EstimateCostUSD(model, u); math.Abs(got-want) > 1e-9 {
			t.Fatalf("EstimateCostUSD(%s, 1MTok in) = %v, want %v", model, got, want)
		}
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
