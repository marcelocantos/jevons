// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/hoststray"
)

// 🎯T780 — the host must not accumulate fixture daemons, journey tmux
// anchors, or detached load-generator shells after tests finish.
func TestT780HostHasNoStrayTestProcesses(t *testing.T) {
	if hoststray.HarnessBusy() {
		t.Skip("another docratchet or journey test is running — stray scan would false-positive")
	}
	strays, err := hoststray.Scan()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(strays) == 0 {
		return
	}
	var b strings.Builder
	for _, s := range strays {
		fmt.Fprintf(&b, "  pid=%d ppid=%d kind=%s args=%s\n", s.PID, s.PPID, s.Kind, s.Args)
	}
	t.Fatalf("host process table has %d stray(s) from tests or load scripts (🎯T780):\n%s", len(strays), b.String())
}
