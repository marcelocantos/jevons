// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

func TestT966FleetOwnerPausedIntent(t *testing.T) {
	dir := t.TempDir()
	s, _, _ := t418Daemon(t, dir)
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	if s.FleetOwnerPaused() {
		t.Fatal("precondition: fleet not paused")
	}
	if err := s.PauseFleetOwner("owner", "test"); err != nil {
		t.Fatal(err)
	}
	if !s.FleetOwnerPaused() {
		t.Fatal("expected blocked_owner after pause")
	}
	if err := s.ResumeFleetOwner("owner", "test"); err != nil {
		t.Fatal(err)
	}
	if s.FleetOwnerPaused() {
		t.Fatal("expected working after resume")
	}
}
