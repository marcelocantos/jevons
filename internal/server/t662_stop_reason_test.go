// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/seatstop"
)

func TestDecorateSeatStopsNamesAnUnrecordedStop(t *testing.T) {
	s := &Server{}
	when := time.Date(2026, 9, 23, 12, 43, 48, 0, time.UTC)
	s.SetSeatStopReader(func(name string) (string, time.Time, bool) {
		if name == "yourworld2-po" {
			return "unknown: process exited and no reason was recorded", when, true
		}
		return "", time.Time{}, false
	})
	rows := s.decorateSeatStops([]agentInfo{
		{Name: "yourworld2-po", Running: false},
		{Name: "ge-po", Running: false},
		{Name: "jevons", Running: true},
	})
	if rows[0].StopReason != seatstop.Unknown || rows[0].StoppedAt == "" {
		t.Fatalf("recorded stop = %#v", rows[0])
	}
	if rows[1].StopReason != seatstop.Unknown {
		t.Fatalf("unrecorded stop reason = %q, want %q", rows[1].StopReason, seatstop.Unknown)
	}
	if rows[1].StoppedAt != "" {
		t.Fatalf("unrecorded stop invented a time %q", rows[1].StoppedAt)
	}
	if rows[2].StopReason != "" {
		t.Fatalf("running seat reason = %q", rows[2].StopReason)
	}
}
