// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// seedStore fills an in-memory store with events inside the monitoring
// window: two attributed workers, one orphan (jevons's, lost in-fleet),
// and one foreign session (the owner's own terminal).
func seedStore(t *testing.T, now time.Time) *Store {
	t.Helper()
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	var events []*Event
	n := 0
	add := func(sess, worker string, cost float64) {
		n++
		events = append(events, &Event{
			Timestamp: now.Add(-time.Minute), SessionID: sess, Worker: worker,
			Model: "claude-opus-4-8", Usage: Usage{Output: 1}, CostUSD: cost,
			RequestID: fmt.Sprintf("r-%d", n),
		})
	}
	add("sess-po", "po", 0.50)   // po: $0.50 in a 5m window = $6/hr
	add("sess-doc", "doc", 0.10) // doc: $1.20/hr
	add("sess-ghost", "", 2.00)  // orphan (IsOrphan true) — jevons's lost worker
	add("sess-ghost", "", 1.00)  // same orphan session, second event
	add("sess-owner", "", 5.00)  // foreign — the owner's own session
	if _, err := s.InsertEvents(events); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return s
}

// onlyGhostOrphan classifies the ghost session as an orphan and the
// owner session as foreign.
func onlyGhostOrphan(r BurnRow) bool { return r.SessionID == "sess-ghost" }

func TestMonitorSnapshotRatesAndSignals(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	s := seedStore(t, now)

	cfg := DefaultBudgetConfig()
	cfg.MaxSessions = 3      // 4 burning sessions → session-count signal
	cfg.DailyBudgetUSD = 100 // projection trips
	cfg.MinEventsForKill = 0 // exercise raw rate levels here

	m := NewMonitor(&MonitorArgs{
		Store:    s,
		Config:   func() *BudgetConfig { return cfg },
		IsOrphan: onlyGhostOrphan,
		Now:      func() time.Time { return now },
	})

	snap, err := m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Global = everything = $8.60 in 5m = $103.20/hr.
	if math.Abs(snap.GlobalUSDPerHour-103.2) > 1e-9 {
		t.Fatalf("global rate = %v, want 103.2", snap.GlobalUSDPerHour)
	}
	// Fleet = attributed ($0.60) + orphan ($3.00), NOT the foreign owner
	// session ($5.00) → $3.60 → $43.20/hr.
	if math.Abs(snap.FleetUSDPerHour-43.2) > 1e-9 {
		t.Fatalf("fleet rate = %v, want 43.2 (attributed+orphan, excl. foreign)", snap.FleetUSDPerHour)
	}
	if math.Abs(snap.WorkerUSDPerHour["po"]-6.0) > 1e-9 {
		t.Fatalf("po rate = %v, want 6.0", snap.WorkerUSDPerHour["po"])
	}
	// The one-query burning view leads with the hottest session (owner, $5).
	if len(snap.Sessions) != 4 || snap.Sessions[0].SessionID != "sess-owner" {
		t.Fatalf("burning view wrong: %+v", snap.Sessions)
	}

	got := map[string]Alert{}
	for _, a := range snap.Alerts {
		got[a.Kind+"/"+a.Worker] = a
	}
	// Global $103.20/hr crosses global kill (60) — reported at true level
	// (the enforcer treats global as informational; the monitor doesn't lie).
	if a, ok := got[AlertGlobalRate+"/"]; !ok || a.Level != LevelKill {
		t.Fatalf("global-rate alert = %+v, want kill", got)
	}
	// Fleet $43.20/hr crosses fleet kill (40).
	if a, ok := got[AlertFleetRate+"/"]; !ok || a.Level != LevelKill {
		t.Fatalf("fleet-rate alert = %+v, want kill", got)
	}
	// po at $6/hr crosses the per-worker throttle rung (5).
	if a, ok := got[AlertWorkerRate+"/po"]; !ok || a.Level != LevelThrottle {
		t.Fatalf("worker-rate alert for po = %+v, want throttle", got)
	}
	// doc at $1.20/hr stays under every rung.
	if _, ok := got[AlertWorkerRate+"/doc"]; ok {
		t.Fatal("doc should not alert")
	}
	// 4 sessions > bound of 3.
	if _, ok := got[AlertSessionCount+"/"]; !ok {
		t.Fatal("session-count signal missing")
	}
	// The orphan (not the foreign owner session) is flagged.
	if a, ok := got[AlertOrphanSessions+"/"]; !ok || len(a.Sessions) != 1 || a.Sessions[0] != "sess-ghost" {
		t.Fatalf("orphan signal = %+v, want sess-ghost only", got[AlertOrphanSessions+"/"])
	}
	// Projected overspend trips against the daily budget.
	if _, ok := got[AlertProjection+"/"]; !ok {
		t.Fatal("projected-overspend signal missing")
	}
}

