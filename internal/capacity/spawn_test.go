// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import "testing"

// 🎯T460 §4: the 2026-08-15 numbers classify critical and refuse a heavy
// spawn; the idle-host control admits. An over-broad fix that refuses
// everything fails the control.
func TestT460MeltedHostRefusesWorkerSpawn(t *testing.T) {
	d := AdmitSpawn(SpawnWorker, meltedHost(), DefaultPolicy())
	if d.Admitted() {
		t.Fatalf("worker spawn admitted on melted host: %+v", d)
	}
	if d.Reason != ReasonHostSaturated {
		t.Errorf("reason %q, want %q", d.Reason, ReasonHostSaturated)
	}
	a := Assess(meltedHost(), DefaultPolicy())
	if a.Pressure != PressureCritical {
		t.Fatalf("melted host pressure %s, want critical", a.Pressure)
	}
	if !BlocksUnattendedSpawn(a) {
		t.Fatal("BlocksUnattendedSpawn false at critical — T155/T193/T325.1 would keep kicking")
	}
}

func TestT460IdleHostAdmitsWorkerSpawn(t *testing.T) {
	d := AdmitSpawn(SpawnWorker, idleHost(), DefaultPolicy())
	if !d.Admitted() {
		t.Fatalf("worker spawn refused on idle host: %+v", d)
	}
	if BlocksUnattendedSpawn(Assess(idleHost(), DefaultPolicy())) {
		t.Fatal("idle host blocked unattended spawn — over-broad")
	}
}

func TestT460OwnerAndRepairNeverBlocked(t *testing.T) {
	for _, kind := range []SpawnKind{SpawnOwner, SpawnControlRepair} {
		d := AdmitSpawn(kind, meltedHost(), DefaultPolicy())
		if !d.Admitted() {
			t.Errorf("%s spawn was %q on melted host, want admitted (%s)", kind, d.Verdict, d.Detail)
		}
	}
}

// 🎯T460 §5: removing the swap dimension makes the swap-exhausted / quiet-
// run-queue fixture read healthy again and must go RED.
func TestT460MutationDroppingSwapAdmitsTheIncident(t *testing.T) {
	snap := Snapshot{
		HostLoad1:          8, // quiet run queue
		HostCores:          meltedCores,
		HostSwapUsedBytes:  meltedSwapUsed,
		HostSwapTotalBytes: meltedSwapTotal,
	}
	if d := AdmitSpawn(SpawnWorker, snap, DefaultPolicy()); d.Admitted() {
		t.Fatalf("swap-exhausted quiet host admitted a worker: %+v", d)
	}

	// Mutant: same numbers with swap unread.
	blind := snap
	blind.HostSwapUsedBytes = 0
	blind.HostSwapTotalBytes = 0
	if d := AdmitSpawn(SpawnWorker, blind, DefaultPolicy()); !d.Admitted() {
		t.Fatalf("mutant (no swap) still refused: %+v — this test is not detecting the mutation", d)
	}
}

func TestT460ClassifySpawnKind(t *testing.T) {
	cases := []struct {
		purpose, name string
		want          SpawnKind
	}{
		{"overseer", "jevons", SpawnOwner},
		{"work", "jevons", SpawnOwner},
		{"work", "jevons-sentinel", SpawnControlRepair},
		{"work", "jevons-watchdog", SpawnControlRepair},
		{"work", "jv-t460-auto", SpawnWorker},
		{"aside", "idea-1", SpawnWorker},
		{"", "jevons-po", SpawnWorker},
	}
	for _, tc := range cases {
		if got := ClassifySpawnKind(tc.purpose, tc.name); got != tc.want {
			t.Errorf("ClassifySpawnKind(%q, %q) = %q, want %q", tc.purpose, tc.name, got, tc.want)
		}
	}
}

func TestT460GovernorNilAdmits(t *testing.T) {
	var g *Governor
	d := g.AdmitSpawn(SpawnWorker, "jv-x")
	if !d.Admitted() {
		t.Fatalf("nil governor refused: %+v", d)
	}
}

func TestT460GovernorUsesLiveSnapshot(t *testing.T) {
	g := NewGovernor(GovernorArgs{Snapshot: meltedHost})
	if d := g.AdmitSpawn(SpawnWorker, "jv-heavy"); d.Admitted() {
		t.Fatalf("governor admitted worker on melted snapshot: %+v", d)
	}
	g = NewGovernor(GovernorArgs{Snapshot: idleHost})
	if d := g.AdmitSpawn(SpawnWorker, "jv-heavy"); !d.Admitted() {
		t.Fatalf("governor refused worker on idle snapshot: %+v", d)
	}
}
