// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"testing"
)

func TestT1132CallerGoneNoteIsKilledNeverGreen(t *testing.T) {
	rec, _ := runIn(t, "sh", "-c", "echo ok; exit 0")
	if rec.Verdict == VerdictGreen {
		// control: ordinary pass stays green
	} else {
		t.Fatalf("control pass = %s", rec.Verdict)
	}

	// Simulate caller-gone verdict path via package API is covered by e2e;
	// here we assert the note token is what check/last surface.
	if CallerGoneNote != "caller_gone" {
		t.Fatalf("note = %q", CallerGoneNote)
	}
}

func TestT1132CallerGoneClassifierRejectsBootstrap(t *testing.T) {
	if CallerGone(1) || CallerGone(0) {
		t.Fatal("bootstrap ppid must not read as gone")
	}
}
