// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
)

func TestClassifyFleetRecoverStuckBusy(t *testing.T) {
	t.Parallel()
	o := FleetRecoverObs{
		Name: "jv-t236", Purpose: claudia.PurposeWork, ProcessRunning: true,
		HasOpenMission: true, PromptInFlight: true,
		SinceProgress: 2 * time.Minute, StuckTimeout: 90 * time.Second,
		Phase: "working",
	}
	act, reason := ClassifyFleetRecover(o)
	if act != FleetRecoverUnstick || reason != "stuck_busy" {
		t.Fatalf("got %s/%s want unstick/stuck_busy", act, reason)
	}
	// Healthy long tool turn: fresh progress heartbeat → no false unstick.
	o.SinceProgress = 5 * time.Second
	act, reason = ClassifyFleetRecover(o)
	if act != FleetRecoverSkip {
		t.Fatalf("fresh progress got %s/%s want skip", act, reason)
	}
}

func TestClassifyFleetRecoverTerminalErrorAndEmpty(t *testing.T) {
	t.Parallel()
	base := FleetRecoverObs{
		Name: "jv-t236", Purpose: claudia.PurposeWork, ProcessRunning: true,
		HasOpenMission: true, Phase: "idle", NeedsRecover: true,
	}
	// Grok Internal error → backend class → rebrief.
	o := base
	o.FailureClass = agenterr.ClassifyText("Internal error")
	act, reason := ClassifyFleetRecover(o)
	if act != FleetRecoverRebrief || !strings.Contains(reason, "terminal_failure") {
		t.Fatalf("internal error: %s/%s", act, reason)
	}
	if o.FailureClass != agenterr.ClassBackendUnavailable {
		t.Fatalf("class=%s want backend_unavailable", o.FailureClass)
	}
	// Empty terminal.
	o = base
	o.TerminalEmpty = true
	o.FailureClass = agenterr.ClassNone
	act, reason = ClassifyFleetRecover(o)
	if act != FleetRecoverRebrief || reason != "terminal_empty" {
		t.Fatalf("empty: %s/%s", act, reason)
	}
	// Auth fail closed.
	o = base
	o.FailureClass = agenterr.ClassAuth
	act, reason = ClassifyFleetRecover(o)
	if act != FleetRecoverSkip || !strings.HasPrefix(reason, "non_transient") {
		t.Fatalf("auth: %s/%s", act, reason)
	}
}

func TestNoteTerminalOutcomeAndSweepDeliver(t *testing.T) {
	t.Parallel()
	actTrack := NewIdleActivityTracker()
	actTrack.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	actTrack.NoteTerminalOutcome("w1", "Internal error")
	got := actTrack.Get("w1")
	if !got.NeedsRecover || got.FailureClass != agenterr.ClassBackendUnavailable {
		t.Fatalf("latch: %+v", got)
	}

	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "w1", Purpose: claudia.PurposeWork, TargetID: "T236",
		SessionID: "s1", AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	var pushed []string
	reps := SweepFleetRecover(FleetRecoverSweepArgs{
		Reg: reg, Activity: actTrack,
		ProcessRunning: func(string) bool { return true },
		PromptInFlight: func(string) bool { return false },
		Push: func(target string, interrupt bool, event, text string) error {
			pushed = append(pushed, target+"|"+event)
			return nil
		},
		Now:          time.Unix(1_700_000_000, 0),
		OverseerName: "jevons",
	})
	var delivered bool
	for _, r := range reps {
		if r.Name == "w1" && r.Delivered {
			delivered = true
			if r.Action != FleetRecoverRebrief {
				t.Fatalf("action=%s", r.Action)
			}
		}
	}
	if !delivered || len(pushed) != 1 {
		t.Fatalf("delivered=%v pushed=%v reps=%+v", delivered, pushed, reps)
	}
	if actTrack.Get("w1").NeedsRecover {
		t.Fatal("NeedsRecover should clear after deliver")
	}
}

func TestClassifyFleetRecoverSkipsNonWork(t *testing.T) {
	t.Parallel()
	o := FleetRecoverObs{
		Purpose: claudia.PurposeAside, ProcessRunning: true, HasOpenMission: true,
		NeedsRecover: true, FailureClass: agenterr.ClassBackendUnavailable,
	}
	act, reason := ClassifyFleetRecover(o)
	if act != FleetRecoverSkip || reason != "not_work_purpose" {
		t.Fatalf("%s/%s", act, reason)
	}
}
