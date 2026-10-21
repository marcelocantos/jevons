// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleetpanel

import (
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

func TestKillStopReasonIsHistory(t *testing.T) {
	got := Classify(Input{StopReason: fleetlog.ReasonKill})
	if got.Visible || got.Class != ClassHistory {
		t.Fatalf("kill row = %+v, want history hidden", got)
	}
}
