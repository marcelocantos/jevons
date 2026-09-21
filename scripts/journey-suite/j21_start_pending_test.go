// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"testing"
)

// 🎯T625.8: the T792 pending result is "in flight"; a genuine start failure
// (control) is not.
func TestIsStartPending(t *testing.T) {
	pending := errors.New("jevons_agent_start: start of \"x\" is still running past this call's deadline and CONTINUES in the daemon (🎯T792): the launch")
	if !isStartPending(pending) {
		t.Fatal("T792 pending text not recognised")
	}
	for _, e := range []error{nil, errors.New("jevons_agent_start: provider unavailable")} {
		if isStartPending(e) {
			t.Fatalf("%v misread as pending", e)
		}
	}
}
