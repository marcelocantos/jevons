// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatstop

import (
	"strings"
	"testing"
	"time"
)

// 🎯T925: a burst of broker stops is a mass stop even when jevonsd booted
// inside the window, and the alert names the broker, not "no daemon
// restart". An ordinary burst beside a boot stays the restart path's story.
func TestT925BrokerBurstAlertsDespiteADaemonBoot(t *testing.T) {
	at := time.Date(2026, 9, 29, 20, 34, 21, 0, time.UTC)
	boot := at.Add(-20 * time.Second)
	burst := func(source Source, reason string) []Record {
		var rs []Record
		for i, seat := range []string{"a", "b", "c"} {
			rs = append(rs, Record{Seat: seat, At: at.Add(time.Duration(i) * time.Second), Source: source, Reason: reason})
		}
		return rs
	}
	reason := BrokerReason("claudia broker connection closed", false)
	a, ok := MassStop(burst(SourceBroker, reason), DefaultWindow, DefaultMinSeats, boot)
	if !ok || !a.Broker || a.Shared != reason {
		t.Fatalf("broker burst: ok=%v alert=%+v", ok, a)
	}
	line := FormatAlert(a)
	if !strings.Contains(line, "when the Claudia broker stopped or restarted") || strings.Contains(line, "no daemon restart") {
		t.Fatalf("alert = %q", line)
	}
	if _, ok := MassStop(burst(SourceExit, Unknown), DefaultWindow, DefaultMinSeats, boot); ok {
		t.Fatal("an unexplained burst beside a daemon boot was reported as a mass stop")
	}
}
