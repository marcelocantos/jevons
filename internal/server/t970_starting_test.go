// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/seatstop"
)

// 🎯T970: /api/agents says a seat being brought up is starting, with no stop
// reason. Unknown stays the answer only for a seat nothing is starting.
func TestT970DecorateSeatStopsSaysStarting(t *testing.T) {
	s := &Server{}
	s.SetSeatStopReader(func(string) (string, time.Time, bool) { return "", time.Time{}, false })
	s.SetSeatStartingReader(func(name string) bool { return name == "jv-t947-plan-token" || name == "jevons" })
	rows := s.decorateSeatStops([]agentInfo{
		{Name: "jv-t947-plan-token", Running: false},
		{Name: "jv-gone", Running: false},
		{Name: "jevons", Running: true},
	})
	if !rows[0].Starting || rows[0].StopReason != "" {
		t.Fatalf("starting row = %#v, want starting with no stop reason", rows[0])
	}
	if rows[1].Starting || rows[1].StopReason != seatstop.Unknown {
		t.Fatalf("row nothing is starting = %#v, want %q", rows[1], seatstop.Unknown)
	}
	if rows[2].Starting || rows[2].StopReason != "" {
		t.Fatalf("running row = %#v, want neither", rows[2])
	}
}
