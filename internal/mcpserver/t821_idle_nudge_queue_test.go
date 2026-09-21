// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
)

// 🎯T821: no idle nudge is queued for a reaped seat (gate feedback still is,
// 🎯T401), and repeated nudges to a seat leave one pending.
func TestT821ReapedSeatReceivesNoIdleNudge(t *testing.T) {
	s, dir := t401Server(t)
	t401RegisterWork(t, s, t401Agent)
	t401ReapWithReport(t, s, dir, t401Agent, "Done. SHA abcdef1234567890. GATE t821 exit=0 GREEN id=deadbeef.")

	for _, ev := range []string{"idle-nudge", "idle-nudge-brief"} {
		res, err := s.deliverByName(t401Agent, formatIdleNudgeWire(ev, "resume work"), OriginAgent, false)
		if err != nil {
			t.Fatalf("%s: %v", ev, err)
		}
		if res.Status == StatusReapedHeld || res.Queued != 0 {
			t.Fatalf("%s to reaped seat was held: %+v", ev, res)
		}
	}
	if n := s.pendingAgentSends(t401Agent); n != 0 {
		t.Fatalf("reaped seat sendq depth after nudges = %d, want 0", n)
	}

	// T401 contract intact: non-nudge traffic is still held.
	res, err := s.deliverByName(t401Agent, "gate: master is red on your commit — fix", OriginAgent, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusReapedHeld {
		t.Fatalf("gate feedback status = %q, want %q", res.Status, StatusReapedHeld)
	}
	if n := s.pendingAgentSends(t401Agent); n != 1 {
		t.Fatalf("depth after gate feedback = %d, want 1", n)
	}
}

func TestT821NudgesToBusySeatLeaveOnePending(t *testing.T) {
	s, _ := t401Server(t)
	const name = "jv-t821-busy"
	if _, err := s.enqueueAgentSend(name, "gate: fix your commit"); err != nil {
		t.Fatal(err)
	}
	var depth int
	for i := 0; i < 10; i++ {
		var err error
		depth, err = s.enqueueAgentSend(name, "identity header\n"+formatIdleNudgeWire("idle-nudge", "nudge "+strings.Repeat("x", i)))
		if err != nil {
			t.Fatal(err)
		}
	}
	if depth != 2 {
		t.Fatalf("depth = %d, want 2 (one non-nudge + one nudge)", depth)
	}
	entries, err := s.sendQueue().Snapshot(name)
	if err != nil {
		t.Fatal(err)
	}
	nudges := 0
	for _, e := range entries {
		if IsIdleNudgeText(e.Text) {
			nudges++
			if !strings.Contains(e.Text, "nudge xxxxxxxxx") {
				t.Fatalf("held nudge is not the newest: %q", e.Text)
			}
		}
	}
	if nudges != 1 {
		t.Fatalf("pending nudges = %d, want 1", nudges)
	}
	if !strings.Contains(entries[0].Text, "gate: fix") {
		t.Fatalf("non-nudge message was disturbed: %q", entries[0].Text)
	}
}
