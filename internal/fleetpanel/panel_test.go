// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleetpanel

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
)

func TestT1121FixtureRendersFiveVisibleRows(t *testing.T) {
	rows := []Input{
		{Running: true},
		{Running: true},
		{Running: true},
		{Intent: fleetintent.BlockedOwner, Record: fleetintent.Record{Reason: "waiting on owner merge approval"}},
		{AutoStart: true, Intent: fleetintent.Working, StoppedAt: "2026-10-21T12:00:00Z"},
	}
	for i := 0; i < 20; i++ {
		rows = append(rows, Input{
			Intent: fleetintent.Reaped,
			Record: fleetintent.Record{StopReason: fleetlog.ReasonKill},
		})
	}
	visible := 0
	for _, in := range rows {
		if Classify(in).Visible {
			visible++
		}
	}
	if visible != 5 {
		t.Fatalf("visible rows = %d, want 5 (3 running + 1 park + 1 fault)", visible)
	}
}

func TestParkedButRunningIsFault(t *testing.T) {
	got := Classify(Input{Running: true, Intent: fleetintent.Parked})
	if got.Class != ClassFault || !got.Visible {
		t.Fatalf("parked+running = %+v, want visible fault", got)
	}
}

func TestServedListStatusFlagsLiveIntentFault(t *testing.T) {
	if got := ServedListStatus(true, false, fleetintent.Parked, "running"); got != StatusRunningAgainstIntent {
		t.Fatalf("live parked = %q, want %q", got, StatusRunningAgainstIntent)
	}
	if got := ServedListStatus(false, false, fleetintent.Parked, "stopped"); got != "stopped" {
		t.Fatalf("parked down keeps observed status: %q", got)
	}
	if got := ServedListStatus(false, false, fleetintent.Working, "stopped"); got != "stopped" {
		t.Fatalf("dead durable fault keeps observed status: %q", got)
	}
}

func TestKillStopReasonIsHistory(t *testing.T) {
	got := Classify(Input{StopReason: fleetlog.ReasonKill})
	if got.Visible || got.Class != ClassHistory {
		t.Fatalf("kill row = %+v, want history hidden", got)
	}
}

// An unobserved seat with runnable intent stays on the panel: not knowing is
// not a stop (the minute after a restart hid every live worker).
func TestUnobservedRunnableSeatStaysVisible(t *testing.T) {
	row := Classify(Input{Unobserved: true, Intent: fleetintent.Working})
	if !row.Visible || row.Class != ClassLive {
		t.Fatalf("unobserved working seat = %+v, want visible live", row)
	}
	if row := Classify(Input{Unobserved: true, Intent: fleetintent.Reaped}); row.Visible {
		t.Fatalf("unobserved reaped seat = %+v, want hidden history", row)
	}
	if row := Classify(Input{Unobserved: true, Intent: fleetintent.Working, StopReason: "kill"}); row.Visible {
		t.Fatalf("unobserved killed seat = %+v, want hidden history", row)
	}
}

func TestT1263IntentReapedQueueDoesNotProduceFault(t *testing.T) {
	held := Classify(Input{AutoStart: true, Intent: fleetintent.Reaped, AutoHold: "frontier_consume_disabled", StoppedAt: "2026-10-21T12:00:00Z"})
	if held.Visible || held.Class != ClassHistory || strings.Contains(held.Line, "should be running") {
		t.Fatalf("agent_intent_reaped held queue = %+v", held)
	}
	if got := Classify(Input{AutoStart: true, Intent: fleetintent.Working, AutoHold: "frontier_consume_disabled", Unobserved: true}); got.Class == ClassFault || got.Visible {
		t.Fatalf("paused frontier auto seat = %+v", got)
	}
	if got := Classify(Input{AutoStart: true, Intent: fleetintent.Working, StoppedAt: "2026-10-21T12:00:00Z"}); got.Class != ClassFault {
		t.Fatalf("deliverable queue = %+v", got)
	}
}
