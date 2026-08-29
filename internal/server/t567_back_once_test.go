// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"
	"testing"
	"time"
)

// countBackFrames drains the live listener and counts "overseer is back"
// status frames.
func countBackFrames(live chan string) int {
	n := 0
	for {
		select {
		case line := <-live:
			if strings.Contains(line, `"type":"status"`) && strings.Contains(line, "overseer is back") {
				n++
			}
		default:
			return n
		}
	}
}

// 🎯T567: N re-attach / unstick recoveries with no intervening degraded
// state broadcast produce exactly one "overseer is back" frame — the one
// that answers the outage. The converge loop re-attaching on every tick
// across a daemon bounce used to print one line per tick.
func TestT567BackEmittedOncePerOutage(t *testing.T) {
	s := New("test", t.TempDir())
	live := make(chan string, 64)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, live)
	s.mu.Unlock()

	// No outage yet: a re-attach is silent.
	for i := 0; i < 5; i++ {
		s.broadcastCockpitReady("overseer is back")
	}
	if got := countBackFrames(live); got != 0 {
		t.Fatalf("re-attach with no outage emitted %d frames, want 0", got)
	}

	// One outage (down reason broadcast), then N recoveries: one frame.
	s.SetOverseerDownReason("overseer process exited")
	for i := 0; i < 7; i++ {
		s.broadcastCockpitReady("overseer is back")
	}
	if got := countBackFrames(live); got != 1 {
		t.Fatalf("7 recoveries after one outage emitted %d frames, want 1", got)
	}

	// A stuck frame is an outage too; M unstick cycles → one frame.
	s.markOverseerStuck()
	for i := 0; i < 4; i++ {
		s.broadcastCockpitReady("overseer is back")
	}
	if got := countBackFrames(live); got != 1 {
		t.Fatalf("4 unsticks after one stuck outage emitted %d frames, want 1", got)
	}

	// A second outage earns a second line.
	s.SetOverseerDownReason("overseer stuck-busy; relaunching")
	s.broadcastCockpitReady("overseer is back")
	if got := countBackFrames(live); got != 1 {
		t.Fatalf("second outage emitted %d frames, want 1", got)
	}
}

// 🎯T567: a busy overseer with no progress for 100s on a host at 3×/core
// is slow, not stuck — the watchdog stretches to 3×90s and planCockpit
// says OK. The same observation on an idle host is stuck-busy (control).
func TestT567StuckBusyThresholdScalesWithHostLoad(t *testing.T) {
	s := New("test", t.TempDir())
	obs := cockpitObs{
		Registered: true, ProcAlive: true, ChatAttached: true,
		PromptInFlight: true,
		SinceProgress:  100 * time.Second,
	}

	if got := s.stuckBusyTimeout(); got != DefaultStuckBusyTimeout {
		t.Fatalf("unknown load timeout = %s, want %s", got, DefaultStuckBusyTimeout)
	}
	if p := planCockpit(obs, 0, 8, s.stuckBusyTimeout()); p != cockpitUnstickBusy {
		t.Fatalf("idle host, 100s no progress: phase = %d, want unstick", p)
	}

	s.SetHostLoadSource(func() (float64, int) { return 48, 16 }) // 3×/core
	if got, want := s.stuckBusyTimeout(), 3*DefaultStuckBusyTimeout; got != want {
		t.Fatalf("loaded host timeout = %s, want %s", got, want)
	}
	if p := planCockpit(obs, 0, 8, s.stuckBusyTimeout()); p != cockpitOK {
		t.Fatalf("host at 3x/core, 100s no progress: phase = %d, want OK (slow, not stuck)", p)
	}

	// Still stuck eventually: past the stretched threshold it unsticks.
	obs.SinceProgress = 3*DefaultStuckBusyTimeout + time.Second
	if p := planCockpit(obs, 0, 8, s.stuckBusyTimeout()); p != cockpitUnstickBusy {
		t.Fatalf("past stretched threshold: phase = %d, want unstick", p)
	}
}
