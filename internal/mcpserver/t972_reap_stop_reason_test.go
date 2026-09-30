// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
)

// 🎯T972: every accounted removal used to stamp fleetintent.Reaped alike, so
// jevons_agent_list's "finished and reaped" line read the same for a
// genuine 🎯T165 finish and for an explicit jevons_agent_kill — a reader
// could not tell a worker that said it was done from one that was killed
// mid-mission without parsing free-text prose. onAccountedRemoval now
// carries the closed-vocabulary fleetlog.Reason through as stop_reason, so
// the two are distinguishable by field, not by prose-sniffing.
func TestT972StopReasonDistinguishesFinishedWorkFromKill(t *testing.T) {
	s, _ := t401Server(t)

	s.onAccountedRemoval("jv-finished", fleetlog.Removal{
		Reason: fleetlog.ReasonReapDone,
		Detail: "reaped on a finished-work report (finished_work)",
	})
	s.onAccountedRemoval("jv-killed", fleetlog.Removal{
		Reason: fleetlog.ReasonKill,
		Detail: "killed by explicit request",
	})
	s.onAccountedRemoval("jv-dead", fleetlog.Removal{
		Reason: fleetlog.ReasonDeadSeat,
		Detail: "silent-death sweep found the process gone",
	})

	snap := s.fleetIntent()

	finished, ok := snap.Agents["jv-finished"]
	if !ok {
		t.Fatal("jv-finished has no recorded intent")
	}
	if finished.StopReason != fleetlog.ReasonReapDone {
		t.Fatalf("jv-finished stop_reason = %q, want %q", finished.StopReason, fleetlog.ReasonReapDone)
	}
	if !fleetintent.IsFinishedWorkStopReason(finished.StopReason) {
		t.Fatalf("IsFinishedWorkStopReason(%q) = false, want true", finished.StopReason)
	}

	killed, ok := snap.Agents["jv-killed"]
	if !ok {
		t.Fatal("jv-killed has no recorded intent")
	}
	if killed.StopReason != fleetlog.ReasonKill {
		t.Fatalf("jv-killed stop_reason = %q, want %q", killed.StopReason, fleetlog.ReasonKill)
	}
	if fleetintent.IsFinishedWorkStopReason(killed.StopReason) {
		t.Fatal("a kill must not read as a finished-work stop_reason")
	}

	dead, ok := snap.Agents["jv-dead"]
	if !ok {
		t.Fatal("jv-dead has no recorded intent")
	}
	if dead.StopReason != fleetlog.ReasonDeadSeat {
		t.Fatalf("jv-dead stop_reason = %q, want %q", dead.StopReason, fleetlog.ReasonDeadSeat)
	}
	if fleetintent.IsFinishedWorkStopReason(dead.StopReason) {
		t.Fatal("a dead-seat sweep removal must not read as a finished-work stop_reason")
	}

	// The rendered line still says "finished and reaped" (State-level
	// wording, unchanged for compatibility with existing 🎯T401 callers)
	// but now also carries the machine-readable field.
	if !strings.Contains(killed.Describe(), "stop_reason=kill") {
		t.Fatalf("killed.Describe() = %q, want it to name stop_reason=kill", killed.Describe())
	}
	if !strings.Contains(finished.Describe(), "stop_reason=reap_done") {
		t.Fatalf("finished.Describe() = %q, want it to name stop_reason=reap_done", finished.Describe())
	}

	// FormatReapedListSection (the jevons_agent_list surface) must carry the
	// same distinguishing field through to the tool's text output.
	section := FormatReapedListSection(snap)
	for _, want := range []string{"stop_reason=reap_done", "stop_reason=kill", "stop_reason=dead_seat"} {
		if !strings.Contains(section, want) {
			t.Fatalf("FormatReapedListSection missing %q:\n%s", want, section)
		}
	}
}
