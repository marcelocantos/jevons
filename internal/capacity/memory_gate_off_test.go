// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import (
	"strings"
	"testing"
)

// kernelCriticalHost is the worst memory reading the policy can get: the
// kernel itself says critical, which memoryGrindHeadroom zeroes outright
// and no threshold knob can reach. The switch has to survive this, not a
// comfortable snapshot.
func kernelCriticalHost() Snapshot {
	return Snapshot{
		HostMemoryPressure:    MemoryPressureCritical,
		HostMemoryFreePercent: 3,
		HostSwapUsedBytes:     19_000_000_000,
		HostSwapTotalBytes:    19_300_000_000,
		HostLoad1:             2,
		HostCores:             16,
		HostSource:            "test fixture (2026-08-31)",
	}
}

func TestMemoryGateOnHaltsASpawnOnAMeltedHost(t *testing.T) {
	// The control. If this ever stops failing, the switch below proves nothing.
	pol := DefaultPolicy()
	d := AdmitSpawn(SpawnWorker, kernelCriticalHost(), pol)
	if d.Verdict != VerdictDefer || d.Reason != ReasonMemoryGrind {
		t.Fatalf("melted host must refuse a worker pane, got %s/%s", d.Verdict, d.Reason)
	}
}

func TestMemoryGateOffAdmitsTheSameSpawn(t *testing.T) {
	pol := DefaultPolicy()
	pol.MemoryGateOff = true
	d := AdmitSpawn(SpawnWorker, kernelCriticalHost(), pol)
	if d.Verdict != VerdictAdmit {
		t.Fatalf("gate off must admit, got %s/%s: %s", d.Verdict, d.Reason, d.Detail)
	}
}

func TestMemoryGateOffReportsUnknownNotHealthy(t *testing.T) {
	// Eliminated, not relaxed: an invented healthy number would still let
	// memory drag the overall headroom around, which is the variable the
	// owner is trying to remove.
	pol := DefaultPolicy()
	pol.MemoryGateOff = true
	a := Assess(kernelCriticalHost(), pol)
	if a.MemoryHeadroom != unknownHeadroom {
		t.Fatalf("MemoryHeadroom = %v, want unknown (%v)", a.MemoryHeadroom, unknownHeadroom)
	}
	for _, r := range a.Reasons {
		if strings.Contains(r, "memory grind") {
			t.Fatalf("a disabled dimension must not file a reason: %q", r)
		}
	}
}

func TestMemoryGateOffLeavesOtherHaltsAlone(t *testing.T) {
	// The switch is memory-only. A fork bomb must still be refused, or
	// "eliminate one variable" quietly becomes "disable the governor".
	snap := kernelCriticalHost()
	snap.HostLoad1 = 400
	pol := DefaultPolicy()
	pol.MemoryGateOff = true
	a := Assess(snap, pol)
	if a.LoadAverageHeadroom == unknownHeadroom {
		t.Fatal("load-average must still be assessed with the memory gate off")
	}
}
