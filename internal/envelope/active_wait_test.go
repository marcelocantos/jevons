// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"strings"
	"testing"
)

// 🎯T766.4.1: the 🎯T1024 active wait is a declared slot, not a sentence.

func TestT76641ActiveWaitSlotParsesAndRoundTrips(t *testing.T) {
	ping := Format(&Message{
		Kind:   KindStatusPing,
		Target: "T41",
		Status: ProgressInProgress,
		ActiveWaits: []ActiveWait{
			{Class: WaitGate, On: "t41-slowpath on 9890903 (queued fifth on the shared heavy lease)"},
			{Class: WaitProcess, On: "monitor watching the result file"},
		},
		Payload: "No change.",
	})
	if !strings.Contains(ping, "jevons: active-wait gate t41-slowpath on 9890903 (queued fifth on the shared heavy lease)\n") {
		t.Fatalf("Format did not write the slot verbatim:\n%s", ping)
	}
	m, err := Parse(ping)
	if err != nil || m == nil {
		t.Fatalf("Parse: m=%v err=%v", m, err)
	}
	if len(m.ActiveWaits) != 2 || m.ActiveWaits[0].Class != WaitGate || m.ActiveWaits[1].Class != WaitProcess {
		t.Fatalf("ActiveWaits = %+v", m.ActiveWaits)
	}
	if got := m.ActiveWaits[0].On; got != "t41-slowpath on 9890903 (queued fifth on the shared heavy lease)" {
		t.Fatalf("On = %q", got)
	}
	if w, ok := ActiveWaitIn(ping); !ok || w.Class != WaitGate {
		t.Fatalf("ActiveWaitIn = %+v %v", w, ok)
	}
	if !strings.Contains(m.SlotsFingerprint(), "active-wait=") {
		t.Fatal("fingerprint ignores the slot; two different waits would read as chatter")
	}
	if ClassifyDone(ping) != DoneNo {
		t.Fatalf("status-ping with active-wait = %s, want no", ClassifyDone(ping))
	}
}

func TestT76641ActiveWaitSlotIsClosedVocabulary(t *testing.T) {
	for _, tc := range []struct{ name, slot, wantErr string }{
		{"unknown class", "jevons: active-wait vibes the queue", "unknown active-wait class"},
		{"missing object", "jevons: active-wait lease", "requires what it waits on"},
		{"empty", "jevons: active-wait", "unknown active-wait class"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := "```jevons\njevons: kind status-ping\njevons: status in-progress\n" + tc.slot + "\n```\n"
			_, err := Parse(text)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}
	for _, c := range AllWaitClasses() {
		if got, ok := ParseWaitClass(strings.ToUpper(string(c))); !ok || got != c {
			t.Fatalf("ParseWaitClass(%q) = %q %v", c, got, ok)
		}
	}
}

// A finish-report that declares an active wait is contradictory: it is
// never Done, whatever oracle or status it also carries, and its author is
// told why.
func TestT76641FinishReportWithActiveWaitIsUnknown(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  Message
	}{
		{"oracle finish", Message{Kind: KindFinishReport, Target: "T41", SHA: "9890903", Verdict: VerdictGreen, SilentLedger: SilentLedgerEmpty}},
		{"risk finish", Message{Kind: KindFinishReport, Target: "T41", Risk: RiskAccepted, SilentLedger: SilentLedgerEmpty}},
		{"in-progress finish", Message{Kind: KindFinishReport, Target: "T41", Status: ProgressInProgress, SilentLedger: SilentLedgerEmpty}},
		{"blocked finish", Message{Kind: KindFinishReport, Target: "T41", Status: ProgressBlocked, Blocker: "owner", SilentLedger: SilentLedgerEmpty}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.msg
			msg.ActiveWaits = []ActiveWait{{Class: WaitLease, On: "shared heavy gate lease"}}
			msg.Payload = "The landing is done and verified."
			text := Format(&msg)
			if got := ClassifyDone(text); got != DoneUnknown {
				t.Fatalf("ClassifyDone = %s, want unknown", got)
			}
			if IsDoneReport(text) {
				t.Fatal("contradictory finish-report read as done")
			}
			m, err := Parse(text)
			if m == nil || err == nil || !strings.Contains(err.Error(), "must not carry active-wait") {
				t.Fatalf("Parse err = %v, want the contradiction named", err)
			}
			if w, ok := ActiveWaitIn(text); !ok || w.Class != WaitLease {
				t.Fatalf("validation error hid the declared wait: %+v %v", w, ok)
			}
			if n := CorrectionNotice(text); !strings.Contains(n, "must not carry active-wait") {
				t.Fatalf("author is not told: %q", n)
			}
		})
	}
}
