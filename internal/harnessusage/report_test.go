// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package harnessusage

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testRoot(t *testing.T) string {
	t.Helper()
	// testdata is next to this package.
	return filepath.Join("testdata")
}

func fixedNow() time.Time {
	return time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
}

func fixtureArgs(t *testing.T) *CollectArgs {
	t.Helper()
	td := testRoot(t)
	return &CollectArgs{
		Now: fixedNow(),
		Roots: map[Harness]string{
			HarnessClaude: filepath.Join(td, "claude"),
			HarnessGrok:   filepath.Join(td, "grok"),
			HarnessCodex:  filepath.Join(td, "codex"),
		},
		CursorDashboardJSON: filepath.Join(td, "cursor", "dashboard-usage.json"),
		CursorTrackingDB:    filepath.Join(td, "cursor", "ai-code-tracking.db"),
	}
}

func TestParseHarness(t *testing.T) {
	cases := []struct {
		in   string
		want Harness
	}{
		{"claude", HarnessClaude},
		{"claude-code", HarnessClaude},
		{"grok", HarnessGrok},
		{"acp", HarnessGrok},
		{"codex", HarnessCodex},
		{"cursor", HarnessCursor},
	}
	for _, tc := range cases {
		got, err := ParseHarness(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("ParseHarness(%q)=%q,%v want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := ParseHarness("nope"); err == nil {
		t.Fatal("expected error for unknown harness")
	}
}

func TestCollectClaudeFixture(t *testing.T) {
	r, err := Collect(HarnessClaude, fixtureArgs(t))
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "local-jsonl" {
		t.Fatalf("source=%q", r.Source)
	}
	if r.Sessions != 1 {
		t.Fatalf("sessions=%d want 1", r.Sessions)
	}
	if r.Events != 2 {
		t.Fatalf("events=%d want 2", r.Events)
	}
	// 1000+2000 input, 200+100 output, 100+0 cache create, 500+1000 cache read
	if r.Tokens.Input != 3000 || r.Tokens.Output != 300 {
		t.Fatalf("tokens in/out=%d/%d", r.Tokens.Input, r.Tokens.Output)
	}
	if r.Tokens.CacheCreate != 100 || r.Tokens.CacheRead != 1500 {
		t.Fatalf("cache create/read=%d/%d", r.Tokens.CacheCreate, r.Tokens.CacheRead)
	}
	// First line costUSD=0.42; second line has no costUSD so EstimateCostUSD adds more.
	if r.CostUSD == nil || *r.CostUSD < 0.42-1e-9 {
		t.Fatalf("cost=%v want >= 0.42", deref(r.CostUSD))
	}
	if len(r.Models) != 2 {
		t.Fatalf("models=%v", r.Models)
	}
}

func TestCollectGrokFixture(t *testing.T) {
	r, err := Collect(HarnessGrok, fixtureArgs(t))
	if err != nil {
		t.Fatal(err)
	}
	if r.Events != 2 || r.Sessions != 1 {
		t.Fatalf("events=%d sessions=%d", r.Events, r.Sessions)
	}
	if r.Tokens.Input != 57966 || r.Tokens.Output != 258 {
		t.Fatalf("tokens=%+v", r.Tokens)
	}
	if r.Tokens.CacheRead != 48800 {
		t.Fatalf("cache_read=%d", r.Tokens.CacheRead)
	}
	// costUsdTicks: 431800000 + 1500000000 = 1931800000 / 1e10 = 0.19318
	// (the divisor is 1e10, not the nano-dollars first assumed — 🎯T394).
	want := 0.19318
	if r.CostUSD == nil || math.Abs(*r.CostUSD-want) > 1e-9 {
		t.Fatalf("cost=%v want %v", r.CostUSD, want)
	}
	if len(r.Models) != 1 || r.Models[0].Model != "grok-4.5-build" {
		t.Fatalf("models=%v", r.Models)
	}
}

func TestCollectCodexFixture(t *testing.T) {
	r, err := Collect(HarnessCodex, fixtureArgs(t))
	if err != nil {
		t.Fatal(err)
	}
	if r.Events != 2 || r.Sessions != 1 {
		t.Fatalf("events=%d sessions=%d", r.Events, r.Sessions)
	}
	// Sum of last_token_usage: (1000+2000) in, (50+100) out, (100+400) cache read, (10+30) reasoning
	if r.Tokens.Input != 3000 || r.Tokens.Output != 150 {
		t.Fatalf("tokens=%+v", r.Tokens)
	}
	if r.Tokens.CacheRead != 500 || r.Tokens.Reasoning != 40 {
		t.Fatalf("cache/reasoning=%d/%d", r.Tokens.CacheRead, r.Tokens.Reasoning)
	}
	if r.RateLimits == nil || r.RateLimits.PlanType != "pro" {
		t.Fatalf("rate_limits=%v", r.RateLimits)
	}
	if r.RateLimits.PrimaryUsedPercent == nil || *r.RateLimits.PrimaryUsedPercent != 12.5 {
		t.Fatalf("used_percent=%v", r.RateLimits.PrimaryUsedPercent)
	}
}

func TestCollectCursorDashboardFixture(t *testing.T) {
	args := fixtureArgs(t)
	r, err := Collect(HarnessCursor, args)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "cursor-dashboard-fixture" {
		t.Fatalf("source=%q", r.Source)
	}
	if r.Events != 123 {
		t.Fatalf("events(used_requests)=%d", r.Events)
	}
	if r.CostUSD == nil || math.Abs(*r.CostUSD-4.56) > 1e-9 {
		t.Fatalf("cost=%v", r.CostUSD)
	}
	if r.Tokens.Input != 1600000 || r.Tokens.Output != 65000 {
		t.Fatalf("tokens=%+v", r.Tokens)
	}
	if len(r.Models) != 2 {
		t.Fatalf("models=%v", r.Models)
	}
	if r.RateLimits == nil || r.RateLimits.PlanType != "pro" {
		t.Fatalf("rate_limits=%v", r.RateLimits)
	}
}

func TestCollectCursorTrackingDBFixture(t *testing.T) {
	td := testRoot(t)
	args := &CollectArgs{
		Now:              fixedNow(),
		CursorTrackingDB: filepath.Join(td, "cursor", "ai-code-tracking.db"),
		// no dashboard → force DB path
	}
	r, err := Collect(HarnessCursor, args)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "cursor-tracking-db" {
		t.Fatalf("source=%q", r.Source)
	}
	if r.Events != 3 {
		t.Fatalf("events=%d want 3", r.Events)
	}
	if r.Sessions != 2 {
		t.Fatalf("sessions=%d want 2", r.Sessions)
	}
	if len(r.Models) != 2 {
		t.Fatalf("models=%v", r.Models)
	}
}

func TestCollectAllFixture(t *testing.T) {
	reps := CollectAll(fixtureArgs(t))
	if len(reps) != len(AllHarnesses) {
		t.Fatalf("len=%d", len(reps))
	}
	for _, r := range reps {
		if r.Source == "error" {
			t.Fatalf("harness %s error: %v", r.Harness, r.Notes)
		}
		if r.GeneratedAt != fixedNow() {
			t.Fatalf("%s GeneratedAt=%v", r.Harness, r.GeneratedAt)
		}
	}
}

func TestCollectMissingRootEmpty(t *testing.T) {
	args := &CollectArgs{
		Now:  fixedNow(),
		Home: t.TempDir(), // no harness data
		Roots: map[Harness]string{
			HarnessClaude: filepath.Join(t.TempDir(), "nope"),
		},
	}
	r, err := Collect(HarnessClaude, args)
	if err != nil {
		t.Fatal(err)
	}
	if r.Events != 0 || r.Sessions != 0 {
		t.Fatalf("expected empty report, got %+v", r)
	}
	if len(r.Notes) == 0 {
		t.Fatal("expected empty-data note")
	}
}

func TestTryLiveAPINotesDoNotFail(t *testing.T) {
	args := fixtureArgs(t)
	args.TryLiveAPI = true
	r, err := Collect(HarnessGrok, args)
	if err != nil {
		t.Fatal(err)
	}
	if r.Events != 2 {
		t.Fatalf("live API must not block scrape; events=%d", r.Events)
	}
	found := false
	for _, n := range r.Notes {
		if len(n) > 0 && (contains(n, "live API") || contains(n, "XAI")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected live API note, notes=%v", r.Notes)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

func deref(p *float64) float64 {
	if p == nil {
		return math.NaN()
	}
	return *p
}
