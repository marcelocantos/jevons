// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"
	"time"
)

// 🎯T601: the overseer was declared stuck at since_progress=7m14s while its
// pane read "Calling jevonsmcp… ✽ Orbiting… esc to interrupt". The
// recovery that followed interrupted real work. Event silence is not
// absence of work: a long tool call, or plain model thinking, emits no ACP
// events at all.
func busyButSilent() cockpitObs {
	return cockpitObs{
		Registered:    true,
		ProcAlive:     true,
		ChatAttached:  true,
		SinceProgress: 7 * time.Minute,
		QueueDepth:    6,
	}
}

func TestAWorkingPaneIsNotStuck(t *testing.T) {
	o := busyButSilent()
	o.PaneWorking = true
	if got := planCockpit(o, 0, 0, 0); got == cockpitUnstickBusy {
		t.Fatal("a pane that says it is working was declared stuck")
	}
}

// The control. Without it the fix would be indistinguishable from
// disabling the detector.
func TestSilenceWithAnIdlePaneIsStillStuck(t *testing.T) {
	o := busyButSilent() // PaneWorking false
	if got := planCockpit(o, 0, 0, 0); got != cockpitUnstickBusy {
		t.Fatalf("a silent, idle overseer with work owed was not stuck: %v", got)
	}
}

// An ACP provider's in-flight flag must NOT read as pane evidence: "the
// client says a prompt is outstanding" is exactly what a wedge looks like,
// and treating it as work would make the 🎯T204 case unconvictable.
func TestAnInFlightPromptWithNoPaneEvidenceIsStillStuck(t *testing.T) {
	o := busyButSilent()
	o.PromptInFlight = true
	o.QueueDepth = 0
	o.PaneWorking = false
	if got := planCockpit(o, 0, 0, 0); got != cockpitUnstickBusy {
		t.Fatalf("a wedged in-flight prompt escaped the stuck verdict: %v", got)
	}
}

// A working pane does not paper over a session that is actually down —
// the earlier phases still win.
func TestPaneEvidenceDoesNotMaskADeadSession(t *testing.T) {
	o := busyButSilent()
	o.PaneWorking = true
	o.ProcAlive = false
	if got := planCockpit(o, 0, 0, 0); got != cockpitLaunch {
		t.Fatalf("a dead process with a stale working pane: %v", got)
	}
	o = busyButSilent()
	o.PaneWorking = true
	o.ChatAttached = false
	if got := planCockpit(o, 0, 0, 0); got != cockpitAttach {
		t.Fatalf("a detached chat with a working pane: %v", got)
	}
}

// Fresh progress is still healthy, pane or no pane.
func TestRecentProgressIsHealthy(t *testing.T) {
	o := busyButSilent()
	o.SinceProgress = time.Second
	if got := planCockpit(o, 0, 0, 0); got != cockpitOK {
		t.Fatalf("a recently-progressing overseer: %v", got)
	}
}
