// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

// 🎯T781 — fleet-run heavy suites share one CPU knob with the capacity
// governor (state_dir/capacity.json), not a per-suite Makefile constant.

// DefaultTestMaxProcs caps GOMAXPROCS for fleet-seat heavy runs. Matches the
// shipped Makefile TEST_CPUS default on a 16-core host.
const DefaultTestMaxProcs = 8

// DefaultTestPkgParallel is the default `go test -p` / gotest -p cap. Kept
// below DefaultTestMaxProcs because suites that shell out to builds multiply
// schedulers per package binary.
const DefaultTestPkgParallel = 4

// DefaultTestNice is the nice(1) increment applied to fleet heavy runs.
const DefaultTestNice = 5

// TestBudget is the CPU envelope for one heavy suite run.
type TestBudget struct {
	MaxProcs int // GOMAXPROCS for the suite and its children
	PkgPar   int // go test / gotest -p
	Nice     int // nice increment; 0 means do not wrap with nice
}

// TestBudget derives the fleet heavy-suite envelope from policy. Zero fields
// in the policy fall back to the shipped defaults above.
func (p *Policy) TestBudget() TestBudget {
	if p == nil {
		p = DefaultPolicy()
	}
	max := p.TestMaxProcs
	if max <= 0 {
		max = DefaultTestMaxProcs
	}
	par := p.TestPkgParallel
	if par <= 0 {
		par = max / 2
		if par < 1 {
			par = 1
		}
	}
	nice := p.TestNice
	if nice == 0 {
		nice = DefaultTestNice
	} else if nice < 0 {
		nice = 0
	}
	return TestBudget{MaxProcs: max, PkgPar: par, Nice: nice}
}
