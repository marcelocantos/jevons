// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/rsi"
)

// t574write writes path and makes the change observable to an mtime poll
// even on a coarse-mtime filesystem.
func t574write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Now(), time.Now().Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
}

// capacity.json: the governor reads the hot policy on every Status, so an
// edit shows in jevons_capacity_status without a bounce; garbage keeps the
// last-good policy (🎯T574).
func TestT574CapacityPolicyIsHot(t *testing.T) {
	dir := t.TempDir()
	path := capacity.ConfigPath(dir)
	w := config.NewWatcher(nil)
	policy, err := config.Watch(w, &config.WatchArgs[*capacity.Policy]{
		Path: path, Load: capacity.LoadPolicy, Fallback: capacity.DefaultPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	gov := capacity.NewGovernor(capacity.GovernorArgs{Policy: policy.Get})
	if got := gov.Status().Policy.MaxConcurrentBackground; got != capacity.DefaultPolicy().MaxConcurrentBackground {
		t.Fatalf("boot policy = %d, want default", got)
	}
	t574write(t, path, `{"max_concurrent_background": 17}`)
	w.Poll()
	if got := gov.Status().Policy.MaxConcurrentBackground; got != 17 {
		t.Fatalf("after edit: max_concurrent_background = %d, want 17", got)
	}
	t574write(t, path, `{"max_concurrent_background": `)
	w.Poll()
	if got := gov.Status().Policy.MaxConcurrentBackground; got != 17 {
		t.Fatalf("garbage must keep last-good 17, got %d", got)
	}
}

// budget.json: the cost guard's config func sees the new limits; the
// overseer stays protected even when the file drops it.
func TestT574BudgetIsHotAndOverseerStaysProtected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "budget.json")
	w := config.NewWatcher(nil)
	budget, err := config.Watch(w, &config.WatchArgs[*cost.BudgetConfig]{
		Path: path, Load: budgetLoader("jevons"), Fallback: cost.DefaultBudgetConfig(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := budget.Get().DailyBudgetUSD; got != cost.DefaultBudgetConfig().DailyBudgetUSD {
		t.Fatalf("boot daily = %v, want default", got)
	}
	t574write(t, path, `{"daily_budget_usd": 123, "protected_workers": ["someone-else"]}`)
	w.Poll()
	got := budget.Get()
	if got.DailyBudgetUSD != 123 {
		t.Fatalf("after edit: daily = %v, want 123", got.DailyBudgetUSD)
	}
	found := false
	for _, p := range got.ProtectedWorkers {
		found = found || p == "jevons"
	}
	if !found {
		t.Fatalf("overseer dropped from protected_workers: %v", got.ProtectedWorkers)
	}
	t574write(t, path, `not json`)
	w.Poll()
	if got := budget.Get().DailyBudgetUSD; got != 123 {
		t.Fatalf("garbage must keep last-good 123, got %v", got)
	}
}

// config.yaml: portfolio membership applies live; structural fields are
// named so the reload can force a bounce rather than ignore them.
func TestT574ConfigYAMLIsHotAndNamesRestartOnlyFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	w := config.NewWatcher(nil)
	var seen []config.Config
	boot, err := config.Watch(w, &config.WatchArgs[config.Config]{
		Path: path, Load: config.Load, Fallback: config.Default(),
		OnChange: func(c config.Config) { seen = append(seen, c) },
	})
	if err != nil {
		t.Fatal(err)
	}
	t574write(t, path, "portfolios:\n  - id: personal\n    name: Personal\n    members: [github.com/marcelocantos]\n")
	w.Poll()
	if n := len(seen); n != 2 || len(seen[1].Portfolios) != 1 || seen[1].Portfolios[0].ID != "personal" {
		t.Fatalf("portfolio edit not applied: %+v", seen)
	}
	if fields := config.RestartOnlyDiff(boot.Get(), seen[0]); len(fields) != 0 {
		t.Fatalf("no structural change yet, got %v", fields)
	}
	t574write(t, path, "port: 1\noverseer_name: other\n")
	w.Poll()
	fields := config.RestartOnlyDiff(seen[0], seen[len(seen)-1])
	if len(fields) != 2 || fields[0] != "overseer_name" || fields[1] != "port" {
		t.Fatalf("restart-only diff = %v, want [overseer_name port]", fields)
	}
	t574write(t, path, "port: [\n")
	w.Poll()
	if got := seen[len(seen)-1].Port; got != 1 {
		t.Fatalf("garbage must keep last-good port 1, got %d", got)
	}
}

// llm-portfolio.json: the loaded flag and the seed both follow the file.
func TestT574LLMPortfolioIsHot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llm-portfolio.json")
	w := config.NewWatcher(nil)
	type pf struct {
		p      *cost.Portfolio
		loaded bool
	}
	h, err := config.Watch(w, &config.WatchArgs[pf]{Path: path, Load: func(path string) (pf, error) {
		p, loaded, err := cost.LoadPortfolioOverride(path)
		return pf{p, loaded}, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	if h.Get().loaded {
		t.Fatal("missing file must report compiled seed")
	}
	t574write(t, path, `{"default_provider": "claude"}`)
	w.Poll()
	if v := h.Get(); !v.loaded || v.p.DefaultProvider != "claude" {
		t.Fatalf("after edit: %+v", v)
	}
	t574write(t, path, `{`)
	w.Poll()
	if v := h.Get(); !v.loaded || v.p.DefaultProvider != "claude" {
		t.Fatalf("garbage must keep last-good: %+v", v)
	}
}

// rsi coach config: the coach re-reads its file every cycle by design, so
// Config() reflects an edit with no watcher at all.
func TestT574CoachConfigIsHot(t *testing.T) {
	dir := t.TempDir()
	coach, err := rsi.NewCoach(rsi.CoachArgs{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	def := rsi.DefaultCoachConfig()
	def.Overseer = "edited"
	if err := rsi.SaveCoachConfig(dir, def); err != nil {
		t.Fatal(err)
	}
	got, err := coach.Config()
	if err != nil || got.Overseer != "edited" {
		t.Fatalf("coach config = %+v err=%v, want overseer edited", got, err)
	}
}

// mcpscope owner map: the element is bounce-required, but only a change to
// the servers themselves counts — Claude Code rewrites ~/.claude.json for
// its own state constantly, and that must never bounce the daemon.
func TestT574MCPOwnerMapFingerprintIgnoresNonServerRewrites(t *testing.T) {
	a := &claudia.MCPInventory{Servers: []claudia.MCPServer{{Name: "b", Type: "http", URL: "http://x"}, {Name: "a", Type: "stdio", Command: "c"}}}
	b := &claudia.MCPInventory{Servers: []claudia.MCPServer{{Name: "a", Type: "stdio", Command: "c"}, {Name: "b", Type: "http", URL: "http://x"}}, Source: "other", Sources: []string{"other"}}
	if mcpMapFingerprint(a) != mcpMapFingerprint(b) {
		t.Fatal("same servers in a different order / from a different source must fingerprint equal")
	}
	c := &claudia.MCPInventory{Servers: []claudia.MCPServer{{Name: "a", Type: "stdio", Command: "c"}, {Name: "b", Type: "http", URL: "http://y"}}}
	if mcpMapFingerprint(a) == mcpMapFingerprint(c) {
		t.Fatal("a changed endpoint must change the fingerprint")
	}
}

// A bounce-required element under a disarmed daemon (isolate, test) raises
// the request — owner notice, eventlog — and does not signal the process.
func TestT574BounceRequestIsRaisedNotTakenWhenDisarmed(t *testing.T) {
	configBounceArmed = false
	t.Setenv(configBounceEnv, "0")
	// Not signalling ourselves is the whole test: a SIGHUP here would end
	// the test binary through the harness's default handler.
	bounceForConfig(nil, "/x/config.yaml", []string{"port"})
}
