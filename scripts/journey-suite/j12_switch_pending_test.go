// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"testing"
)

// 🎯T833 / 🎯T625.8: the T790 pending result is "in flight"; a genuine switch
// failure (control) is not.
func TestIsSwitchPending(t *testing.T) {
	pending := errors.New(`jevons_agent_switch_model: switch of "orch-switch-1" → claude is still running past this call's deadline and CONTINUES in the daemon (🎯T790): it is not cancelled`)
	if !isSwitchPending(pending) {
		t.Fatal("T790 pending text not recognised")
	}
	for _, e := range []error{
		nil,
		errors.New(`jevons_agent_start: start of "x" is still running past this call's deadline and CONTINUES in the daemon (🎯T792)`),
		errors.New("jevons_agent_switch_model: switch refused: the conversation does not fit"),
	} {
		if isSwitchPending(e) {
			t.Fatalf("%v misread as pending", e)
		}
	}
}
