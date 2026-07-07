// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"fmt"
	"math"
	"testing"
	"time"
)

// seedStore fills an in-memory store with events inside the monitoring
// window: two attributed workers and one unattributed session.
func seedStore(t *testing.T, now time.Time) *Store {
	t.Helper()
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	var events []*Event
	add := func(sess, worker string, cost float64) {
		events = append(events, &Event{
			Timestamp: now.Add(-time.Minute), SessionID: sess, Worker: worker,
			Model: "claude-opus-4-8", Usage: Usage{Output: 1}, CostUSD: cost,
			RequestID: fmt.Sprintf("r-%s-%f", sess, cost),
		})
	}
	add("sess-po", "po", 0.50)   // po: $0.50 in a 5m window = $6/hr
	add("sess-doc", "doc", 0.10) // doc: $1.20/hr
	add("sess-ghost", "", 2.00)  // unattributed: $24/hr — the incident shape
	add("sess-ghost", "", 1.00)  // same session, second event
	if _, err := s.InsertEvents(events); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return s
}

func TestMonitorSnapshotRatesAndSignals(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	s := seedStore(t, now)

	cfg := DefaultBudgetConfig()
	cfg.MaxSessions = 2      // 3 burning sessions → session-count signal
	cfg.DailyBudgetUSD = 100 // projection: 3.6 spent + 43.2*12h ≈ 522 → trips

	m := NewMonitor(&MonitorArgs{
		Store:    s,
		Config:   func() *BudgetConfig { return cfg },
		IsOrphan: func(r BurnRow) bool { return r.Worker == "" }, // ghost = orphan
		Now:      func() time.Time { return now },
	})

	snap, err := m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// $3.60 total in a 5-minute window = $43.20/hr global.
	if math.Abs(snap.GlobalUSDPerHour-43.2) > 1e-9 {
		t.Fatalf("global rate = %v, want 43.2", snap.GlobalUSDPerHour)
	}
	// Fleet (attributed) = $0.60 → $7.20/hr.
	if math.Abs(snap.FleetUSDPerHour-7.2) > 1e-9 {
		t.Fatalf("fleet rate = %v, want 7.2", snap.FleetUSDPerHour)
	}
	if math.Abs(snap.WorkerUSDPerHour["po"]-6.0) > 1e-9 {
		t.Fatalf("po rate = %v, want 6.0", snap.WorkerUSDPerHour["po"])
	}
	// The one-query burning view leads with the hottest session.
	if len(snap.Sessions) != 3 || snap.Sessions[0].SessionID != "sess-ghost" {
		t.Fatalf("burning view wrong: %+v", snap.Sessions)
	}

	got := map[string]Alert{}
	for _, a := range snap.Alerts {
		got[a.Kind+"/"+a.Worker] = a
	}
	// Global $43.20/hr crosses pause (40) but not kill (60).
	if a, ok := got[AlertGlobalRate+"/"]; !ok || a.Level != LevelPause {
		t.Fatalf("global-rate alert = %+v, want pause", got)
	}
	// Fleet $7.20/hr crosses warn (5) but not throttle (10).
	if a, ok := got[AlertFleetRate+"/"]; !ok || a.Level != LevelWarn {
		t.Fatalf("fleet-rate alert = %+v, want warn", got)
	}
	// po at $6/hr crosses the per-worker throttle rung (5).
	if a, ok := got[AlertWorkerRate+"/po"]; !ok || a.Level != LevelThrottle {
		t.Fatalf("worker-rate alert for po = %+v, want throttle", got)
	}
	// doc at $1.20/hr stays under every rung.
	if _, ok := got[AlertWorkerRate+"/doc"]; ok {
		t.Fatal("doc should not alert")
	}
	// 3 sessions > bound of 2.
	if _, ok := got[AlertSessionCount+"/"]; !ok {
		t.Fatal("session-count signal missing")
	}
	// The unattributed burner is flagged as an orphan.
	if a, ok := got[AlertOrphanSessions+"/"]; !ok || len(a.Sessions) != 1 || a.Sessions[0] != "sess-ghost" {
		t.Fatalf("orphan signal = %+v, want sess-ghost", got[AlertOrphanSessions+"/"])
	}
	// Projected overspend trips against the daily budget.
	if _, ok := got[AlertProjection+"/"]; !ok {
		t.Fatal("projected-overspend signal missing")
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

func TestBudgetConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/budget.json"

	// Missing file → defaults, no error.
	cfg, err := LoadBudgetConfig(path)
	if err != nil || cfg.Global.KillUSDPerHour != 60 {
		t.Fatalf("defaults not loaded: %+v, %v", cfg, err)
	}

	cfg.Global.KillUSDPerHour = 99
	cfg.DeadManIdle = Duration(2 * time.Hour)
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := LoadBudgetConfig(path)
	if err != nil || back.Global.KillUSDPerHour != 99 || back.DeadManIdle.Std() != 2*time.Hour {
		t.Fatalf("round-trip lost data: %+v, %v", back, err)
	}
}
