// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/spool"
)

// These fixtures test the projection's transaction semantics, not provider
// counter normalization. Actual producer/wire acceptance remains separate.
func usagePair(call string, seq uint64) (claudiawire.UsageFact, claudiawire.UsageFact) {
	start := claudiawire.UsageFact{Seat: "worker", SessionID: "generation", CallID: call,
		Seq: seq, Kind: "model", State: "started", Provider: "anthropic", Model: "fixture-model",
		TurnID: "turn", Requests: []claudiawire.UsageRequestRef{{RequestID: "owner-request", AttemptID: "delivery-attempt"}},
		StartedAt: testNow.Format(time.RFC3339Nano), CounterSource: "adapter_normalized_positive", Coverage: "engine_invocation"}
	final := start
	final.Seq, final.State, final.FinishedAt = seq+1, "completed", testNow.Add(time.Second).Format(time.RFC3339Nano)
	input, output, zero, reasoning, contextSize := int64(21), int64(4), int64(0), int64(2), int64(500)
	final.Counters = claudiawire.UsageCounters{InputTokens: &input, OutputTokens: &output, CacheReadInputTokens: &zero, ReasoningTokens: &reasoning}
	final.ContextTokens = &contextSize
	return start, final
}

func TestMonitorStructuredUsageKeepsUnknownAndLegacySeparate(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start, final := usagePair("measured", 1)
	unfinished, _ := usagePair("unfinished", 3)
	if _, err := s.InsertEvents([]*Event{
		{UsageFact: &start}, {UsageFact: &final}, {UsageFact: &unfinished},
		{UsageFailure: &UsageFailure{Seat: "worker", At: testNow, Message: "observation lost"}},
		// This legacy estimate may describe the very same provider call.
		{Timestamp: testNow, SessionID: "generation", RequestID: "legacy", Model: "fixture", Usage: Usage{Input: 21, Output: 4}, CostUSD: 2},
	}); err != nil {
		t.Fatal(err)
	}
	now := testNow.Add(time.Minute)
	m := NewMonitor(&MonitorArgs{Store: s, Config: DefaultBudgetConfig, Now: func() time.Time { return now }})
	snap, err := m.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	u := snap.StructuredUsage
	if u.Calls != 2 || u.IncompleteCalls != 1 || u.ObservationFailures != 1 || u.UnknownInputTotal != 2 || u.UnknownOutput != 1 || u.UnknownCost != 2 || u.KnownConsumptionTokens() != 25 {
		t.Fatalf("monitor erased unknowns or counted overlapping legacy evidence: %+v", u)
	}
	if snap.StructuredUsageToday != u || snap.SpentTodayUSD != 2 || u.APIEquivalentCostUSD != 0 {
		t.Fatalf("daily observation or legacy estimate changed: %+v", snap)
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Usage struct {
			Calls       int64 `json:"calls"`
			UnknownCost int64 `json:"unknown_cost_calls"`
		} `json:"structured_usage"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil || wire.Usage.Calls != 2 || wire.Usage.UnknownCost != 2 {
		t.Fatalf("cost API lost availability: %s, %v", raw, err)
	}
	now = now.Add(DefaultBudgetConfig().Window.Std())
	snap, err = m.Snapshot()
	if err != nil || snap.StructuredUsage.Calls != 0 || snap.StructuredUsageToday != u {
		t.Fatalf("window aging changed daily observations: %+v, %v", snap, err)
	}
}

func TestStructuredUsageReplayAfterLiveFinalAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "usage.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	start, final := usagePair("call-A", 1)
	if err := s.IngestUsageFact(ctx, final.Seat, final.SessionID, &final); err != nil {
		t.Fatal(err)
	}
	if cursor, err := s.UsageCursor(ctx, start.Seat, start.SessionID); err != nil || cursor != 0 {
		t.Fatalf("live event advanced replay: %d %v", cursor, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	page := &claudiawire.UsagePage{Seat: start.Seat, SessionID: start.SessionID, Facts: []claudiawire.UsageFact{start, final}, Next: 2}
	q := claudiawire.UsageQuery{Seat: start.Seat, SessionID: start.SessionID}
	if err := s.IngestUsagePage(ctx, q, page); err != nil {
		t.Fatal(err)
	}
	if err := s.IngestUsageFact(ctx, final.Seat, final.SessionID, &final); err != nil {
		t.Fatal(err)
	}
	got, err := s.StructuredUsage(ctx, testNow, testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got.Calls != 1 || got.Known != (Usage{Input: 21, Output: 4}) || got.UnknownInput != 0 || got.UnknownCacheRead != 0 || got.UnknownCacheCreate != 1 || got.UnknownCost != 1 || got.IncompleteCalls != 0 {
		t.Fatalf("unknown/zero/dedup/independent dimensions lost: %+v", got)
	}
	if cursor, err := s.UsageCursor(ctx, start.Seat, start.SessionID); err != nil || cursor != 2 {
		t.Fatalf("cursor=%d err=%v", cursor, err)
	}
	var raw string
	if err := s.db.QueryRow(`SELECT fact FROM usage_calls`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var stored claudiawire.UsageFact
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored, final) {
		t.Fatal("immutable attribution or context was lost")
	}
}

func TestStructuredUsageCursorAndFactsRollbackTogether(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start, final := usagePair("call", 1)
	page := &claudiawire.UsagePage{Seat: start.Seat, SessionID: start.SessionID, Facts: []claudiawire.UsageFact{start, final}, Next: 2}
	q := claudiawire.UsageQuery{Seat: start.Seat, SessionID: start.SessionID}
	// Fail on the second fact, after the first INSERT has succeeded.
	if _, err := s.db.Exec(`CREATE TRIGGER fail_usage BEFORE UPDATE ON usage_calls BEGIN SELECT RAISE(ABORT, 'fixture storage failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.IngestUsagePage(ctx, q, page); err == nil {
		t.Fatal("database failure accepted")
	}
	got, err := s.StructuredUsage(ctx, testNow, testNow.Add(time.Hour))
	if err != nil || got.Calls != 0 {
		t.Fatalf("partial fact survived rollback: %+v %v", got, err)
	}
	if cursor, err := s.UsageCursor(ctx, q.Seat, q.SessionID); err != nil || cursor != 0 {
		t.Fatalf("failed cursor=%d err=%v", cursor, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_usage`); err != nil {
		t.Fatal(err)
	}
	if err := s.IngestUsagePage(ctx, q, page); err != nil {
		t.Fatal(err)
	}
	if err := s.IngestUsagePage(ctx, q, page); err == nil {
		t.Fatal("stale caller cursor accepted")
	}
}

func TestStructuredUsageConflictsAndRequestMultiplicity(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, af := usagePair("A", 1)
	b, bf := usagePair("B", 3) // Same owner request, distinct provider invocation.
	bf.Kind, b.Kind = "compaction", "compaction"
	page := &claudiawire.UsagePage{Seat: a.Seat, SessionID: a.SessionID, Facts: []claudiawire.UsageFact{a, af, b, bf}, Next: 4}
	q := claudiawire.UsageQuery{Seat: a.Seat, SessionID: a.SessionID}
	if err := s.IngestUsagePage(ctx, q, page); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*claudiawire.UsageFact){
		"counter":         func(f *claudiawire.UsageFact) { n := int64(999); f.Counters.InputTokens = &n },
		"model":           func(f *claudiawire.UsageFact) { f.Model = "replacement" },
		"cause":           func(f *claudiawire.UsageFact) { f.Cause = "changed" },
		"second terminal": func(f *claudiawire.UsageFact) { f.Seq++ },
	} {
		t.Run(name, func(t *testing.T) {
			bad := af
			change(&bad)
			if err := s.IngestUsageFact(ctx, bad.Seat, bad.SessionID, &bad); err == nil {
				t.Fatal("conflicting fact accepted")
			}
		})
	}
	// A reused seat name is a new generation, not the previous call.
	fresh := af
	fresh.SessionID = "next-generation"
	if err := s.IngestUsageFact(ctx, fresh.Seat, fresh.SessionID, &fresh); err != nil {
		t.Fatal(err)
	}
	if err := s.IngestUsageFact(ctx, "foreign", af.SessionID, &af); err == nil {
		t.Fatal("foreign identity accepted")
	}
	got, err := s.StructuredUsage(ctx, testNow, testNow.Add(time.Hour))
	if err != nil || got.Calls != 3 || got.Known.Input != 63 || got.Known.Output != 12 {
		t.Fatalf("multiplicity lost: %+v %v", got, err)
	}
	// A corrupt/gapped page cannot advance either facts or cursor.
	bad := &claudiawire.UsagePage{Seat: a.Seat, SessionID: a.SessionID, Facts: []claudiawire.UsageFact{af}, Next: 5}
	q.After = 4
	if err := s.IngestUsagePage(ctx, q, bad); err == nil {
		t.Fatal("gapped page accepted")
	}
}

func TestStructuredUsageIncompleteIsNotMeasuredZero(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start, _ := usagePair("unfinished", 1)
	if err := s.IngestUsageFact(ctx, start.Seat, start.SessionID, &start); err != nil {
		t.Fatal(err)
	}
	got, err := s.StructuredUsage(ctx, testNow, testNow.Add(time.Nanosecond))
	if err != nil || got.Calls != 1 || got.IncompleteCalls != 1 || got.UnknownInput != 1 || got.UnknownOutput != 1 || got.UnknownCacheRead != 1 || got.UnknownCost != 1 {
		t.Fatalf("missing became zero: %+v %v", got, err)
	}
}

func TestCollectorStructuredSpoolReplayAndObservationFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JEVONS_SPOOL_DIR", root)
	path := filepath.Join(root, "events-2026-07-07.log")
	start, final := usagePair("spooled", 1)
	var data []byte
	for _, rec := range []spool.Record{
		{RawTS: testNow.Format(time.RFC3339Nano), Seat: "broken-worker", Type: "usage_error", Text: "fixture journal failure"},
		usageSpoolRecord(t, start), usageSpoolRecord(t, final), usageSpoolRecord(t, final),
	} {
		line, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, append(line, '\n')...)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := NewCollector(&CollectorArgs{Store: s, ProjectsRoot: root, Now: func() time.Time { return testNow }})
	if _, err := c.ScanOnce(); err != nil {
		t.Fatal(err)
	}
	if n, err := c.PollOnce(); err != nil || n != 2 {
		t.Fatalf("first poll: %d %v", n, err)
	}
	if off, err := s.TailOffset(path); err != nil || off != int64(len(data)) {
		t.Fatalf("offset: %d %v", off, err)
	}
	// Re-reading the exact spool after an offset loss/restart is idempotent.
	if err := s.SetTailOffset(path, 0); err != nil {
		t.Fatal(err)
	}
	if n, err := c.PollOnce(); err != nil || n != 0 {
		t.Fatalf("replay: %d %v", n, err)
	}
	got, err := s.StructuredUsage(context.Background(), testNow, testNow.Add(time.Hour))
	if err != nil || got.Calls != 1 || got.Known != (Usage{Input: 21, Output: 4}) || got.ObservationFailures != 1 {
		t.Fatalf("spool projection: %+v %v", got, err)
	}
	var legacy int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_events`).Scan(&legacy); err != nil || legacy != 0 {
		t.Fatalf("structured usage was flattened into legacy events: %d %v", legacy, err)
	}
	if cursor, err := s.UsageCursor(context.Background(), start.Seat, start.SessionID); err != nil || cursor != 0 {
		t.Fatalf("spool advanced journal replay cursor: %d %v", cursor, err)
	}
}

func TestCollectorStructuredStorageFailureLeavesOffsetForRetry(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JEVONS_SPOOL_DIR", root)
	path := filepath.Join(root, "events-2026-07-07.log")
	_, final := usagePair("spooled", 1)
	data, err := json.Marshal(usageSpoolRecord(t, final))
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := NewCollector(&CollectorArgs{Store: s, ProjectsRoot: root, Now: func() time.Time { return testNow }})
	if _, err := c.ScanOnce(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_usage BEFORE INSERT ON usage_calls BEGIN SELECT RAISE(ABORT, 'fixture storage failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PollOnce(); err == nil {
		t.Fatal("failed write reported success")
	}
	if off, err := s.TailOffset(path); err != nil || off != 0 {
		t.Fatalf("failed offset: %d %v", off, err)
	}
	if c.Health().LastPollErr == "" {
		t.Fatal("storage failure disappeared from health")
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_usage`); err != nil {
		t.Fatal(err)
	}
	if n, err := c.PollOnce(); err != nil || n != 1 {
		t.Fatalf("retry: %d %v", n, err)
	}
	if off, err := s.TailOffset(path); err != nil || off != int64(len(data)) {
		t.Fatalf("retry offset: %d %v", off, err)
	}
	got, err := s.StructuredUsage(context.Background(), testNow, testNow.Add(time.Hour))
	if err != nil || got.Calls != 1 {
		t.Fatalf("retry projection: %+v %v", got, err)
	}
}

func TestCollectorInvalidStructuredRecordCannotAdvance(t *testing.T) {
	_, final := usagePair("spooled", 1)
	for name, line := range map[string][]byte{
		"malformed json":       []byte("{\n"),
		"invalid fact":         []byte(`{"type":"usage","seat":"worker","usage_fact":{}}` + "\n"),
		"missing failure time": []byte(`{"type":"usage_error","seat":"worker","text":"lost"}` + "\n"),
		"foreign seat": func() []byte {
			rec := usageSpoolRecord(t, final)
			rec.Seat = "other-worker"
			body, err := json.Marshal(rec)
			if err != nil {
				t.Fatal(err)
			}
			return append(body, '\n')
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("JEVONS_SPOOL_DIR", root)
			path := filepath.Join(root, "events-2026-07-07.log")
			if err := os.WriteFile(path, line, 0o600); err != nil {
				t.Fatal(err)
			}
			s, err := OpenStore(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			c := NewCollector(&CollectorArgs{Store: s, ProjectsRoot: root, Now: func() time.Time { return testNow }})
			if _, err := c.ScanOnce(); err != nil {
				t.Fatal(err)
			}
			if _, err := c.PollOnce(); err == nil {
				t.Fatal("invalid observation accepted")
			}
			if off, err := s.TailOffset(path); err != nil || off != 0 {
				t.Fatalf("invalid offset: %d %v", off, err)
			}
		})
	}
}

func TestUsageFailureReplayPreservesHistoricalUncertainty(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	later := &Event{UsageFailure: &UsageFailure{Seat: "worker", At: testNow.Add(time.Hour), Message: "later"}}
	earlier := &Event{UsageFailure: &UsageFailure{Seat: "worker", At: testNow, Message: "earlier"}}
	if _, err := s.InsertEvents([]*Event{earlier, later, earlier}); err != nil {
		t.Fatal(err)
	}
	got, err := s.StructuredUsage(context.Background(), testNow.Add(time.Hour), testNow.Add(2*time.Hour))
	if err != nil || got.ObservationFailures != 1 {
		t.Fatalf("later error hidden: %+v %v", got, err)
	}
	got, err = s.StructuredUsage(context.Background(), testNow, testNow.Add(time.Hour))
	if err != nil || got.ObservationFailures != 1 {
		t.Fatalf("earlier uncertainty erased: %+v %v", got, err)
	}
	got, err = s.StructuredUsage(context.Background(), testNow, testNow.Add(2*time.Hour))
	if err != nil || got.ObservationFailures != 2 {
		t.Fatalf("failure replay duplicated or erased history: %+v %v", got, err)
	}
}

func usageSpoolRecord(t *testing.T, fact claudiawire.UsageFact) spool.Record {
	t.Helper()
	body, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	return spool.Record{RawTS: fact.StartedAt, Seat: fact.Seat, Type: "usage", UsageFact: body}
}

func TestPhysicalUsageRetainsTerminalIDsAndInclusiveInput(t *testing.T) {
	zero, input, output, cache, total := int64(0), int64(21), int64(4), int64(7), int64(28)
	for _, tc := range []struct {
		name                                                   string
		counters                                               claudiawire.UsageCounters
		wantInput, wantConsumption, unknownTotal, unknownFresh int64
	}{
		{"inclusive only", claudiawire.UsageCounters{TotalInputTokens: &total, OutputTokens: &output}, 28, 32, 0, 1},
		{"inclusive and buckets", claudiawire.UsageCounters{TotalInputTokens: &total, InputTokens: &input, OutputTokens: &output, CacheReadInputTokens: &cache, CacheCreationInputTokens: &zero}, 28, 32, 0, 0},
		{"complete buckets", claudiawire.UsageCounters{InputTokens: &input, OutputTokens: &output, CacheReadInputTokens: &cache, CacheCreationInputTokens: &zero}, 28, 32, 0, 0},
		{"partial buckets", claudiawire.UsageCounters{InputTokens: &input, OutputTokens: &output, CacheReadInputTokens: &cache}, 28, 32, 1, 0},
		{"observed zero total", claudiawire.UsageCounters{TotalInputTokens: &zero, OutputTokens: &zero}, 0, 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := OpenStore(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			start, final := usagePair("physical-call", 1)
			for _, f := range []*claudiawire.UsageFact{&start, &final} {
				f.Coverage, f.CounterSource = "provider_attempt", "provider_reported"
				f.InvocationID, f.ProviderAttemptID = "engine-call", f.CallID
			}
			final.ProviderRequestID, final.ProviderResponseID = "http-request", "response-id"
			final.Counters = tc.counters
			ctx := context.Background()
			page := &claudiawire.UsagePage{Seat: start.Seat, SessionID: start.SessionID, Facts: []claudiawire.UsageFact{start, final}, Next: 2}
			if err := s.IngestUsagePage(ctx, claudiawire.UsageQuery{Seat: start.Seat, SessionID: start.SessionID}, page); err != nil {
				t.Fatal(err)
			}
			// A final delivered again over the spool is the same physical call.
			if n, err := s.InsertEvents([]*Event{{UsageFact: &final}}); err != nil || n != 0 {
				t.Fatalf("repeated final: %d %v", n, err)
			}
			got, err := s.StructuredUsage(ctx, testNow, testNow.Add(time.Hour))
			if err != nil || got.Calls != 1 || got.KnownInputTotal != tc.wantInput || got.KnownConsumptionTokens() != tc.wantConsumption || got.UnknownInputTotal != tc.unknownTotal || got.UnknownInput != tc.unknownFresh {
				t.Fatalf("inclusive input counted incorrectly: %+v %v", got, err)
			}
			var body string
			if err := s.db.QueryRow(`SELECT fact FROM usage_calls`).Scan(&body); err != nil {
				t.Fatal(err)
			}
			var stored claudiawire.UsageFact
			if err := json.Unmarshal([]byte(body), &stored); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(stored, final) {
				t.Fatal("physical identities or raw dimensions lost")
			}
			changed := final
			changed.ProviderResponseID = "replacement"
			if err := s.IngestUsageFact(ctx, final.Seat, final.SessionID, &changed); err == nil {
				t.Fatal("changed final identity accepted")
			}
		})
	}
}

func TestMalformedUsageCannotKeepCollectorFalselyFresh(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JEVONS_SPOOL_DIR", root)
	now := testNow
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := NewCollector(&CollectorArgs{Store: s, ProjectsRoot: root, Now: func() time.Time { return now }})
	if _, err := c.ScanOnce(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PollOnce(); err != nil {
		t.Fatal(err)
	}
	lastSuccess := c.LastPoll()
	bad := filepath.Join(root, "events-2026-07-07.log")
	if err := os.WriteFile(bad, []byte("{\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, final := usagePair("healthy", 1)
	data, err := json.Marshal(usageSpoolRecord(t, final))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "events-2026-07-08.log"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ScanOnce(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(collectorStaleAfter + time.Second)
	if n, err := c.PollOnce(); err == nil || n != 1 {
		t.Fatalf("healthy file not ingested alongside failure: %d %v", n, err)
	}
	if c.LastPoll() != lastSuccess {
		t.Fatal("failed observation refreshed the collector's success clock")
	}
	m := NewMonitor(&MonitorArgs{Store: s, Config: DefaultBudgetConfig, CollectorHealth: c.Health, Now: func() time.Time { return now }})
	// The existing monitor confirms a stale collector on its second read.
	if _, err := m.Snapshot(); err != nil {
		t.Fatal(err)
	}
	snap, err := m.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	visible := false
	for _, alert := range snap.Alerts {
		if alert.Kind == AlertCollectorStale {
			visible = true
		}
	}
	if !visible {
		t.Fatalf("persistent malformed usage remained silently fresh: %+v", snap.Alerts)
	}
	// Repair permits the normal successful-pass clock to advance again.
	if err := os.WriteFile(bad, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PollOnce(); err != nil {
		t.Fatal(err)
	}
	if c.LastPoll() != now || c.Health().LastPollErr != "" {
		t.Fatal("repaired collector did not recover")
	}
}
