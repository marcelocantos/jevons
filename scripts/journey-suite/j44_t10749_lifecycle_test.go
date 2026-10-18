// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestT10749ControlFactsNeedATerminalReceipt(t *testing.T) {
	open := controlFacts{usage: 1, receipts: []string{"req started"}}
	if open.terminal() {
		t.Fatal("a started receipt is not a finished turn")
	}
	unknown := controlFacts{usage: 1, receipts: []string{"req unknown"}}
	if unknown.terminal() {
		t.Fatal("an unknown receipt is not a finished turn")
	}
	done := controlFacts{usage: 2, receipts: []string{"req completed"}}
	if !done.terminal() {
		t.Fatal("a completed receipt is a finished turn")
	}
	if !done.equal(done) {
		t.Fatal("a fact set must equal itself")
	}
	grew := done
	grew.usage = 3
	if done.equal(grew) {
		t.Fatal("a larger usage count must not compare equal")
	}
}
