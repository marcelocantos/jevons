// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// TestT966OracleIdleSeatNoNewTurnDuringBlockedOwner is the hermetic half of
// the 🎯T966 journey oracle: fleet blocked_owner must refuse ControlSpawn and
// ControlDeliverStart so no seat begins a new provider turn while paused.
// Queued overseer delivery after resume is covered by J40 and
// internal/server TestT1074BlockedOwnerHoldsOverseerNotifyDrain.
func TestT966OracleIdleSeatNoNewTurnDuringBlockedOwner(t *testing.T) {
	dir := t.TempDir()
	s, _, _ := t418Daemon(t, dir)
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	if err := s.PauseFleetOwner("owner", "oracle"); err != nil {
		t.Fatal(err)
	}
	name := "jv-t966-oracle"
	if dec := s.AllowFleetControl(name, fleetintent.ControlSpawn); dec.Allow {
		t.Fatalf("spawn allowed under owner pause: %+v", dec)
	}
	if dec := s.AllowFleetControl(name, fleetintent.ControlDeliverStart); dec.Allow {
		t.Fatalf("deliver start allowed under owner pause: %+v", dec)
	}
}
