// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// With the claudia daemon present, jevonsd neither stops nor reaps seats on
// its own restart: they are the daemon's, and ReattachFleet reclaims them by
// name over the socket.
func TestDaemonHeldSeatsAreNeitherStoppedNorReaped(t *testing.T) {
	prevAvail, prevReap := brokerAvailable, reapOrphanCursorACP
	t.Cleanup(func() { brokerAvailable, reapOrphanCursorACP = prevAvail, prevReap })
	brokerAvailable = func() bool { return true }
	reaped := 0
	reapOrphanCursorACP = func() []int { reaped++; return nil }

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "cursor-seat", WorkDir: t.TempDir(), SessionID: "s1", Provider: claudia.ProviderCursor}); err != nil {
		t.Fatal(err)
	}
	if n := StopNonAdoptable(reg); n != 0 {
		t.Fatalf("StopNonAdoptable stopped %d daemon-held seats", n)
	}
	t.Setenv("CLAUDIA_NO_BROKER", "1") // no real daemon in the hermetic run; Launch stays direct
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // ReattachFleet returns before launching anything
	_ = ReattachFleetContext(ctx, reg)
	if reaped != 0 {
		t.Fatalf("ReattachFleet reaped %d orphan Cursor clients while the daemon holds the fleet", reaped)
	}
}

// Without a daemon the pre-existing behaviour holds: Cursor seats are
// stopped on upgrade exit.
func TestWithoutDaemonCursorSeatsStillStopOnUpgrade(t *testing.T) {
	prev := brokerAvailable
	t.Cleanup(func() { brokerAvailable = prev })
	brokerAvailable = func() bool { return false }
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "cursor-seat", WorkDir: t.TempDir(), SessionID: "s1", Provider: claudia.ProviderCursor}); err != nil {
		t.Fatal(err)
	}
	if n := StopNonAdoptable(reg); n != 1 {
		t.Fatalf("StopNonAdoptable stopped %d, want 1", n)
	}
}
