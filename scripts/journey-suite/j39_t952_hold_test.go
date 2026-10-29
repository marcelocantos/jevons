// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/sendq"
)

func TestT952IsolateVerdictMatchesTheAllowance(t *testing.T) {
	stallWarn := `level=WARN msg="🎯T418 backlog stalled" component=agent_send agent=t952-stall queued=1 oldest_age=10m25s reason="it has no live process to deliver to"`
	holdWarn := `level=WARN msg="🎯T418 backlog stalled" component=agent_send agent=t952-hold queued=1 oldest_age=77h26m41s reason="stood down: parked by owner/overseer"`
	started := `level=INFO msg="agent started" name=t952-stall provider=cursor session=abc`
	overseerStarted := `level=INFO msg="agent started" name=jevons provider=cursor`
	reoffer := `level=INFO msg="🎯T418 backlog sweep: re-offering a held message" component=agent_send agent=t952-stall queued=1`
	holdReoffer := `level=INFO msg="🎯T418 backlog sweep: re-offering a held message" component=agent_send agent=t952-hold queued=1`
	// Gate 252cf6ac, byte for byte: the overseer dropped the stall on the record.
	reconciled := `time=2026-10-20T09:30:05.227+11:00 level=INFO msg=agent_send.sendq_reconcile component=agent_send source=server decision=sendq_reconcile outcome=drop removed="[map[attempt_id: bytes:39 enqueued_at:2026-10-14T05:35:01Z entry_id:3d08454cb769 state: text:t952-stall integrate the worker reports]]" removed_bytes=39 depth_after=0 agent=t952-stall actor=jevons evidence="Journey fixture t952-stall: session d6989db4 has no transcript file; seat activity none since mint; workdir empty; auto_start false; start refused (existing conversation required, no session to resume). Held text is journey residue 't952-stall integrate the worker reports' enqueued 2026-10-14T05:35:01Z. Cannot deliver; drain blocked on locked DB earlier. Drop orphan hold."`
	refusedStart := `time=2026-10-20T09:29:57.280+11:00 level=WARN msg=agent_lifecycle.start component=agent_lifecycle source=server decision=start outcome=error name=t952-stall err="broker protocol: agent_failed: existing conversation required"`

	cases := []struct {
		name string
		f    t952Facts
		log  string
		pass bool
	}{
		{name: "pending through the 15-minute band", f: t952Facts{HoldIntact: true, StallPending: true, StallNotices: 2}, pass: true},
		{name: "pending lost the 15-minute band", f: t952Facts{HoldIntact: true, StallPending: true, StallNotices: 1}, pass: false},
		{name: "pending with no alert", f: t952Facts{HoldIntact: true, StallPending: true}, pass: false},
		{name: "three notices is one per sweep", f: t952Facts{HoldIntact: true, StallPending: true, StallNotices: 3}, pass: false},
		{name: "overseer started the stall after the open notice", f: t952Facts{HoldIntact: true, StallNotices: 1, StallResolved: t952StallResolved(started + "\n" + stallWarn)}, log: started, pass: true},
		{name: "drain of a live stall after both bands", f: t952Facts{HoldIntact: true, StallNotices: 2, StallResolved: t952StallResolved(reoffer)}, pass: true},
		{name: "empty stall queue with only the warn", f: t952Facts{HoldIntact: true, StallNotices: 1, StallResolved: t952StallResolved(stallWarn + "\n" + holdWarn + "\n" + overseerStarted)}, pass: false},
		{name: "overseer reconciled the stall on the record after a refused start", f: t952Facts{HoldIntact: true, StallNotices: 1, StallResolved: t952StallResolved(refusedStart + "\n" + reconciled)}, log: refusedStart + "\n" + reconciled, pass: true},
		{name: "a refused start alone is not a resolution", f: t952Facts{HoldIntact: true, StallNotices: 1, StallResolved: t952StallResolved(refusedStart)}, log: refusedStart, pass: false},
		{name: "resolved with the alert lost", f: t952Facts{HoldIntact: true, StallResolved: true}, pass: false},
		{name: "hold was re-offered", f: t952Facts{HoldIntact: true, HoldSubmitted: t952HoldSubmitted(holdReoffer), StallPending: true, StallNotices: 2}, pass: false},
		{name: "hold named in a no-action body", f: t952Facts{HoldIntact: true, StallPending: true, StallNotices: 2, HoldNamed: 1}, pass: false},
		{name: "hold queue changed", f: t952Facts{StallPending: true, StallNotices: 2}, pass: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.log != "" && tc.f.StallResolved != t952StallResolved(tc.log) {
				t.Fatalf("StallResolved=%v from log %q", tc.f.StallResolved, tc.log)
			}
			ok, why := t952IsolateVerdict(tc.f)
			if ok != tc.pass {
				t.Fatalf("ok=%v want %v (%s)", ok, tc.pass, why)
			}
			if !tc.pass && why == "" {
				t.Fatal("a failure needs a reason")
			}
		})
	}
	if t952StallResolved(stallWarn) {
		t.Fatal("the no-process warn is not a resolution")
	}
	if t952HoldSubmitted(holdWarn) {
		t.Fatal("the parked warn is not a submission")
	}
	if !t952PendingText([]sendq.Entry{{Text: t952StallText, State: sendq.Pending}}, t952StallText) {
		t.Fatal("original pending text must count as intact")
	}
	if !stallMutated([]sendq.Entry{{Text: "other", State: sendq.Pending}}) {
		t.Fatal("a different pending text is a mutated stall")
	}
	if stallMutated(nil) {
		t.Fatal("an empty stall queue is resolved or silent, not mutated")
	}
}

