// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package harnessusage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
//
// 🎯T684 (2026-10-14): the oracle no longer reads the live ~/.grok tree.
// That tree is a moving target — harness layout changes and session
// rotation silently emptied the historical window, and the failure mode
// looked like a CollectSpend regression (turns=2 want 1070). The durable
// corpus under testdata/baseline/grok/ is the standing reference: it is
// committed, layout-stable, and produces the original 🎯T392 aggregates.
// A missing or near-empty corpus fails naming the gap; it never skips.
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

// baselineGrokRoot returns the committed durable corpus (🎯T684).
// Resolved from this source file so the test does not depend on cwd.
func baselineGrokRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate baseline corpus")
	}
	root := filepath.Join(filepath.Dir(file), "testdata", "baseline", "grok")
	sessions := resolveGrokSessions(root)
	if !isDir(sessions) {
		t.Fatalf("baseline corpus missing: resolved root=%s sessions=%s — the durable 🎯T684 fixture under internal/harnessusage/testdata/baseline/grok must be present; a CollectSpend regression is not diagnosable without it",
			root, sessions)
	}
	return root
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
// 🎯T684: reads the committed corpus under testdata/baseline/grok, never
// the live ~/.grok tree. Missing or near-empty corpus fails naming the
// gap (root, window, turn count); it does not skip and it does not look
// like a collector numeric mismatch.
func TestCollectSpendReproducesFrozenBaseline(t *testing.T) {
	root := baselineGrokRoot(t)

	rep, err := CollectSpend(SpendArgs{
		Collect:      CollectArgs{Roots: map[Harness]string{HarnessGrok: root}},
		Since:        baselineSince,
		Until:        baselineUntil,
		Harnesses:    []Harness{HarnessGrok},
		Coordinators: BaselineCoordinators,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertBaselineCorpusSane(t, root, rep)

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

// 🎯T684: a near-empty walk is reported as a broken oracle (names root,
// window, counts), never as "turns=2 want 1070".
func TestT684NearEmptyCorpusIsNamedBrokenOracle(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "grok")
	writeJSONL(t, filepath.Join(root, "sessions", "thin", "updates.jsonl"),
		grokTurn("thin", 1786154000, 1000, 10, 0, 1, "end_turn"),
		grokTurn("thin", 1786154100, 1000, 10, 0, 1, "end_turn"),
	)
	rep, err := CollectSpend(SpendArgs{
		Collect:   CollectArgs{Roots: map[Harness]string{HarnessGrok: root}},
		Since:     baselineSince,
		Until:     baselineUntil,
		Harnesses: []Harness{HarnessGrok},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Turns != 2 {
		t.Fatalf("fixture setup: turns=%d want 2", rep.Turns)
	}
	// Drive the guard through the pure message helper.
	got := baselineCorpusSaneMessage(root, rep)
	if got == "" {
		t.Fatal("near-empty corpus produced no broken-oracle message")
	}
	resolved := resolveGrokSessions(root)
	for _, want := range []string{
		resolved,
		"near-empty",
		fmt.Sprintf("files=%d", rep.FilesWalked),
		fmt.Sprintf("turns=%d", rep.Turns),
		fmt.Sprintf("%d", baselineTurns),
		"🎯T684",
		"broken oracle",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("broken-oracle message %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "want 1070") && !strings.Contains(got, "broken oracle") {
		t.Errorf("message still reads as a bare numeric mismatch: %q", got)
	}
}

// 🎯T684: an empty walk is the same class of failure as near-empty.
func TestT684EmptyCorpusIsNamedBrokenOracle(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "grok")
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	rep, err := CollectSpend(SpendArgs{
		Collect:   CollectArgs{Roots: map[Harness]string{HarnessGrok: root}},
		Since:     baselineSince,
		Until:     baselineUntil,
		Harnesses: []Harness{HarnessGrok},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Turns != 0 {
		t.Fatalf("fixture setup: turns=%d want 0", rep.Turns)
	}
	got := baselineCorpusSaneMessage(root, rep)
	if got == "" {
		t.Fatal("empty corpus produced no broken-oracle message")
	}
	resolved := resolveGrokSessions(root)
	for _, want := range []string{
		resolved,
		"empty",
		"files=0",
		"turns=0",
		"🎯T684",
		"broken oracle",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("broken-oracle message %q missing %q", got, want)
		}
	}
}

// baselineCorpusSaneMessage is the pure form of assertBaselineCorpusSane:
// returns the failure text, or "" when the corpus is sane. Tests assert
// on the text; the production path calls t.Fatalf with it.
func baselineCorpusSaneMessage(root string, rep SpendReport) string {
	sessions := resolveGrokSessions(root)
	if rep.Turns == 0 {
		return fmt.Sprintf("baseline corpus empty: resolved root=%s window=%s..%s files=%d turns=0 (want %d) — broken oracle, not a CollectSpend regression (🎯T684)",
			sessions, baselineSince.Format(time.RFC3339), baselineUntil.Format(time.RFC3339), rep.FilesWalked, baselineTurns)
	}
	if rep.Turns < baselineTurns/2 {
		return fmt.Sprintf("baseline corpus near-empty: resolved root=%s window=%s..%s files=%d turns=%d (want %d) — broken oracle, not a CollectSpend regression (🎯T684)",
			sessions, baselineSince.Format(time.RFC3339), baselineUntil.Format(time.RFC3339), rep.FilesWalked, rep.Turns, baselineTurns)
	}
	return ""
}

func assertBaselineCorpusSane(t *testing.T, root string, rep SpendReport) {
	t.Helper()
	if msg := baselineCorpusSaneMessage(root, rep); msg != "" {
		t.Fatal(msg)
	}
}

// loadBaselineAgents reads the session-to-agent map the daemon recorded.
// Attribution is best-effort by construction — daemon logs rotate — so the
// baseline assertions above deliberately cover only the frame-level
// aggregates, which are reproducible from immutable session logs.
func TestCollectSpendWalksDatedSpool(t *testing.T) {
	home := t.TempDir()
	spoolDir := filepath.Join(home, "spool")
	if err := os.MkdirAll(spoolDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JEVONS_SPOOL_DIR", spoolDir)
	if err := os.WriteFile(filepath.Join(spoolDir, "events-2026-09-25.log"), []byte(
		`{"ts":"2026-09-25T00:00:00.000Z","seat":"po","type":"text","text":"from spool","costUSD":0.5}`+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	vendor := filepath.Join(home, ".claude", "projects", "bucket", "00000000-0000-4000-8000-000000000001.jsonl")
	writeJSONL(t, vendor, `{"type":"assistant","sessionId":"00000000-0000-4000-8000-000000000001","timestamp":"2026-09-25T00:00:00Z","message":{"id":"m","model":"claude-opus-4-8","usage":{"input_tokens":99,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`)

	rep, err := CollectSpend(SpendArgs{
		Collect: CollectArgs{
			Home:  home,
			Roots: map[Harness]string{HarnessGrok: filepath.Join(home, "missing-grok"), HarnessClaude: filepath.Join(home, "missing-claude")},
			Now:   time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC),
		},
		Since:     time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC),
		Until:     time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC),
		Harnesses: []Harness{HarnessGrok},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(rep.Notes, "\n"), vendor) {
		t.Fatalf("collect noted a vendor JSONL for the sidecar walk: %v", rep.Notes)
	}
	if rep.Turns < 1 {
		t.Fatalf("CollectSpend missed the dated spool (turns=%d notes=%v)", rep.Turns, rep.Notes)
	}
}

