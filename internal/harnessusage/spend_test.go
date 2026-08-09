// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package harnessusage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 🎯T392 froze a baseline window from Grok's own turn_completed billing
// frames. These are the numbers the whole sub-graph is measured against;
// if CollectSpend cannot reproduce them, nothing it says about a
// post-change window is worth reading.
//
// The window is closed at both ends: a baseline whose value depends on
// when you compute it is not a baseline.
//
// These numbers were briefly "corrected" down to 1042 turns / 893.1M when
// this test first ran red. That was the wrong diagnosis — the collector
// was losing whole session files to a bufio.Scanner that failed the walk
// on one oversized line (see forEachJSONLLine). Fixing the reader restored
// the original figures exactly. A measurement that disagrees with its
// reference is a bug in one of them; moving the reference to match is how
// you lose the ability to tell which.
var (
	baselineSince = time.Date(2026, 8, 8, 1, 53, 0, 0, time.UTC)
	baselineUntil = time.Date(2026, 8, 9, 11, 53, 0, 0, time.UTC)
)

const (
	baselineTurns       = 1070
	baselineCalls       = 4848
	baselineInput       = 907_100_000 // compared at 0.1M resolution
	baselineMeanContext = 187_000     // compared at 1k resolution
	baselineCancelled   = 120
)

