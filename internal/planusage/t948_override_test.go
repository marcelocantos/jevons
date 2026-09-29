// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T948: an owner override paints the plan in its band, keeps the seats on
// it, and leaves the readings alone; clearing it restores the readings'
// verdict; a file gone bad keeps the last good override.
func TestT948OwnerOverrideKeepsTheFleetOnAHotPlan(t *testing.T) {
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	th := DefaultThresholds()
	hot := Snapshot{Backends: []Backend{{
		Provider: "claude", Status: StatusAvailable,
		Windows: []Window{bandWindow(WindowWeekly, 92, 8, now, 0.5)},
	}}}
	seats := []AgentRef{{Name: "po", Provider: "anthropic"}}
	if got := WithBands(hot, now, th).Backends[0].Windows[0].Band; got == string(BandOK) {
		t.Fatalf("fixture is not hot: band %q", got)
	}
	if !MigrateOff(hot.Backends[0], now, th) {
		t.Fatal("fixture does not ask seats to leave")
	}

	path := filepath.Join(t.TempDir(), OverrideFile)
	store := NewOverrideStore(path)
	fanned := 0
	store.OnChange(func() { fanned++ })
	const reason = "Owner has a Claude reset available; spend it before other plans."
	if err := store.Set("claude", Override{Band: BandOK, Reason: reason, SetAt: now}); err != nil {
		t.Fatal(err)
	}
	if fanned != 1 {
		t.Fatalf("a set re-fanned the cockpit %d times, want 1", fanned)
	}

	held := store.Apply(hot)
	be := held.Backends[0]
	if be.Override == nil || be.Override.Reason != reason {
		t.Fatalf("override not carried on the backend: %+v", be.Override)
	}
	if hot.Backends[0].Override != nil {
		t.Fatal("Apply wrote onto the shared snapshot")
	}
	w := WithBands(held, now, th).Backends[0].Windows[0]
	if w.Band != string(BandOK) {
		t.Fatalf("overridden band %q, want ok", w.Band)
	}
	if *w.UsedPercent != 92 {
		t.Fatalf("the override changed the reading: used %v", *w.UsedPercent)
	}
	if MigrateOff(be, now, th) || MintIneligible(be, now, th) || !DestEligible(be, now, th) {
		t.Fatal("an overridden plan still reads as one to leave")
	}
	acts := PlanDecisions(held, seats, now, th)
	if len(acts) != 1 || acts[0].Action != claudia.SeatStay || acts[0].From != "claude" {
		t.Fatalf("decisions = %+v, want the seat to stay on claude", acts)
	}
	if len(PlanActions(held, seats, now, th)) != 0 {
		t.Fatal("the sweep still has a move for a seat on an overridden plan")
	}

	// A file gone bad keeps the last good override.
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if store.Apply(hot).Backends[0].Override == nil {
		t.Fatal("a malformed file dropped the owner's override")
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("a malformed file loads without an error")
	}

	// Cleared, the readings decide again.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := store.Clear("claude"); err != nil {
		t.Fatal(err)
	}
	if got := store.Apply(hot).Backends[0].Override; got != nil {
		t.Fatalf("cleared override still applied: %+v", got)
	}
}
