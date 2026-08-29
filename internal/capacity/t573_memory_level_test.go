// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import (
	"strings"
	"testing"
)

// The 2026-08-29 refusal, as numbers: a 137 GB host at 75% free with swap
// 15.8 of 17 GB used — a scar of past pressure that macOS never heals. The
// swap-keyed gate refused every pane for over an hour (🎯T573).
func scarredHost() Snapshot {
	return Snapshot{
		HostLoad1:             6,
		HostCores:             16,
		HostSwapUsedBytes:     gibBytes(16.15), // 95%
		HostSwapTotalBytes:    gibBytes(17),
		HostMemoryFreePercent: 75,
		HostMemoryPressure:    MemoryPressureNormal,
		HostSource:            "test fixture (2026-08-29)",
	}
}

func TestT573HighSwapWithHighMemoryLevelAdmits(t *testing.T) {
	pol := DefaultPolicy()
	a := Assess(scarredHost(), pol)
	if MemoryGrindBlocks(a) {
		t.Fatalf("scarred swap at 75%% free fired memory grind: %+v", a)
	}
	if a.Pressure != PressureNormal {
		t.Fatalf("pressure %s, want normal: %+v", a.Pressure, a)
	}
	if d := AdmitSpawn(SpawnWorker, scarredHost(), pol); !d.Admitted() {
		t.Fatalf("worker refused on a host at 75%% free: %+v", d)
	}
	// The default swap fraction (not the 1.0 stopgap) must not halt either.
	if pol.SwapCriticalFraction != DefaultSwapCriticalFraction {
		t.Fatalf("fixture policy is not the shipped default")
	}
}

func TestT573LowMemoryLevelRefuses(t *testing.T) {
	pol := DefaultPolicy()
	snap := scarredHost()
	snap.HostSwapUsedBytes = gibBytes(1) // swap fine; memory is not
	snap.HostMemoryFreePercent = 15
	a := Assess(snap, pol)
	if !MemoryGrindBlocks(a) {
		t.Fatalf("15%% free did not fire memory grind: %+v", a)
	}
	if d := AdmitSpawn(SpawnWorker, snap, pol); d.Admitted() || d.Reason != ReasonMemoryGrind {
		t.Fatalf("worker at 15%% free: %+v, want refuse/%s", d, ReasonMemoryGrind)
	}
	var named bool
	for _, r := range a.Reasons {
		if strings.Contains(r, "kernel pressure") && strings.Contains(r, "advisory") {
			named = true
		}
	}
	if !named {
		t.Errorf("reasons do not name the kernel level with swap advisory: %q", a.Reasons)
	}
}

func TestT573KernelCriticalPressureRefusesRegardlessOfLevel(t *testing.T) {
	snap := scarredHost()
	snap.HostMemoryPressure = MemoryPressureCritical
	if d := AdmitSpawn(SpawnWorker, snap, DefaultPolicy()); d.Admitted() {
		t.Fatalf("kernel pressure critical admitted a worker: %+v", d)
	}
	snap.HostMemoryPressure = MemoryPressureWarn
	a := Assess(snap, DefaultPolicy())
	if MemoryGrindBlocks(a) || a.Pressure != PressureElevated {
		t.Fatalf("warn should be elevated, not a halt: %+v", a)
	}
}

// Swap stays the fallback reading only when no kernel memory level was
// read — the melted-host fixtures (🎯T463) keep binding on that path.
func TestT573SwapIsFallbackOnlyWhenMemoryLevelUnread(t *testing.T) {
	snap := scarredHost()
	snap.HostMemoryPressure = ""
	snap.HostMemoryFreePercent = 0
	if d := AdmitSpawn(SpawnWorker, snap, DefaultPolicy()); d.Admitted() {
		t.Fatalf("with no kernel level, 95%% swap must still refuse: %+v", d)
	}
}
