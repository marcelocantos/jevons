// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package landhold_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/landhold"
)

func TestT1040LandHoldBlocksIntegrateAndRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := landhold.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	hold := landhold.Hold{NoMerge: true, NoActivation: true}
	if err := st.SetAgent("jv-t1037-seat-name-hotlink", hold, "owner", "fan-out hold", time.Now()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JEVONS_STATE_DIR", dir)
	t.Setenv("JEVONS_AGENT", "jv-t1037-seat-name-hotlink")

	intCmd := "go run ./cmd/integrate -repo /Users/marcelo/work/github.com/marcelocantos/jevons jv-t1037-seat-name-hotlink"
	act, ok := landhold.CommandAction(intCmd)
	if !ok || act != landhold.ActionIntegrate {
		t.Fatalf("integrate command not detected: %v %v", act, ok)
	}
	ref, err := landhold.CheckSeat(st, "jv-t1037-seat-name-hotlink", "", act)
	if err != nil || ref == nil {
		t.Fatalf("integrate must be refused under hold: ref=%v err=%v", ref, err)
	}

	restartCmd := "nohup scripts/restart-jevonsd.sh &"
	act, ok = landhold.CommandAction(restartCmd)
	if !ok || act != landhold.ActionRestart {
		t.Fatalf("restart command not detected: %v %v", act, ok)
	}
	ref, err = landhold.CheckSeat(st, "jv-t1037-seat-name-hotlink", "", act)
	if err != nil || ref == nil {
		t.Fatalf("restart must be refused under hold: ref=%v err=%v", ref, err)
	}

	ref, err = landhold.CheckSeat(st, "jv-t1037-seat-name-hotlink", "owner lifts hold", act)
	if err != nil || ref != nil {
		t.Fatalf("override must allow: ref=%v err=%v", ref, err)
	}
}

func TestFleetWideHold(t *testing.T) {
	dir := t.TempDir()
	st, err := landhold.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetFleet(landhold.Hold{NoMerge: true}, "owner", "fleet-wide", time.Now()); err != nil {
		t.Fatal(err)
	}
	if !st.EffectiveHold("jv-any-auto").NoMerge {
		t.Fatal("fleet hold must apply to any seat")
	}
}

func TestLandHoldStorePersists(t *testing.T) {
	dir := t.TempDir()
	st, err := landhold.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgent("jv-a", landhold.Hold{NoMerge: true}, "po", "brief", time.Now()); err != nil {
		t.Fatal(err)
	}
	st2, err := landhold.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !st2.EffectiveHold("jv-a").NoMerge {
		t.Fatal("persisted hold missing after reopen")
	}
	if _, err := os.Stat(filepath.Join(dir, "fleet", landhold.StoreFileName)); err != nil {
		t.Fatal(err)
	}
}
