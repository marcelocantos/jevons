// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import "testing"

func TestTestBudgetDefaults(t *testing.T) {
	b := DefaultPolicy().TestBudget()
	if b.MaxProcs != DefaultTestMaxProcs {
		t.Fatalf("MaxProcs = %d, want %d", b.MaxProcs, DefaultTestMaxProcs)
	}
	if b.PkgPar != DefaultTestPkgParallel {
		t.Fatalf("PkgPar = %d, want %d", b.PkgPar, DefaultTestPkgParallel)
	}
	if b.Nice != DefaultTestNice {
		t.Fatalf("Nice = %d, want %d", b.Nice, DefaultTestNice)
	}
}

func TestTestBudgetPolicyOverride(t *testing.T) {
	pol := DefaultPolicy()
	pol.TestMaxProcs = 12
	pol.TestPkgParallel = 3
	pol.TestNice = -1
	b := pol.TestBudget()
	if b.MaxProcs != 12 || b.PkgPar != 3 || b.Nice != 0 {
		t.Fatalf("override budget = %+v", b)
	}
}
