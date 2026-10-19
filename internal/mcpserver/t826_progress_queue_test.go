// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T826: enqueueAgentSend collapses non-terminal worker progress per sender.
func TestT826EnqueueCollapsesProgressPerWorker(t *testing.T) {
	s, _ := t401Server(t)
	const po, worker = "jevons-po", "jv-t826-auto"
	for i := 0; i < 6; i++ {
		text := fmt.Sprintf("[Agent %s responded] report_id=r%d\n%s still running, waiting on the monitor (round %d)",
			worker, i, worker, i)
		depth, err := s.enqueueAgentSend(po, text)
		if err != nil {
			t.Fatal(err)
		}
		if depth != 1 {
			t.Fatalf("after progress %d: depth=%d want 1", i, depth)
		}
	}
	// A free-form supervisor note must not be disturbed.
	if _, err := s.enqueueAgentSend(po, "SUPERVISOR: check the frontier"); err != nil {
		t.Fatal(err)
	}
	// A GATE-carrying mid-work report is kept beside the newest progress.
	gateText := fmt.Sprintf("[Agent %s responded] report_id=g1\nGATE make-test-go exit=0 GREEN id=9f13c0a2 out=abc dur=1s\nstill mid-work", worker)
	depth, err := s.enqueueAgentSend(po, gateText)
	if err != nil {
		t.Fatal(err)
	}
	if depth != 3 {
		t.Fatalf("depth after gate=%d want 3 (progress+supervisor+gate)", depth)
	}
	// More progress collapses only the prior progress entry.
	depth, err = s.enqueueAgentSend(po, fmt.Sprintf("[Agent %s responded] report_id=r9\n%s still running, waiting on the monitor (round 9)", worker, worker))
	if err != nil {
		t.Fatal(err)
	}
	if depth != 3 {
		t.Fatalf("depth after newer progress=%d want 3", depth)
	}

	entries, err := s.sendQueue().Snapshot(po)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries=%d want 3", len(entries))
	}
	var progress, gate, free int
	for _, e := range entries {
		switch {
		case sendq.IsSupersedableProgressReport(e.Text):
			progress++
			if !strings.Contains(e.Text, "round 9") {
				t.Fatalf("progress is not newest: %q", e.Text)
			}
		case strings.Contains(e.Text, "GATE make-test-go"):
			gate++
		case strings.Contains(e.Text, "SUPERVISOR:"):
			free++
		default:
			t.Fatalf("unexpected entry: %q", e.Text)
		}
	}
	if progress != 1 || gate != 1 || free != 1 {
		t.Fatalf("progress=%d gate=%d free=%d", progress, gate, free)
	}
}
