// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ctxcap

import (
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/handover"
)

// 2026-08-15 numbers: successor at 105336 after a migrate, then a
// SIGHUP-shaped memory wipe, then a governor pass. Must hold.
func TestSIGHUPAfterMigrateHoldsAtIncidentNumbers(t *testing.T) {
	p := Policy{Ceiling: 100_000, MinInterval: 30 * time.Minute}
	// Cold in-memory map: no SinceLastCompaction.
	obs := Observation{
		Agent:      "jevons",
		Context:    105_336,
		HasContext: true,
	}
	// Durable last-rotation: migrate a few seconds ago.
	dir := t.TempDir()
	store := handover.NewRotationStore(dir)
	if err := store.Put(handover.Rotation{
		Agent: "jevons", Kind: handover.KindMigrate,
		At: time.Now().Add(-8 * time.Second).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	since, ok := store.Observe("jevons", time.Now())
	if !ok {
		t.Fatal("persisted migrate not readable after the wipe")
	}
	obs = ApplyPersistedRotation(obs, since, ok)
	d := p.Evaluate(obs)
	if d.Verdict != VerdictHold {
		t.Fatalf("incident fixture verdict=%s want hold (%s)", d.Verdict, d.Reason)
	}

	// Mutation: compact-now on zero last-rotation (ignore the persist).
	naked := Observation{Agent: "jevons", Context: 105_336, HasContext: true}
	if got := p.Evaluate(naked).Verdict; got != VerdictCompact {
		t.Fatalf("mutation control: zero last-rotation verdict=%s want compact", got)
	}
}

func TestSeedOnlySessionHoldsWithoutPersistedRotation(t *testing.T) {
	p := Policy{Ceiling: 100_000}
	d := p.Evaluate(Observation{
		Agent: "jevons", Context: 105_336, HasContext: true, SeedOnly: true,
	})
	if d.Verdict != VerdictHold {
		t.Fatalf("seed-only verdict=%s want hold (%s)", d.Verdict, d.Reason)
	}
}

func TestPostSeedWorkOverCeilingAfterIntervalCompacts(t *testing.T) {
	p := Policy{Ceiling: 100_000, MinInterval: 30 * time.Minute}
	d := p.Evaluate(Observation{
		Agent:             "jevons",
		Context:           105_336,
		HasContext:        true,
		SeedOnly:          false,
		HasLastRotation:   true,
		SinceLastRotation: 31 * time.Minute,
	})
	if d.Verdict != VerdictCompact {
		t.Fatalf("elapsed-interval verdict=%s want compact (%s)", d.Verdict, d.Reason)
	}
}

func TestRotationStoreSurvivesProcessRestart(t *testing.T) {
	dir := t.TempDir()
	if err := handover.NewRotationStore(dir).Put(handover.Rotation{
		Agent: "jevons-po", Kind: handover.KindCompact,
	}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := handover.NewRotationStore(dir).Get("jevons-po")
	if err != nil || !ok {
		t.Fatalf("Get after restart: ok=%v err=%v", ok, err)
	}
	if got.Kind != handover.KindCompact {
		t.Fatalf("kind=%q", got.Kind)
	}
	if _, tok := got.Time(); !tok {
		t.Fatal("At not stamped")
	}
}
