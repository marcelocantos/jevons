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

// A reason string is not an actor oracle: plan policy stops have a separate
// trusted Actor field with no "by X:" syntax in Reason (🎯T1059).
func TestDecorateSeatStopsPreservesTrustedActor(t *testing.T) {
	s := &Server{}
	when := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	s.SetSeatStopDetailsReader(func(name string) (string, string, time.Time, bool) {
		if name == "plan-worker" {
			return "plan policy parked: no eligible destination", "product:plan_policy", when, true
		}
		return "", "", time.Time{}, false
	})
	rows := s.decorateSeatStops([]agentInfo{{Name: "plan-worker", Running: false}, {Name: "unknown", Running: false}})
	if rows[0].StopReason != "plan policy parked: no eligible destination" || rows[0].StopActor != "product:plan_policy" || rows[0].StoppedAt != when.Format(time.RFC3339) {
		t.Fatalf("plan policy row = %+v", rows[0])
	}
	if rows[1].StopActor != "" || rows[1].StoppedAt != "" {
		t.Fatalf("invented attribution: %+v", rows[1])
	}
}
