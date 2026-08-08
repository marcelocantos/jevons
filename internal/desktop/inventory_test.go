// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package desktop_test

// 🎯T27.7 acceptance oracle (2): supersedes mnemo T85/T86/T89 with a
// checkable inventory of equivalent signals reachable from the Jevons head.

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/desktop"
)

func TestMnemoSupersessionInventoryComplete(t *testing.T) {
	inv := desktop.MnemoSupersessionInventory()
	if miss := desktop.InventoryComplete(inv); len(miss) > 0 {
		t.Fatalf("inventory missing or empty reachability: %v", miss)
	}
	// Every superseded target is named in the catalog.
	wantTargets := map[string]bool{"T85": false, "T86": false, "T89": false}
	for _, s := range inv {
		for tID := range wantTargets {
			if strings.Contains(s.Source, tID) {
				wantTargets[tID] = true
			}
		}
		if !strings.Contains(strings.ToLower(s.Reachability), "head") &&
			!strings.Contains(s.Reachability, "provider") &&
			!strings.Contains(s.Reachability, "/api/") &&
			!strings.Contains(s.Reachability, "section") {
			t.Errorf("signal %s reachability does not reference head/provider path: %q",
				s.ID, s.Reachability)
		}
	}
	for tID, ok := range wantTargets {
		if !ok {
			t.Errorf("no inventory entry sources mnemo %s", tID)
		}
	}
	if len(desktop.SupersededMnemoTargets) != 3 {
		t.Fatalf("SupersededMnemoTargets = %v", desktop.SupersededMnemoTargets)
	}
}

func TestInventoryRejectsEmptyReachability(t *testing.T) {
	bad := []desktop.Signal{{ID: "t85.status_item", Reachability: ""}}
	if miss := desktop.InventoryComplete(bad); len(miss) == 0 {
		t.Fatal("expected incomplete inventory")
	}
}
