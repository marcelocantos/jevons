// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package staffops

import (
	"testing"
	"time"
)

// The two rows the 2026-08-09 RSI ops live drill appended to the daily
// eventlog (🎯T352). Sentinel classified event:error:rsi_drill → file+PO;
// they are synthetic coach stimulus and must be ignored.
func drillRows(ts time.Time) []EventRow {
	rows := make([]EventRow, 0, 2)
	for i := 0; i < 2; i++ {
		rows = append(rows, EventRow{
			Source:    "rsi-drill",
			Level:     "error",
			Msg:       "rsi_ops_live_drill: synthetic coach stimulus (safe to ignore)",
			Component: "rsi_drill",
			Decision:  "live_drill",
			Drill:     true,
			TS:        ts,
		})
	}
	return rows
}

func TestIsSyntheticDrillRow(t *testing.T) {
	cases := []struct {
		name string
		row  EventRow
		want bool
	}{
		{"source marker", EventRow{Source: "rsi-drill", Level: "error", Msg: "boom"}, true},
		{"component marker", EventRow{Component: "rsi_drill", Level: "error", Msg: "boom"}, true},
		{"msg marker", EventRow{Component: "server", Level: "error",
			Msg: "rsi_ops_live_drill: synthetic coach stimulus"}, true},
		{"fields drill marker", EventRow{Component: "server", Level: "error", Drill: true}, true},
		{"decision from drill source", EventRow{Source: "ops-drill", Decision: "live_drill",
			Level: "error", Msg: "boom"}, true},

		{"real server error", EventRow{Component: "server", Level: "error", Msg: "panic recover"}, false},
		{"real rsi coach error", EventRow{Source: "server", Component: "rsi_coach",
			Level: "error", Msg: "coach cycle failed"}, false},
		{"real rsi component error", EventRow{Source: "server", Component: "rsi",
			Level: "error", Msg: "mint failed"}, false},
		{"decision without drill origin", EventRow{Source: "server", Component: "rsi_coach",
			Decision: "live_drill", Level: "error", Msg: "coach cycle failed"}, false},
		{"empty row", EventRow{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSyntheticDrillRow(tc.row); got != tc.want {
				t.Fatalf("IsSyntheticDrillRow(%+v)=%v want %v", tc.row, got, tc.want)
			}
		})
	}
}

// Drill rows alone must produce no anomaly at all — no daemon_error cluster.
func TestClusterEventAnomaliesIgnoresDrillRows(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	obs := ClusterEventAnomalies(drillRows(now), now, 15*time.Minute)
	if len(obs) != 0 {
		t.Fatalf("drill rows produced anomalies: %+v", obs)
	}
}

// Real error rows still cluster exactly as before when drill rows sit beside
// them, and the drill component never becomes its own daemon_error symptom.
func TestClusterEventAnomaliesRealErrorsUnchanged(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	real := []EventRow{
		{Source: "server", Level: "error", Component: "server", Msg: "panic recover", TS: now},
		{Source: "server", Level: "error", Component: "server", Msg: "panic recover 2", TS: now},
	}
	want := ClusterEventAnomalies(real, now, 15*time.Minute)
	if len(want) != 1 || want[0].Kind != "daemon_error" || want[0].Count != 2 {
		t.Fatalf("baseline real-error cluster wrong: %+v", want)
	}

	mixed := append(append([]EventRow(nil), real...), drillRows(now)...)
	got := ClusterEventAnomalies(mixed, now, 15*time.Minute)
	if len(got) != len(want) {
		t.Fatalf("drill rows changed real clustering: got=%+v want=%+v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("obs[%d]=%+v want %+v", i, got[i], want[i])
		}
	}
	for _, o := range got {
		if o.Symptom == "event:error:rsi_drill" {
			t.Fatalf("drill symptom survived clustering: %+v", o)
		}
	}
}

// End-to-end pure path: drill rows never reach file+PO; real errors still do.
func TestDrillRowsDoNotFilePO(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	drillOnly := RunCycle(CycleArgs{
		Signals: BuildSignals(ObserveInput{
			OverseerAlive: true,
			Events:        ClusterEventAnomalies(drillRows(now), now, 15*time.Minute),
		}),
		Now:      now,
		Sentinel: true,
	})
	if drillOnly.Primary == ActionFilePO {
		t.Fatalf("drill-only cycle filed to PO: %s\n%s", drillOnly.Primary, drillOnly.WireText)
	}
	if len(drillOnly.FiledSymptoms) != 0 {
		t.Fatalf("drill-only cycle filed symptoms: %v", drillOnly.FiledSymptoms)
	}

	realErrors := []EventRow{
		{Source: "server", Level: "error", Component: "server", Msg: "panic recover", TS: now},
		{Source: "server", Level: "error", Component: "server", Msg: "panic recover 2", TS: now},
	}
	withReal := RunCycle(CycleArgs{
		Signals: BuildSignals(ObserveInput{
			OverseerAlive: true,
			Events: ClusterEventAnomalies(
				append(append([]EventRow(nil), realErrors...), drillRows(now)...),
				now, 15*time.Minute),
		}),
		Now:      now,
		Sentinel: true,
	})
	if withReal.Primary != ActionFilePO {
		t.Fatalf("real error no longer files: %s\n%s", withReal.Primary, withReal.WireText)
	}
	for _, sym := range withReal.FiledSymptoms {
		if sym == "event:error:rsi_drill" {
			t.Fatalf("drill symptom filed to PO: %v", withReal.FiledSymptoms)
		}
	}
}
