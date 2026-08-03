// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeFiler records File calls and returns sequential ids.
type fakeFiler struct {
	calls []FileArgs
	ids   []string
	err   error
}

func (f *fakeFiler) File(args FileArgs) (string, error) {
	f.calls = append(f.calls, args)
	if f.err != nil {
		return "", f.err
	}
	id := fmt.Sprintf("T9%d", 20+len(f.calls))
	if len(f.ids) >= len(f.calls) {
		id = f.ids[len(f.calls)-1]
	}
	return id, nil
}

func TestExtractCandidatesRequiresFrequency(t *testing.T) {
	ev := []Evidence{
		{Kind: "lifecycle_error", Component: "event_push", Decision: "push", Outcome: "error", Message: "undeliverable", SourceID: "a1"},
	}
	if got := ExtractCandidates(ev, 2); len(got) != 0 {
		t.Fatalf("single evidence must not mint, got %+v", got)
	}
	ev = append(ev, Evidence{
		Kind: "lifecycle_error", Component: "event_push", Decision: "push", Outcome: "error", Message: "undeliverable", SourceID: "a2",
	})
	got := ExtractCandidates(ev, 2)
	if len(got) != 1 {
		t.Fatalf("want 1 candidate, got %d %+v", len(got), got)
	}
	if got[0].Count != 2 {
		t.Errorf("count=%d", got[0].Count)
	}
	if got[0].Name == "" || len(got[0].Acceptance) == 0 {
		t.Fatalf("candidate missing name/acceptance: %+v", got[0])
	}
	if got[0].Fingerprint == "" {
		t.Error("missing fingerprint")
	}
}

func TestExtractCandidatesIgnoresReapedIdleAlone(t *testing.T) {
	ev := StreamReaped([]string{"t1", "t2", "t3"}, time.Now())
	if got := ExtractCandidates(ev, 2); len(got) != 0 {
		t.Fatalf("reaped_idle alone must not mint: %+v", got)
	}
}

func TestEventsToEvidenceClassifies(t *testing.T) {
	rows := []EventRow{
		{Level: "error", Component: "event_push", Decision: "push", Outcome: "error", Msg: "no target", Corr: "c1"},
		{Level: "error", Component: "event_push", Decision: "push", Outcome: "error", Msg: "no target", Corr: "c2"},
		{Level: "info", Component: "thread", Decision: "spawn", Outcome: "ok", Msg: "spawned"},
		{Level: "warn", Component: "cost", Decision: "monitor", Msg: "cost spike anomaly", Corr: "c3"},
		{Level: "warn", Component: "cost", Decision: "monitor", Msg: "cost spike anomaly", Corr: "c4"},
	}
	ev := EventsToEvidence(rows)
	// info ok dropped; 2 event_push_error + 2 cost_anomaly
	if len(ev) != 4 {
		t.Fatalf("want 4 evidence rows, got %d %+v", len(ev), ev)
	}
	cands := ExtractCandidates(ev, 2)
	if len(cands) < 2 {
		t.Fatalf("want ≥2 candidates (push + cost), got %d %+v", len(cands), cands)
	}
}

func TestDedupePriorAndExisting(t *testing.T) {
	cands := []Candidate{
		{Name: "event_push push failures are diagnosed or eliminated", Acceptance: []string{"a"}, Fingerprint: "fp1"},
		{Name: "Other unique improvement", Acceptance: []string{"b"}, Fingerprint: "fp2"},
	}
	keep, skipped := Dedupe(cands, []ExistingTarget{
		{ID: "T1", Name: "event_push push failures are diagnosed or eliminated"},
	}, nil)
	if len(keep) != 1 || keep[0].Fingerprint != "fp2" {
		t.Fatalf("keep=%+v skipped=%+v", keep, skipped)
	}
	keep2, skipped2 := Dedupe(cands, nil, []string{"fp2"})
	if len(keep2) != 1 || keep2[0].Fingerprint != "fp1" {
		t.Fatalf("prior keep=%+v skipped=%+v", keep2, skipped2)
	}
}

