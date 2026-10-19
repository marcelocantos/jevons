// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"testing"
)

// 🎯T833: the T790 pending result is "in flight"; a genuine switch failure
// (control) is not.
func TestIsSwitchPending(t *testing.T) {
	pending := errors.New("jevons_agent_switch_model: switch of \"x\" → claude is still running past this call's deadline and CONTINUES in the daemon (🎯T790): it is not cancelled")
	if !isSwitchPending(pending) {
		t.Fatal("T790 pending text not recognised")
	}
	// Trimmed MCP error still carries the early phrase.
	trimmed := errors.New("jevons_agent_switch_model: switch of \"orch-switch-12345\" → codex is still running past this call's deadline and CONTINUES in the daemon (🎯T790)")
	if !isSwitchPending(trimmed) {
		t.Fatal("trimmed T790 pending text not recognised")
	}
	for _, e := range []error{
		nil,
		errors.New("jevons_agent_switch_model: provider unavailable"),
		errors.New("switch refused: the conversation does not fit"),
		errors.New("switch jevons: a turn is in flight; wait for it, or force to interrupt it"),
	} {
		if isSwitchPending(e) {
			t.Fatalf("%v misread as pending", e)
		}
	}
}
