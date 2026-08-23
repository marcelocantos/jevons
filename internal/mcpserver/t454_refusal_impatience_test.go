// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/converge"
)

// 🎯T454 daemon glue: refusal-only terminals latch RefusalHold; impatience
// observe keeps the gap open and suppresses rungs; a substantive terminal
// pulse closes with the agent-work notice.
func TestT454ImpatienceRefusalHoldFromTerminal(t *testing.T) {
	t.Parallel()
	now := time.Unix(20_000, 0)
	s, activity := impatienceFixture(t, now)

	pm := &recordingPostmortem{}
	rep := &recordingRepressure{}
	ov := &recordingOverseer{}
	hum := &recordingHuman{}
	eng := NewImpatienceEngine(ImpatienceEngineArgs{
		Sinks:      converge.Sinks{RePressure: rep, Overseer: ov, Human: hum},
		Postmortem: pm,
	})
	s.SetImpatienceEngine(eng)
	s.SetIdlePressureHooks(IdlePressureHooks{
		MissionOpen: func(string) bool { return true },
	})
	running := func(name string) bool { return name == "jv-t317-stuck" }

	// Open gap past repressure.
	s.idlePressureSweep(idlePressureDeps{Now: now, Running: running})
	s.idlePressureSweep(idlePressureDeps{
		Now: now.Add(converge.RepressureAfter), Running: running,
	})
	if len(rep.agents) == 0 {
		t.Fatal("setup: want a repressure before the refusal wall")
	}
	rep.agents = nil

	// Refusal-only terminal latches the hold.
	spend := "You've hit your monthly spend limit. Run /usage-credits to manage your limit."
	activity.NoteTerminalOutcome("jv-t317-stuck", spend)
	if !activity.Get("jv-t317-stuck").RefusalHold {
		t.Fatal("NoteTerminalOutcome must latch RefusalHold on spend-limit text")
	}

	// Phase=working under hold must not close; rungs must not climb.
	activity.by["jv-t317-stuck"] = IdleActivity{
		Phase: "working", Updated: now, RefusalHold: true, LastTerminal: spend,
	}
	s.idlePressureSweep(idlePressureDeps{
		Now: now.Add(converge.HumanAlertAfter + time.Hour), Running: running,
	})
	eng.mu.Lock()
	open := eng.set.Len()
	tracked := eng.ladder.Tracked("jv-t317-stuck")
	eng.mu.Unlock()
	if open != 1 {
		t.Fatalf("refusal-hold working must keep the gap open, open=%d", open)
	}
	if !tracked {
		t.Fatal("incident must stay tracked under refusal hold")
	}
	if len(rep.agents) != 0 || len(ov.events) != 0 || len(hum.raised) != 0 {
		t.Fatalf("refusal hold must suppress rungs: rep=%v ov=%v hum=%v",
			rep.agents, ov.events, hum.raised)
	}
	if len(pm.texts) != 0 {
		t.Fatalf("must not emit Cleared postmortem under refusal: %v", pm.texts)
	}

	// Substantive terminal clears hold and pulses satisfaction.
	work := "Done — implemented refusal-hold; suite green."
	activity.NoteTerminalOutcome("jv-t317-stuck", work)
	act := activity.Get("jv-t317-stuck")
	if act.RefusalHold || !act.SubstantivePulse {
		t.Fatalf("substantive terminal: hold=%v pulse=%v", act.RefusalHold, act.SubstantivePulse)
	}
	s.idlePressureSweep(idlePressureDeps{
		Now: now.Add(converge.HumanAlertAfter + 2*time.Hour), Running: running,
	})
	eng.mu.Lock()
	open = eng.set.Len()
	tracked = eng.ladder.Tracked("jv-t317-stuck")
	eng.mu.Unlock()
	if open != 0 || tracked {
		t.Fatalf("substantive turn must close gap: open=%d tracked=%v", open, tracked)
	}
	if len(pm.texts) != 1 {
		t.Fatalf("want one postmortem, got %d %v", len(pm.texts), pm.texts)
	}
	if !strings.Contains(pm.texts[0], "returned to working") {
		t.Fatalf("substantive close must name agent work:\n%s", pm.texts[0])
	}
	if strings.Contains(pm.texts[0], "provider began accepting") {
		t.Fatalf("substantive close must not claim provider resume:\n%s", pm.texts[0])
	}
}
