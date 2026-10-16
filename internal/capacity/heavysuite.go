// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import "runtime"

// DefaultHeavySuiteMaxProcs is the shipped ceiling when Policy.HeavySuiteMaxProcs
// is unset and the host has at least this many cores. It matches the Makefile's
// TEST_CPUS default so `make test-go` and a gated bare `go test` agree.
const DefaultHeavySuiteMaxProcs = 8

// DefaultHeavySuiteNice is the niceness applied to a heavy suite so interactive
// work keeps the machine (🎯T781). Higher is nicer to everything else.
const DefaultHeavySuiteNice = 5

// HeavySuiteBudget is the CPU share one heavy test suite may take (🎯T781).
// Gate and gotest both resolve through ResolveHeavySuiteBudget so the fleet
// does not grow a second, per-suite constant.
//
//	MaxProcs         → GOMAXPROCS for the suite process tree
//	PackageParallel  → go test -p / gotest -p (how many package binaries at once)
//	Nice             → niceness; 0 means leave the priority alone
type HeavySuiteBudget struct {
	MaxProcs        int `json:"max_procs"`
	PackageParallel int `json:"package_parallel"`
	Nice            int `json:"nice"`
}

// ResolveHeavySuiteBudget turns the single capacity knob into the three
// controls a suite actually needs. cores is the host core count (runtime.NumCPU
// at the call site); ≤0 falls back to runtime.NumCPU so a forgotten argument
// never resolves to a zero budget.
//
// One knob: Policy.HeavySuiteMaxProcs. Everything else is derived:
//
//   - 0 → min(DefaultHeavySuiteMaxProcs, max(1, cores/2))
//   - PackageParallel → max(1, MaxProcs/2)  (the multiplier that actually
//     saturates when suites shell out to `go build`)
//   - Nice → DefaultHeavySuiteNice when the budget is below the host core
//     count; 0 when the owner has asked for the whole machine
func ResolveHeavySuiteBudget(pol *Policy, cores int) HeavySuiteBudget {
	if pol == nil {
		pol = DefaultPolicy()
	}
	if cores <= 0 {
		cores = runtime.NumCPU()
	}
	if cores < 1 {
		cores = 1
	}

	max := pol.HeavySuiteMaxProcs
	if max <= 0 {
		half := cores / 2
		if half < 1 {
			half = 1
		}
		max = half
		if max > DefaultHeavySuiteMaxProcs {
			max = DefaultHeavySuiteMaxProcs
		}
	}
	if max > cores {
		// Never claim more cores than the host has — that is not a budget,
		// it is a no-op dressed as one.
		max = cores
	}
	if max < 1 {
		max = 1
	}

	pkg := max / 2
	if pkg < 1 {
		pkg = 1
	}

	nice := 0
	if max < cores {
		nice = DefaultHeavySuiteNice
	}

	return HeavySuiteBudget{
		MaxProcs:        max,
		PackageParallel: pkg,
		Nice:            nice,
	}
}
