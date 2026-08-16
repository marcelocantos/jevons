// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/wakebatch"
)

// 🎯T413: a reaped agent generates no parent-facing repair prompt; a
// queued pre-reap idle event is dropped at flush; a crashed open-mission
// agent (still registered, working intent) still produces a live prompt.
func TestT413ReapedAgentEmitsNoIdlePrompt(t *testing.T) {
	s, sender := t451Server(t, t.TempDir(),
		claudia.AgentDef{Name: "jevons-po", Purpose: claudia.PurposeWork, SessionID: "po"},
	)
	s.emitWorkerIdleToParent("jv-t413-auto", "working", "idle")
	if got := sender.delivered(); t451IdleText(got, "jv-t413-auto") {
		t.Fatalf("reaped/absent agent still woke the PO: %v", got)
	}
}

func TestT413QueuedPreReapNotificationIsDropped(t *testing.T) {
	evs := []wakebatch.Event{
		{Recipient: "jevons-po", Kind: eventWorkerIdle, Subject: "jv-t413-auto"},
		{Recipient: "jevons-po", Kind: eventWorkerIdle, Subject: "still-here"},
	}
	present := map[string]bool{"still-here": true}
	got := FilterIdleEventsForLiveAgents(evs, func(name string) bool { return present[name] })
	if len(got) != 1 || got[0].Subject != "still-here" {
		t.Fatalf("kept %+v; want only still-here (reaped subject dropped)", got)
	}
}

func TestT413CrashedOpenMissionStillNotifies(t *testing.T) {
	// Control: working intent + still registered + idle after a crash
	// must still emit — silencing that is the over-broad mutant.
	if !ShouldEmitWorkerIdle("working", "idle", claudia.PurposeWork, true,
		fleetintent.Working, fleetintent.Working) {
		t.Fatal("open-mission crashed worker was silenced — T171/T208 must survive")
	}
	// Reaped intent must not emit.
	if ShouldEmitWorkerIdle("working", "idle", claudia.PurposeWork, true,
		fleetintent.Working, fleetintent.Reaped) {
		t.Fatal("reaped intent still emitted an idle prompt")
	}
}
