// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"
	"testing"
)

func TestBootAlwaysReattachFleet(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if !strings.Contains(body, "upgrade.ReattachSeatsContext(ctx, registry,") {
		t.Fatal("boot does not call ReattachSeats — crash/drain will Launch-only")
	}
	if strings.Contains(body, "registry.StartAll()") {
		t.Fatal("boot still has Launch-only StartAll — leftover+Launch is the ghost fleet")
	}
}

func TestContextCeilingSourceDoesNotRemint(t *testing.T) {
	src, err := os.ReadFile("ctxcap.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	for _, bad := range []string{"PrepareCompaction", "compactFleetAgent", "CompactOverseer", "SeedSuccessor", ".rotate("} {
		if strings.Contains(body, bad) {
			t.Fatalf("ctxcap.go still remints via %s", bad)
		}
	}
	if !strings.Contains(body, "not reminting") {
		t.Fatal("ctxcap.go lost the observe-only log")
	}
	if !strings.Contains(body, "reporting unworkable") {
		t.Fatal("ctxcap.go lost the 🎯T417 unworkable report path")
	}
}

// 🎯T778: the owner's chat must not wait on fleet seats. The overseer
// reattaches alone and first, the cockpit converge loop starts next, and only
// then does the rest of the fleet start.
func TestT778BootOverseerThenConvergeThenFleet(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	overseer := strings.Index(body, "upgrade.ReattachSeatsContext(ctx, registry, isOverseerSeat, 1)")
	converge := strings.Index(body, "srv.StartCockpitConverge(ctx, server.DefaultCockpitInterval)")
	fleet := strings.Index(body, "upgrade.DefaultReattachConcurrency))")
	if overseer < 0 || converge < 0 || fleet < 0 {
		t.Fatalf("boot lost a T778 marker: overseer=%d converge=%d fleet=%d", overseer, converge, fleet)
	}
	if !(overseer < converge && converge < fleet) {
		t.Fatalf("boot order wrong: overseer=%d converge=%d fleet=%d", overseer, converge, fleet)
	}
	if strings.Contains(body, "upgrade.ReattachFleetContext(") {
		t.Fatal("boot reattaches the whole fleet in one serial-blocking call")
	}
}