// TestThinRateGuard: a kill-level rate backed by too few events is a
// spike, capped at pause; the same rate with enough events kills.
func TestThinRateGuard(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	// One fat event: $6 in 5m = $72/hr, past the global kill rung (60).
	if _, err := s.InsertEvents([]*Event{{
		Timestamp: now.Add(-time.Minute), SessionID: "spike", Model: "claude-opus-4-8",
		Usage: Usage{Output: 1}, CostUSD: 6.0, RequestID: "one",
	}}); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultBudgetConfig() // MinEventsForKill 30
	m := NewMonitor(&MonitorArgs{Store: s, Config: func() *BudgetConfig { return cfg },
		Now: func() time.Time { return now }})

	snap, _ := m.Snapshot()
	var g *Alert
	for i := range snap.Alerts {
		if snap.Alerts[i].Kind == AlertGlobalRate {
			g = &snap.Alerts[i]
		}
	}
	if g == nil || g.Level != LevelPause {
		t.Fatalf("thin kill-rate not capped at pause: %+v", g)
	}

	// Disable the guard: the same single event now reads kill.
	cfg.MinEventsForKill = 0
	snap, _ = m.Snapshot()
	for i := range snap.Alerts {
		if snap.Alerts[i].Kind == AlertGlobalRate && snap.Alerts[i].Level != LevelKill {
			t.Fatalf("guard disabled but level = %s, want kill", snap.Alerts[i].Level)
		}
	}
}

func TestMonitorHardCeilingAndQuiet(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	s := seedStore(t, now)

	// Quiet config: nothing trips when limits are far away.
	cfg := &BudgetConfig{
		Global: Limits{KillUSDPerHour: 1000},
		Window: Duration(5 * time.Minute),
	}
	m := NewMonitor(&MonitorArgs{Store: s, Config: func() *BudgetConfig { return cfg },
		Now: func() time.Time { return now }})
	snap, err := m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Alerts) != 0 {
		t.Fatalf("quiet config still alerted: %+v", snap.Alerts)
	}

	// Hard ceiling: today's spend ($3.60) ≥ ceiling → kill-level alert.
	cfg.HardCeilingUSDPerDay = 3
	snap, err = m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Alerts) != 1 || snap.Alerts[0].Kind != AlertHardCeiling || snap.Alerts[0].Level != LevelKill {
		t.Fatalf("hard-ceiling alert = %+v", snap.Alerts)
	}
}

func TestMonitorCollectorStaleness(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	s := seedStore(t, now)
	cfg := &BudgetConfig{Window: Duration(5 * time.Minute)}

	lastPoll := now.Add(-10 * time.Second)
	m := NewMonitor(&MonitorArgs{
		Store:             s,
		Config:            func() *BudgetConfig { return cfg },
		CollectorLastPoll: func() time.Time { return lastPoll },
		Now:               func() time.Time { return now },
	})

	snap, err := m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Alerts) != 0 {
		t.Fatalf("fresh collector alerted: %+v", snap.Alerts)
	}

	// A collector that stops polling becomes an alarm, not silence.
	lastPoll = now.Add(-5 * time.Minute)
	snap, _ = m.Snapshot()
	if len(snap.Alerts) != 1 || snap.Alerts[0].Kind != AlertCollectorStale {
		t.Fatalf("stale collector did not alarm: %+v", snap.Alerts)
	}
}

func TestBudgetConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/budget.json"

	// Missing file → defaults, no error.
	cfg, err := LoadBudgetConfig(path)
	if err != nil || cfg.Global.KillUSDPerHour != 60 {
		t.Fatalf("defaults not loaded: %+v, %v", cfg, err)
	}
	if cfg.EffectiveAccounting() != AccountingListPrice {
		t.Fatalf("default accounting = %q, want list_price", cfg.EffectiveAccounting())
	}

	cfg.Global.KillUSDPerHour = 99
	cfg.DeadManIdle = Duration(2 * time.Hour)
	cfg.Accounting = AccountingSubscription
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := LoadBudgetConfig(path)
	if err != nil || back.Global.KillUSDPerHour != 99 || back.DeadManIdle.Std() != 2*time.Hour {
		t.Fatalf("round-trip lost data: %+v, %v", back, err)
	}
	if back.EffectiveAccounting() != AccountingSubscription || !back.IsSubscription() {
		t.Fatalf("subscription accounting not preserved: %+v", back)
	}
}

// TestSubscriptionAccountingHonesty (🎯T137): high API-eq burn under
// subscription accounting surfaces only warn-level USD alerts and never
// a kill hard-ceiling — estimates are not real SuperGrok dollars.
func TestSubscriptionAccountingHonesty(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	s := seedStore(t, now)

	cfg := DefaultBudgetConfig()
	cfg.Accounting = AccountingSubscription
	cfg.MaxSessions = 3
	cfg.DailyBudgetUSD = 100
	cfg.HardCeilingUSDPerDay = 3 // today's spend in seed is high enough
	cfg.MinEventsForKill = 0

	m := NewMonitor(&MonitorArgs{
		Store:    s,
		Config:   func() *BudgetConfig { return cfg },
		IsOrphan: onlyGhostOrphan,
		Now:      func() time.Time { return now },
	})
	snap, err := m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.Accounting != AccountingSubscription || snap.Billable {
		t.Fatalf("snapshot accounting/billable = %q/%v", snap.Accounting, snap.Billable)
	}
	if !containsNotBilled(snap.CurrencyNote) {
		t.Fatalf("currency_note not honest: %q", snap.CurrencyNote)
	}

	for _, a := range snap.Alerts {
		switch a.Kind {
		case AlertGlobalRate, AlertFleetRate, AlertWorkerRate, AlertHardCeiling:
			if a.Level > LevelWarn {
				t.Fatalf("subscription USD alert %s level %s > warn: %s", a.Kind, a.Level, a.Detail)
			}
		}
	}
	// Hard ceiling must still surface (as warn), not vanish.
	gotHard := false
	for _, a := range snap.Alerts {
		if a.Kind == AlertHardCeiling {
			gotHard = true
			if a.Level != LevelWarn {
				t.Fatalf("hard ceiling under subscription = %s, want warn", a.Level)
			}
		}
	}
	if !gotHard {
		t.Fatal("hard ceiling signal missing under subscription")
	}
}

func containsNotBilled(s string) bool {
	return strings.Contains(strings.ToLower(s), "not billed") ||
		strings.Contains(strings.ToLower(s), "subscription")
}

// TestDisabledAccounting derives disabled mode from the flag alone.
func TestDisabledAccounting(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.Disabled = true
	cfg.Accounting = AccountingSubscription // ignored when disabled
	if cfg.EffectiveAccounting() != AccountingDisabled {
		t.Fatalf("got %q", cfg.EffectiveAccounting())
	}
	if cfg.IsSubscription() {
		t.Fatal("disabled must not report as subscription")
	}
}
