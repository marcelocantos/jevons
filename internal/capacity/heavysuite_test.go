// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import "testing"

// 🎯T781: one knob, three controls. The defaults must match the Makefile's
// TEST_CPUS=8 / TEST_PKG_PAR=4 on a 16-core host so `make test-go` and a
// gated bare `go test` do not disagree about how much of the machine they
// may take.
func TestT781HeavySuiteBudgetDefaultsOnSixteenCores(t *testing.T) {
	b := ResolveHeavySuiteBudget(DefaultPolicy(), 16)
	if b.MaxProcs != 8 {
		t.Fatalf("MaxProcs = %d, want 8 (DefaultHeavySuiteMaxProcs on a 16-core host)", b.MaxProcs)
	}
	if b.PackageParallel != 4 {
		t.Fatalf("PackageParallel = %d, want 4 (MaxProcs/2)", b.PackageParallel)
	}
	if b.Nice != DefaultHeavySuiteNice {
		t.Fatalf("Nice = %d, want %d (budget is below host cores)", b.Nice, DefaultHeavySuiteNice)
	}
}

// A two-core host must still get a usable budget — never zero, never more
// than the host has.
func TestT781HeavySuiteBudgetOnTwoCores(t *testing.T) {
	b := ResolveHeavySuiteBudget(DefaultPolicy(), 2)
	if b.MaxProcs != 1 {
		t.Fatalf("MaxProcs = %d, want 1 on a 2-core host", b.MaxProcs)
	}
	if b.PackageParallel != 1 {
		t.Fatalf("PackageParallel = %d, want 1", b.PackageParallel)
	}
	if b.Nice != DefaultHeavySuiteNice {
		t.Fatalf("Nice = %d, want %d", b.Nice, DefaultHeavySuiteNice)
	}
}

// The single knob is Policy.HeavySuiteMaxProcs. Setting it must move both
// GOMAXPROCS and -p, and asking for the whole machine must drop nice.
func TestT781HeavySuiteBudgetHonoursTheKnob(t *testing.T) {
	pol := DefaultPolicy()
	pol.HeavySuiteMaxProcs = 6
	b := ResolveHeavySuiteBudget(pol, 16)
	if b.MaxProcs != 6 || b.PackageParallel != 3 {
		t.Fatalf("budget = %+v, want MaxProcs=6 PackageParallel=3", b)
	}
	if b.Nice != DefaultHeavySuiteNice {
		t.Fatalf("Nice = %d, want %d while under host cores", b.Nice, DefaultHeavySuiteNice)
	}

	pol.HeavySuiteMaxProcs = 16
	full := ResolveHeavySuiteBudget(pol, 16)
	if full.MaxProcs != 16 || full.Nice != 0 {
		t.Fatalf("full-machine budget = %+v, want MaxProcs=16 Nice=0", full)
	}
}

// A knob above the host core count is clamped, not a free pass to claim
// cores that do not exist.
func TestT781HeavySuiteBudgetClampsToCores(t *testing.T) {
	pol := DefaultPolicy()
	pol.HeavySuiteMaxProcs = 64
	b := ResolveHeavySuiteBudget(pol, 4)
	if b.MaxProcs != 4 {
		t.Fatalf("MaxProcs = %d, want 4 (clamped to cores)", b.MaxProcs)
	}
	if b.Nice != 0 {
		t.Fatalf("Nice = %d, want 0 when budget equals cores", b.Nice)
	}
}

// A nil policy is the shipped default, not a panic and not an unlimited run.
func TestT781HeavySuiteBudgetNilPolicyIsDefault(t *testing.T) {
	b := ResolveHeavySuiteBudget(nil, 16)
	def := ResolveHeavySuiteBudget(DefaultPolicy(), 16)
	if b != def {
		t.Fatalf("nil policy = %+v, want default %+v", b, def)
	}
}
