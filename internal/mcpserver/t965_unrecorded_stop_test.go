// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T965: before the sweep records a broker stop, a seat whose handle says
// the broker restarted on purpose reads as planned (no reason), not as an
// unknown exit; a seat with neither a record nor such a cause is still
// unrecorded, and the row keeps saying so.
func TestT965UnrecordedPlannedBrokerStopIsQuiet(t *testing.T) {
	prev := stoppedSeatCause
	t.Cleanup(func() { stoppedSeatCause = prev })
	causes := map[string]string{
		"restarted": claudia.ExitCauseBrokerRestarted,
		"lost":      claudia.ExitCauseBrokerLost,
	}
	stoppedSeatCause = func(_ *Server, name string) (string, bool) {
		c, ok := causes[name]
		return c, ok
	}
	s := &Server{}
	if reason, _, ok := s.SeatStopShown("restarted"); !ok || reason != "" {
		t.Fatalf("planned, unrecorded: reason=%q ok=%v, want a quiet row", reason, ok)
	}
	if _, _, ok := s.SeatStopShown("lost"); ok {
		t.Fatal("an unannounced broker loss with no record was treated as recorded")
	}
	if _, _, ok := s.SeatStopShown("never-seen"); ok {
		t.Fatal("a seat with no handle and no record was treated as recorded")
	}
}
