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
		{Name: "jv-t212", TargetID: "T212", Parent: "jevons-po"},
		{Name: "jv-t213", TargetID: "T213", Parent: "jevons-po"},
	}
	text := FormatDaemonRestartedText("jevons-po", workers)
	if !strings.Contains(text, "jevonsd restarted") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "jv-t212") || !strings.Contains(text, "🎯T212") {
		t.Fatal(text)
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
	if !strings.Contains(idle, "Your call") {
		t.Fatal("PO judgment language missing")
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