func writeJSONL(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func grokTurn(session string, ts int64, in, out, cached, calls int, stop string) string {
	return fmt.Sprintf(`{"timestamp":%d,"method":"_x.ai/session/update","params":{"sessionId":%q,"update":{"sessionUpdate":"turn_completed","prompt_id":"p%d","stop_reason":%q,"usage":{"inputTokens":%d,"outputTokens":%d,"cachedReadTokens":%d,"modelCalls":%d,"costUsdTicks":1,"modelUsage":{"grok-4.5-build":{"totalTokens":%d}}}}}}`,
		ts, session, ts, stop, in, out, cached, calls, in+out)
}

// The decomposition is the product: turns x calls x context. A report that
// sums tokens without splitting them cannot tell you which lever to pull,
// so each axis is asserted separately.
func TestCollectSpendDecomposesTheCostIdentity(t *testing.T) {
	dir := t.TempDir()
	grok := filepath.Join(dir, "grok", "sessions", "bucket")
	writeJSONL(t, filepath.Join(grok, "s1", "updates.jsonl"),
		grokTurn("s1", 1786153990, 400_000, 1_000, 380_000, 4, "end_turn"),
		grokTurn("s1", 1786154090, 100_000, 500, 90_000, 2, "cancelled"),
		// Outside the window on both sides — must not be counted.
		grokTurn("s1", 1786153900, 999_999, 1, 0, 1, "end_turn"),
		grokTurn("s1", 1786280000, 999_999, 1, 0, 1, "end_turn"),
	)
	writeJSONL(t, filepath.Join(grok, "s2", "updates.jsonl"),
		grokTurn("s2", 1786154190, 50_000, 200, 40_000, 1, "end_turn"),
	)

	rep, err := CollectSpend(SpendArgs{
		Collect:        CollectArgs{Roots: map[Harness]string{HarnessGrok: filepath.Join(dir, "grok")}},
		Since:          baselineSince,
		Until:          baselineUntil,
		Harnesses:      []Harness{HarnessGrok},
		AgentBySession: map[string]string{"s1": "jevons-po"},
		Coordinators:   map[string]bool{"jevons-po": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Turns != 3 {
		t.Errorf("turns=%d want 3", rep.Turns)
	}
	if rep.ModelCalls != 7 {
		t.Errorf("calls=%d want 7 (4+2+1)", rep.ModelCalls)
	}
	if rep.Input != 550_000 {
		t.Errorf("input=%d want 550000", rep.Input)
	}
	// 550000/7 — the context each call actually carried, which is what a
	// ceiling binds. Not the per-turn total.
	if got := int(rep.MeanContext); got != 78571 {
		t.Errorf("mean context=%d want 78571", got)
	}
	if rep.CancelledTurns != 1 || rep.CancelledInput != 100_000 {
		t.Errorf("cancelled=%d/%d want 1/100000", rep.CancelledTurns, rep.CancelledInput)
	}
	// s1 is a named coordinator; s2 was never named, so its spend is
	// unattributed and stays in the denominator rather than being folded
	// into the implementer bucket to make the split look tidy.
	if rep.CoordinatorInput != 500_000 {
		t.Errorf("coordinator=%d want 500000", rep.CoordinatorInput)
	}
	if rep.UnattributedInput != 50_000 {
		t.Errorf("unattributed=%d want 50000", rep.UnattributedInput)
	}
	if rep.ImplementerInput != 0 {
		t.Errorf("implementer=%d want 0", rep.ImplementerInput)
	}
	if got := rep.CoordinatorShare(); got < 0.90 || got > 0.91 {
		t.Errorf("coordinator share=%.3f want ~0.909 (unattributed stays in the denominator)", got)
	}
}

// A provider that omits the call count must not make context read as zero
// — a silent zero would make any ceiling look satisfied.
func TestCollectSpendTreatsMissingCallCountAsOne(t *testing.T) {
	dir := t.TempDir()
	writeJSONL(t, filepath.Join(dir, "grok", "sessions", "b", "s", "updates.jsonl"),
		`{"timestamp":1786154000,"method":"_x.ai/session/update","params":{"sessionId":"s","update":{"sessionUpdate":"turn_completed","prompt_id":"p1","stop_reason":"end_turn","usage":{"inputTokens":120000,"outputTokens":10,"cachedReadTokens":0,"costUsdTicks":1,"modelUsage":{"grok-4.5-build":{"totalTokens":120010}}}}}}`)
	rep, err := CollectSpend(SpendArgs{
		Collect:   CollectArgs{Roots: map[Harness]string{HarnessGrok: filepath.Join(dir, "grok")}},
		Since:     baselineSince,
		Until:     baselineUntil,
		Harnesses: []Harness{HarnessGrok},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.ModelCalls != 1 || rep.MeanContext != 120_000 {
		t.Fatalf("calls=%d mean=%.0f want 1/120000", rep.ModelCalls, rep.MeanContext)
	}
}

// The frozen-baseline reproduction. This is the gate 🎯T392.6 exists for:
// until CollectSpend reproduces the numbers T392 was written against, no
// post-change measurement it produces is trustworthy.
//
// It reads the owner's real Grok session logs, so it skips where those are
// absent (CI, a fresh clone) rather than failing — but it must never pass
// vacuously, so a present-but-empty root is a failure, not a skip.
func TestCollectSpendReproducesFrozenBaseline(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	root := filepath.Join(home, ".grok")
	if _, err := os.Stat(resolveGrokSessions(root)); err != nil {
		t.Skipf("no Grok session logs on this machine: %v", err)
	}

	rep, err := CollectSpend(SpendArgs{
		Collect:        CollectArgs{Roots: map[Harness]string{HarnessGrok: root}},
		Since:          baselineSince,
		Until:          baselineUntil,
		Harnesses:      []Harness{HarnessGrok},
		AgentBySession: loadBaselineAgents(t),
		Coordinators:   BaselineCoordinators,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Turns == 0 {
		t.Fatal("no turns in the baseline window — the walk found nothing, which is a broken oracle, not a clean run")
	}
	if rep.Turns != baselineTurns {
		t.Errorf("turns=%d want %d", rep.Turns, baselineTurns)
	}
	if rep.ModelCalls != baselineCalls {
		t.Errorf("model calls=%d want %d", rep.ModelCalls, baselineCalls)
	}
	if got, want := rep.Input/100_000, int64(baselineInput)/100_000; got != want {
		t.Errorf("input=%d (%.1fM) want %.1fM", rep.Input, float64(rep.Input)/1e6, float64(baselineInput)/1e6)
	}
	if got := int(rep.MeanContext) / 1000; got != baselineMeanContext/1000 {
		t.Errorf("mean context=%.0f want ~%d", rep.MeanContext, baselineMeanContext)
	}
	if rep.CancelledTurns != baselineCancelled {
		t.Errorf("cancelled turns=%d want %d", rep.CancelledTurns, baselineCancelled)
	}
}

// loadBaselineAgents reads the session-to-agent map the daemon recorded.
// Attribution is best-effort by construction — daemon logs rotate — so the
// baseline assertions above deliberately cover only the frame-level
// aggregates, which are reproducible from immutable session logs.
func loadBaselineAgents(t *testing.T) map[string]string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(home, ".jevons", "agents.json"))
	if err != nil {
		return nil
	}
	var defs []struct {
		Name      string `json:"name"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(data, &defs); err != nil {
		return nil
	}
	m := map[string]string{}
	for _, d := range defs {
		m[d.SessionID] = d.Name
	}
	return m
}
