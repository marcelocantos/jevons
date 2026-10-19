// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T766.4.1: the typed reading of the 🎯T1024 specimens. The protective
// reap/park/idle assertions over the same specimens live in
// t1024_active_wait_test.go.

// TestT1024RawNarrationIsNotAWaitSlot: the incident sentences are not a
// declared wait. Only the slot is.
func TestT1024RawNarrationIsNotAWaitSlot(t *testing.T) {
	for _, sp := range t1024Specimens(t) {
		if w, ok := declaredActiveWait(sp.raw); ok {
			t.Fatalf("%s: monitoring prose was read as an active-wait slot: %+v", sp.name, w)
		}
	}
}

// TestT1024SpecimensDeclareTheWait pins the re-encodings: the slot parses
// into the closed vocabulary, the payload is the raw report unchanged, and
// the finish encoding is contradictory (never Done).
func TestT1024SpecimensDeclareTheWait(t *testing.T) {
	for _, sp := range t1024Specimens(t) {
		t.Run(sp.name, func(t *testing.T) {
			for _, enc := range []struct{ name, text string }{{"finish", sp.finish}, {"status-ping", sp.ping}} {
				m, _ := envelope.Parse(enc.text)
				if m == nil {
					t.Fatalf("%s: no envelope", enc.name)
				}
				if strings.TrimSpace(m.Payload) != strings.TrimSpace(sp.raw) {
					t.Fatalf("%s: payload is not the raw specimen", enc.name)
				}
				w, ok := declaredActiveWait(enc.text)
				if !ok || string(w.Class) != sp.wantClass || !strings.Contains(w.On, sp.wantOnPart) {
					t.Fatalf("%s: active wait = %+v %v, want %s …%s", enc.name, w, ok, sp.wantClass, sp.wantOnPart)
				}
				if !DeclaresBlockingGateWait(enc.text) {
					t.Fatalf("%s: idle-nudge does not hear the declared wait", enc.name)
				}
			}
			if got := envelope.ClassifyDone(sp.finish); got != envelope.DoneUnknown {
				t.Fatalf("finish encoding ClassifyDone = %s, want unknown (contradictory)", got)
			}
			if got := envelope.ClassifyDone(sp.ping); got != envelope.DoneNo {
				t.Fatalf("status-ping encoding ClassifyDone = %s, want no", got)
			}
		})
	}
}

// TestT1024WaitSlotInPayloadIsNotTheWorkersWait: 🎯T750's quotation rule,
// typed. A slot line quoted in the payload — a worker pasting a sibling's
// envelope — is not the author's fence, and does not keep the author.
func TestT1024WaitSlotInPayloadIsNotTheWorkersWait(t *testing.T) {
	t.Parallel()
	report := declaredFinishReport("T1024", "The PO forwarded a sibling's ping:\n\n```jevons\njevons: kind status-ping\njevons: status in-progress\njevons: active-wait lease shared heavy gate lease\n```\n\nThat was its seat, not mine.")
	if _, ok := declaredActiveWait(report); ok {
		t.Fatal("a quoted slot was read as the author's wait")
	}
	reg := t439Registry(t, "jv-t1024-quoted")
	if ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t1024-quoted", report, nil); !ok {
		t.Fatalf("a finish quoting a sibling's wait did not reap (reason %s)", reason)
	}
}
