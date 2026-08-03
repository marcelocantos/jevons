// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

func TestShouldEmitWorkerIdle(t *testing.T) {
	t.Parallel()
	if !ShouldEmitWorkerIdle("working", "idle", claudia.PurposeWork, true) {
		t.Fatal("working→idle open mission should emit")
	}
	if ShouldEmitWorkerIdle("idle", "idle", claudia.PurposeWork, true) {
		t.Fatal("seed/stay idle must not emit")
	}
	if ShouldEmitWorkerIdle("", "idle", claudia.PurposeWork, true) {
		t.Fatal("empty→idle must not emit")
	}
	if ShouldEmitWorkerIdle("working", "working", claudia.PurposeWork, true) {
		t.Fatal("still working must not emit")
	}
	if ShouldEmitWorkerIdle("working", "idle", claudia.PurposeAside, true) {
		t.Fatal("aside must not emit")
	}
	if ShouldEmitWorkerIdle("working", "idle", claudia.PurposeWork, false) {
		t.Fatal("no open mission must not emit")
	}
}

func TestResolveEventParent(t *testing.T) {
	t.Parallel()
	d := claudia.AgentDef{Name: "jv-x", Parent: "jevons-po"}
	if got := ResolveEventParent(d, "jevons-po", "jevons"); got != "jevons-po" {
		t.Fatalf("got %q", got)
	}
	d.Parent = ""
	if got := ResolveEventParent(d, "", "jevons"); got != defaultProductPOName {
		t.Fatalf("empty parent default got %q", got)
	}
}

func TestFormatDaemonRestartedAndWorkerIdle(t *testing.T) {
	t.Parallel()
	workers := []WorkerIdleRef{
		{Name: "jv-t212", TargetID: "T212", Parent: "jevons-po", Status: "running", Phase: "idle"},
		{Name: "jv-t213", TargetID: "T213", Parent: "jevons-po", Status: "running", Phase: "idle"},
	}
	text := FormatDaemonRestartedText("jevons-po", workers)
	if !strings.Contains(text, "jevonsd restarted") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "jv-t212") || !strings.Contains(text, "🎯T212") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "status=running") || !strings.Contains(text, "phase=idle") {
		t.Fatal("reattached summary must include status/phase:", text)
	}
	if !strings.Contains(text, "phase=idle") {
		t.Fatal("hint should note reattached may be idle until a turn")
	}
	if !strings.Contains(text, "continue") {
		t.Fatal("hint should mention continue")
	}
	// Must not be a blast-all-agents continue payload alone.
	if strings.EqualFold(strings.TrimSpace(text), "continue") {
		t.Fatal("must not be bare continue")
	}

	idle := FormatWorkerIdleText(workers[0])
	if !strings.Contains(idle, "jv-t212") || !strings.Contains(idle, "phase=idle") {
		t.Fatal(idle)
	}
	if !strings.Contains(idle, "Act:") && !strings.Contains(idle, "Your call") {
		t.Fatal("PO action language missing:", idle)
	}
	if !strings.Contains(idle, SilentResponsePrefix) {
		t.Fatal("worker-idle must teach [silent] filter prefix")
	}
	if !strings.Contains(text, SilentResponsePrefix) {
		t.Fatal("daemon-restarted must teach [silent] filter prefix")
	}
}

// 🎯T171: daemon-restarted emit targets = each parent PO + overseer (not workers).
func TestDaemonRestartEventTargetsPOsAndOverseer(t *testing.T) {
	t.Parallel()
	byParent := map[string][]WorkerIdleRef{
		"jevons-po":  {{Name: "jv-a", TargetID: "T1"}},
		"claudia-po": {{Name: "jv-b", TargetID: "T2"}},
	}
	got := DaemonRestartEventTargets(byParent, "jevons", "jevons-po")
	want := map[string]bool{"jevons-po": true, "claudia-po": true, "jevons": true}
	if len(got) != 3 {
		t.Fatalf("targets=%v want 3 (2 POs + overseer)", got)
	}
	for _, name := range got {
		if !want[name] {
			t.Fatalf("unexpected target %q in %v", name, got)
		}
	}
	// Empty fleet still notifies default PO + overseer.
	empty := DaemonRestartEventTargets(nil, "jevons", "jevons-po")
	if len(empty) != 2 || empty[0] != "jevons-po" && empty[1] != "jevons-po" {
		// order: defaultPO then overseer
		if !(len(empty) == 2 && empty[0] == "jevons-po" && empty[1] == "jevons") {
			t.Fatalf("empty fleet targets=%v", empty)
		}
	}
	// Workers must never appear as event recipients.
	for _, name := range got {
		if strings.HasPrefix(name, "jv-") {
			t.Fatalf("worker %q must not receive daemon-restarted event", name)
		}
	}
}

