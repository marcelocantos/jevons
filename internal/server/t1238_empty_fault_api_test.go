// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import "testing"

func TestT1238FaultAndBackoffOnCockpitRows(t *testing.T) {
	s := &Server{}
	s.SetEmptyTurnFaultReaders(func(name string) (string, bool) {
		if name == "fault" {
			return "FAULTED fault: two swallowed deliveries", true
		}
		return "", false
	}, func(name string) string {
		if name == "backoff" {
			return "repressure refused: backoff after terminal_empty"
		}
		return ""
	})
	rows := s.decorateSeatStops([]agentInfo{{Name: "fault", Running: true, Status: "idle"}, {Name: "backoff", Running: true, Status: "idle"}, {Name: "normal", Running: true, Status: "idle"}})
	if rows[0].Status != "faulted" || rows[0].PanelLine == "" {
		t.Fatalf("fault row: %+v", rows[0])
	}
	if rows[1].Status != "idle" || rows[1].PanelLine != "repressure refused: backoff after terminal_empty" {
		t.Fatalf("backoff row: %+v", rows[1])
	}
	if rows[2].Status != "idle" || rows[2].PanelLine != "" {
		t.Fatalf("normal row: %+v", rows[2])
	}
}