func TestT952DiagCountsDrainAndBodies(t *testing.T) {
	bodies := []string{
		`{"type":"assistant","text":"no action needed Stalled backlog on \"t952-stall\""}`,
		`{"type":"agent_note","text":"Stalled backlog on \"t952-stall\": 1 message"}`,
	}
	logText := strings.Join([]string{
		`msg=notify_queue decision=enqueue`,
		`msg=notify_queue decision=defer err_class=busy`,
		`msg=notify_queue decision=drain drained=1`,
		`status=delivered_unconfirmed`,
		`msg=fleet_health.notice_coalesced saved=2`,
		`msg="cockpit: stuck-busy detected"`,
	}, "\n")
	got := t952Diag(bodies, logText)
	for _, want := range []string{
		"bodies=2",
		"stalled=1",
		"noAction=1",
		"fleetish=2",
		"named=2",
		"logDrain=1",
		"logDefer=1",
		"logEnqueue=1",
		"logUnconfirmed=1",
		"logCoalesced=1",
		"logStuck=1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("diag missing %s in %s", want, got)
		}
	}
}

// Gate c3afc8fc, row jevons/430 byte for byte: the stall's notice carried a
// concurrent tool call's input naming the hold. The notice's words do not.
func TestT952HoldCountReadsTheNoticeWordsNotFoldedToolFields(t *testing.T) {
	folded := `{"input":{"name":"t952-hold"},"name":"jevons_sendq_reconcile","text":"[Fleet health] Stalled backlog on \"t952-stall\": 1 message(s) held by the daemon, the oldest waiting 10m25s, because it has no live process to deliver to. They are on disk and will be delivered when it next takes a turn. If that agent should not still be busy, confirm from ITS transcript (terminal assistant message + turn_duration, file not growing) and then jevons_agent_send with interrupt=true. (occurrence 8772ea0f5259)","timestamp":"2026-10-20T01:26:56.5688Z","type":"agent_note"}`
	stalled, holdNamed := t952NoticeCounts([]string{folded})
	if stalled != 1 || holdNamed != 0 {
		t.Fatalf("stalled=%d holdNamed=%d, want the stall notice counted and the hold not named", stalled, holdNamed)
	}
	named := `{"text":"[Fleet health] Stalled backlog on \"t952-hold\": 1 message(s) held","type":"agent_note"}`
	if _, holdNamed := t952NoticeCounts([]string{named}); holdNamed != 1 {
		t.Fatalf("a notice whose words name the hold was not counted: %d", holdNamed)
	}
}

// 🎯T1187 changed the parked hold from a WARN per sweep to one WARN per
// daemon lifetime. The isolate still requires a status line, not silence.
func TestT952HoldWarnAllowance(t *testing.T) {
	dir := t.TempDir()
	s := &suite{logPath: filepath.Join(dir, "jevonsd.log")}
	first := `level=WARN msg="🎯T418 backlog stalled" component=agent_send agent=t952-hold queued=1 oldest_age=77h6m41s reason="stood down: parked by owner"`
	debug := `level=DEBUG msg="🎯T1187 stood-down backlog stall already warned" agent=t952-hold oldest_age=77h7m41s`
	if err := os.WriteFile(s.logPath, []byte(first+"\n"+debug+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.checkT952HoldWarns(1); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.logPath, []byte(first+"\n"+debug+"\n"+first+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.checkT952HoldWarns(1); err == nil {
		t.Fatal("repeated WARN in one lifetime accepted")
	}
	if err := s.checkT952HoldWarns(2); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.logPath, []byte(debug+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.checkT952HoldWarns(2); err == nil {
		t.Fatal("missing first WARN accepted")
	}
}