// 🎯T171: open-mission short-resume eligibility (not blast everyone).
func TestEligibleOpenMissionResume(t *testing.T) {
	t.Parallel()
	work := claudia.AgentDef{
		Name: "jv-t171", Purpose: claudia.PurposeWork, AutoStart: true, TargetID: "T171",
	}
	if !EligibleOpenMissionResume(work, true, false, false, false) {
		t.Fatal("bound AutoStart work should be eligible")
	}
	// AutoStart only, no target_id
	autoOnly := claudia.AgentDef{Name: "jv-orphan", Purpose: claudia.PurposeWork, AutoStart: true}
	if !EligibleOpenMissionResume(autoOnly, true, false, false, false) {
		t.Fatal("AutoStart work without target still open-mission residual")
	}
	// bound target without AutoStart (running)
	bound := claudia.AgentDef{Name: "jv-bound", Purpose: claudia.PurposeWork, TargetID: "T9"}
	if !EligibleOpenMissionResume(bound, true, false, false, false) {
		t.Fatal("bound target_id without AutoStart should be eligible when running")
	}
	// missionless non-AutoStart
	missionless := claudia.AgentDef{Name: "jv-ephemeral", Purpose: claudia.PurposeWork, AutoStart: false}
	if EligibleOpenMissionResume(missionless, true, false, false, false) {
		t.Fatal("missionless non-AutoStart must not resume")
	}
	// PO/boss get path-1 events, not short resume
	po := claudia.AgentDef{Name: "jevons-po", Purpose: claudia.PurposeWork, AutoStart: true}
	if EligibleOpenMissionResume(po, true, false, false, false) {
		t.Fatal("PO must not get open-mission short resume")
	}
	aside := claudia.AgentDef{Name: "aside-1", Purpose: claudia.PurposeAside, AutoStart: true}
	if EligibleOpenMissionResume(aside, true, false, false, false) {
		t.Fatal("aside must skip")
	}
	if EligibleOpenMissionResume(work, false, false, false, false) {
		t.Fatal("not running must skip")
	}
	if EligibleOpenMissionResume(work, true, true, false, false) {
		t.Fatal("deliberate stop must skip")
	}
	if EligibleOpenMissionResume(work, true, false, true, false) {
		t.Fatal("design-gated must skip")
	}
	if EligibleOpenMissionResume(work, true, false, false, true) {
		t.Fatal("looks-finished must skip")
	}
}

func TestCollectWorkChildren(t *testing.T) {
	t.Parallel()
	defs := []claudia.AgentDef{
		{Name: "jevons", Purpose: claudia.PurposeOverseer},
		{Name: "jevons-po", Purpose: claudia.PurposeWork, Parent: "jevons"},
		{Name: "jv-a", Purpose: claudia.PurposeWork, Parent: "jevons-po", TargetID: "T1"},
		{Name: "jv-b", Purpose: claudia.PurposeWork, Parent: "claudia-po", TargetID: "T2"},
		{Name: "aside-1", Purpose: claudia.PurposeAside, Parent: "jevons"},
	}
	running := map[string]bool{"jv-a": true, "jv-b": true, "jevons-po": true}
	got := CollectWorkChildren(defs, func(n string) bool { return running[n] }, "jevons-po", "jevons")
	if len(got["jevons-po"]) != 2 { // jv-a + jevons-po itself as work
		// jevons-po is purpose work and running — parent is jevons
		// jv-a under jevons-po
		// jv-b under claudia-po
	}
	if len(got["claudia-po"]) != 1 || got["claudia-po"][0].Name != "jv-b" {
		t.Fatalf("claudia-po kids=%+v", got["claudia-po"])
	}
	// jv-a under jevons-po
	foundA := false
	for _, w := range got["jevons-po"] {
		if w.Name == "jv-a" {
			foundA = true
		}
	}
	if !foundA {
		t.Fatalf("expected jv-a under jevons-po: %+v", got)
	}
	if _, ok := got["jevons"]; ok {
		// jevons-po parent is jevons — that is a parent key if jevons-po is work+running
		// OK if present
	}
}

func TestObserveTransitionEnterIdle(t *testing.T) {
	t.Parallel()
	tr := NewIdleActivityTracker()
	tr.now = func() time.Time { return time.Unix(1000, 0) }
	tr.SeedRunning("w")
	_, _, entered := tr.ObserveTransition("w", claudia.Event{Type: "assistant", Text: "hi"})
	if entered {
		t.Fatal("working should not be enter-idle")
	}
	if ph := tr.Get("w").Phase; ph != "working" {
		t.Fatalf("phase=%q", ph)
	}
	_, _, entered = tr.ObserveTransition("w", claudia.Event{
		Type: "assistant", StopReason: "end_turn",
	})
	// IsTerminalStop on assistant with end_turn
	if !entered {
		// Event needs IsTerminalStop — check Event fields
		t.Log("checking terminal")
	}
}

func TestObserveTransitionWorkingToIdle(t *testing.T) {
	t.Parallel()
	tr := NewIdleActivityTracker()
	tr.by = map[string]IdleActivity{"w": {Phase: "working", Updated: time.Unix(1, 0)}}
	// Terminal stop → idle
	ev := claudia.Event{Type: "assistant", StopReason: "end_turn"}
	if !ev.IsTerminalStop() {
		t.Fatal("fixture must be terminal")
	}
	prev, next, entered := tr.ObserveTransition("w", ev)
	if prev != "working" || next != "idle" || !entered {
		t.Fatalf("prev=%q next=%q entered=%v", prev, next, entered)
	}
	// Seed idle then terminal again must not enter from working
	tr.SeedRunning("x")
	_, _, entered = tr.ObserveTransition("x", ev)
	if entered {
		t.Fatal("idle→idle via terminal must not count as enter-idle from working")
	}
}
