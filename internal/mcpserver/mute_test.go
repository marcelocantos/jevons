// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "testing"

func TestT418MuteWhenEveryAgentIsStuckWithQueuedWork(t *testing.T) {
	mute, reason := ClassifyFleetMute(3, 0, 2)
	if !mute {
		t.Fatalf("expected mute, got %q", reason)
	}
	if reason == "" {
		t.Fatal("mute must name the state")
	}
}

func TestT418MuteControlLiveAgentIsNotMute(t *testing.T) {
	if mute, _ := ClassifyFleetMute(3, 1, 2); mute {
		t.Fatal("a live agent is a rescuer — not mute")
	}
}

func TestT418MuteControlNoQueueIsNotMute(t *testing.T) {
	if mute, _ := ClassifyFleetMute(3, 0, 0); mute {
		t.Fatal("stuck but idle is not the mute-with-queued-work case")
	}
}
