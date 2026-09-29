// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatstop

import (
	"testing"
	"time"
)

// 🎯T944: the owner is drawn in only when something has gone unexpectedly
// off script. A seat running again needs no one; a seat stopped on purpose
// is expected back, and only counts once its grace has passed without it.
func TestT944OnlyUnexpectedUnresolvedStopsWarrantAttention(t *testing.T) {
	now := time.Date(2026, 9, 30, 8, 20, 0, 0, time.UTC)
	running := map[string]bool{"back": true, "planned-back": true}
	recs := []Record{
		{Seat: "back", At: now.Add(-time.Minute), Source: SourceBroker},                                        // unplanned, recovered
		{Seat: "down", At: now.Add(-time.Minute), Source: SourceBroker},                                        // unplanned, still down
		{Seat: "planned-back", At: now.Add(-time.Minute), Source: SourceBroker, Planned: true},                 // planned, recovered
		{Seat: "planned-recovering", At: now.Add(-time.Minute), Source: SourceBroker, Planned: true},           // planned, inside grace
		{Seat: "planned-stuck", At: now.Add(-PlannedGrace - time.Second), Source: SourceBroker, Planned: true}, // planned, past grace
	}
	got := map[string]bool{}
	for _, r := range Unresolved(recs, now, func(s string) bool { return running[s] }) {
		got[r.Seat] = true
	}
	if len(got) != 2 || !got["down"] || !got["planned-stuck"] {
		t.Fatalf("unresolved = %v, want only the unplanned seat still down and the planned seat past its grace", got)
	}
}

// 🎯T944: a planned broker restart's reason says so, not "connection closed".
func TestT944PlannedBrokerStopReadsAsPlanned(t *testing.T) {
	if got := BrokerReason("claudia broker stopped on purpose (planned restart)", true); got != "broker: planned Claudia broker restart" {
		t.Fatalf("planned reason = %q", got)
	}
}
