// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T1066 — PO review, 2026-10-12: SweepFleetRecover's FleetRecoverUnstick
// action calls args.Interrupt(name) DIRECTLY, bypassing deliverByName /
// deliverToSenderModeWithRequestID entirely — neither T1066 gate
// (ControlDeliverSubmit, on the drain path and the immediate-submit path)
// ever runs for this call, because it is not routed through either. A
// provider-outage-driven interrupt on a stuck-busy seat cuts the live turn
// of a seat that fleet/agent intent says should not be touched at all —
// the exact bypass the review named. This test is RED before the fix in
// this commit: args.Interrupt fires regardless of blocked_owner.
//
// The text-delivery half of fleet_recover's push (plain submit, not
// interrupt) already goes through the gated chokepoint and was covered by
// 37536b38 / edf544e6. This closes the one call that skipped it.
func TestT1066FleetRecoverUnstickDoesNotInterruptUnderBlockedOwner(t *testing.T) {
	const name = "jv-t1020-auto"
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, Purpose: claudia.PurposeWork, TargetID: "T1020",
		SessionID: "s1", AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}

	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetFleet(fleetintent.BlockedOwner, "owner", "stabilization pause", time.Now()); err != nil {
		t.Fatal(err)
	}

	act := NewIdleActivityTracker()
	now := time.Unix(1_700_000_000, 0)
	act.now = func() time.Time { return now }
	// Stuck-busy: prompt in flight, no progress for longer than the stuck
	// timeout — exactly the shape that classifies FleetRecoverUnstick.
	act.by[name] = IdleActivity{Phase: "working", Updated: now.Add(-2 * time.Minute)}

	interrupted := false
	pushed := 0
	reps := SweepFleetRecover(FleetRecoverSweepArgs{
		Reg:      reg,
		Activity: act,
		Intent: func(n string) fleetintent.Decision {
			return store.Snapshot().Allow(n, fleetintent.ControlDeliverSubmit)
		},
		ProcessRunning: func(string) bool { return true },
		PromptInFlight: func(string) bool { return true },
		Interrupt: func(string) error {
			interrupted = true
			return nil
		},
		Push: func(target string, interrupt bool, event, text string) error {
			pushed++
			return nil
		},
		Now:          now,
		OverseerName: "jevons",
		StuckTimeout: 90 * time.Second,
	})

	if interrupted {
		t.Fatal("fleet recover interrupted a seat's live turn while fleet intent was blocked_owner — the T1066 gates do not cover this call")
	}
	if pushed != 0 {
		t.Fatalf("fleet recover pushed a message under blocked_owner intent: %d", pushed)
	}
	var found bool
	for _, r := range reps {
		if r.Name == name {
			found = true
			if r.Delivered || r.Interrupted {
				t.Fatalf("report claims action under blocked_owner: %+v", r)
			}
		}
	}
	if !found {
		t.Fatalf("agent %q not reported: %+v", name, reps)
	}
}
