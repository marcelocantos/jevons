// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
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