func TestRunCycleMintsFromSampleEvidence(t *testing.T) {
	// Hermetic oracle for 🎯T92: sample evidence → filed bullseye target.
	ev := []Evidence{
		{Kind: "tool_failure", Component: "mcp", Decision: "call", Outcome: "error", Message: "timeout", SourceID: "s1"},
		{Kind: "tool_failure", Component: "mcp", Decision: "call", Outcome: "error", Message: "timeout", SourceID: "s2"},
		{Kind: "tool_failure", Component: "mcp", Decision: "call", Outcome: "error", Message: "timeout", SourceID: "s3"},
	}
	f := &fakeFiler{}
	res, err := RunCycle(CycleArgs{
		Cwd:      t.TempDir(),
		Evidence: ev,
		Filer:    f,
		MinCount: 2,
		MaxMint:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Proposed) != 1 {
		t.Fatalf("proposed=%+v", res.Proposed)
	}
	if len(res.Filed) != 1 {
		t.Fatalf("filed=%+v skipped=%+v", res.Filed, res.Skipped)
	}
	if res.Filed[0].ID == "" || res.Filed[0].Fingerprint == "" {
		t.Fatalf("filed incomplete: %+v", res.Filed[0])
	}
	if len(f.calls) != 1 {
		t.Fatalf("filer calls=%d", len(f.calls))
	}
	if f.calls[0].Name == "" || len(f.calls[0].Acceptance) == 0 {
		t.Fatalf("filed without acceptance: %+v", f.calls[0])
	}
	if !strings.Contains(f.calls[0].Context, "Ambient RSI") {
		t.Errorf("context should cite ambient RSI: %q", f.calls[0].Context)
	}
}

func TestRunCycleDryRunNoFile(t *testing.T) {
	ev := []Evidence{
		{Kind: "lifecycle_error", Component: "x", Decision: "y", Outcome: "error", Message: "m"},
		{Kind: "lifecycle_error", Component: "x", Decision: "y", Outcome: "error", Message: "m"},
	}
	f := &fakeFiler{}
	res, err := RunCycle(CycleArgs{
		Cwd:      t.TempDir(),
		Evidence: ev,
		Filer:    f,
		DryRun:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Filed) != 0 || len(f.calls) != 0 {
		t.Fatalf("dry-run must not file: filed=%v calls=%d", res.Filed, len(f.calls))
	}
	if len(res.Proposed) != 1 {
		t.Fatalf("want proposed, got %+v", res.Proposed)
	}
}

func TestRunCycleMaxMint(t *testing.T) {
	// Two distinct clusters, MaxMint=1 → only one filed.
	ev := []Evidence{
		{Kind: "lifecycle_error", Component: "a", Decision: "d1", Outcome: "error", Message: "m1"},
		{Kind: "lifecycle_error", Component: "a", Decision: "d1", Outcome: "error", Message: "m1"},
		{Kind: "lifecycle_error", Component: "b", Decision: "d2", Outcome: "error", Message: "m2"},
		{Kind: "lifecycle_error", Component: "b", Decision: "d2", Outcome: "error", Message: "m2"},
	}
	f := &fakeFiler{}
	res, err := RunCycle(CycleArgs{
		Cwd: t.TempDir(), Evidence: ev, Filer: f, MaxMint: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Filed) != 1 {
		t.Fatalf("want 1 filed under max mint, got %+v", res.Filed)
	}
	var maxSkip bool
	for _, s := range res.Skipped {
		if s.Reason == "max_mint_per_cycle" {
			maxSkip = true
		}
	}
	if !maxSkip {
		t.Errorf("expected max_mint skip, skipped=%+v", res.Skipped)
	}
}

func TestLedgerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	led, err := OpenLedger(dir, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if err := led.Record([]Filed{{Fingerprint: "abc", ID: "T1", Name: "n"}}, now); err != nil {
		t.Fatal(err)
	}
	fps, err := led.ActiveFingerprints(now.Add(30 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(fps) != 1 || fps[0] != "abc" {
		t.Fatalf("active=%v", fps)
	}
	fps2, err := led.ActiveFingerprints(now.Add(2 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(fps2) != 0 {
		t.Fatalf("expired should be empty, got %v", fps2)
	}
}

func TestLoopRunOnceFromEventLog(t *testing.T) {
	state := t.TempDir()
	logPath := filepath.Join(state, "logs", "events.jsonl")
	// Write a tiny journal with repeated errors.
	j, err := openTestJournal(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := j.Append(map[string]any{
			"ts":        time.Now().UTC().Format(time.RFC3339Nano),
			"source":    "server",
			"level":     "error",
			"msg":       "push failed",
			"component": "event_push",
			"decision":  "push",
			"fields":    map[string]any{"outcome": "error"},
			"corr":      fmt.Sprintf("c%d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	_ = j.Close()

	// Create bullseye.yaml so mint is not forced dry.
	mintCwd := t.TempDir()
	if err := writeFile(filepath.Join(mintCwd, "bullseye.yaml"), "schema_version: 1\ntargets: {}\n"); err != nil {
		t.Fatal(err)
	}

	f := &fakeFiler{}
	var saw CycleResult
	loop, err := NewLoop(LoopArgs{
		StateDir:     state,
		MintCwd:      mintCwd,
		EventLogPath: logPath,
		Interval:     -1, // no ticker
		Filer:        f,
		OnResult:     func(r CycleResult) { saw = r },
	})
	if err != nil {
		t.Fatal(err)
	}
	loop.NoteReaped([]string{"worker-1"})
	res, err := loop.RunOnce("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Filed) != 1 {
		t.Fatalf("want filed from eventlog evidence, got filed=%+v proposed=%+v skipped=%+v",
			res.Filed, res.Proposed, res.Skipped)
	}
	if saw.Filed[0].ID != res.Filed[0].ID {
		t.Error("OnResult not wired")
	}
	// Second cycle should suppress via ledger.
	res2, err := loop.RunOnce("test2")
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Filed) != 0 {
		t.Fatalf("ledger should suppress re-mint, filed=%+v", res2.Filed)
	}
}

func openTestJournal(path string) (*fileJournal, error) {
	return newFileJournal(path)
}
