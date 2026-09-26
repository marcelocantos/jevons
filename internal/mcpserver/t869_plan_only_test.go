// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/converge"
)

// The 2026-09-26 overseer sequence: two restart nudges, a sentinel notice,
// a forwarded plan sentence, an impatience-closed notice. The seat answered
// each with a plan and no tool call. None of those causes may submit
// another prompt after that reply.
func TestT869OverseerSequenceDoesNotReprompt(t *testing.T) {
	const reply = "I'll inspect the workspace, the T65 brief, the directory listing, and git status."
	causes := []string{
		promptRestartNudge, promptRestartNudge,
		promptSentinel, promptAgentForward, promptImpatience,
	}
	for _, cause := range causes {
		if earnsAnotherPrompt(0, reply, cause) {
			t.Fatalf("plan-only reply earned another %s prompt", cause)
		}
	}
	if !earnsAnotherPrompt(1, reply, promptSentinel) {
		t.Fatal("a turn that called a tool must still accept a sentinel notice")
	}
	if earnsAnotherPrompt(0, "", promptSentinel) == false {
		t.Fatal("no reply yet must not block the sequence's own prompts")
	}
	if shouldForwardAgentReply(reply, 0) {
		t.Fatal("plan-only reply was forwarded to the parent")
	}
	const finish = "Done — implemented the hold. suite green."
	if !shouldForwardAgentReply(finish, 0) {
		t.Fatal("a finish with no tool call in the closing message must still be forwarded")
	}
	if !shouldForwardAgentReply(reply, 2) {
		t.Fatal("a reply that called tools must still be forwarded")
	}

	now := time.Unix(20_000, 0)
	s, activity := impatienceFixture(t, now)
	pm := &recordingPostmortem{}
	rep := &recordingRepressure{}
	eng := NewImpatienceEngine(ImpatienceEngineArgs{
		Sinks:      converge.Sinks{RePressure: rep, Overseer: &recordingOverseer{}, Human: &recordingHuman{}},
		Postmortem: pm,
	})
	s.SetImpatienceEngine(eng)
	s.SetIdlePressureHooks(IdlePressureHooks{MissionOpen: func(string) bool { return true }})
	running := func(name string) bool { return name == "jv-t317-stuck" }

	s.idlePressureSweep(idlePressureDeps{Now: now, Running: running})
	s.idlePressureSweep(idlePressureDeps{Now: now.Add(converge.RepressureAfter), Running: running})
	if len(rep.agents) != 1 {
		t.Fatalf("setup: want one repressure, got %v", rep.agents)
	}

	activity.NoteTerminalTurn("jv-t317-stuck", reply, 0)
	act := activity.Get("jv-t317-stuck")
	if !act.PlanOnly || act.SubstantivePulse || act.Phase != "idle" {
		t.Fatalf("plan-only turn: plan=%v pulse=%v phase=%q", act.PlanOnly, act.SubstantivePulse, act.Phase)
	}
	// The T454 prose finish still closes. A plan must not.
	activity.NoteTerminalTurn("jv-t317-stuck", finish, 0)
	if activity.Get("jv-t317-stuck").PlanOnly || !activity.Get("jv-t317-stuck").SubstantivePulse {
		t.Fatalf("finish without a tool call must still pulse, got %+v", activity.Get("jv-t317-stuck"))
	}
	activity.NoteTerminalTurn("jv-t317-stuck", reply, 0)

	fired := len(rep.agents)
	later := now.Add(converge.RepressureAfter + converge.RepressureEvery + time.Hour)
	s.idlePressureSweep(idlePressureDeps{Now: later, Running: running})
	if len(rep.agents) != fired {
		t.Fatalf("plan-only turn armed another repressure: %v", rep.agents)
	}
	eng.mu.Lock()
	open := eng.set.Len()
	tracked := eng.ladder.Tracked("jv-t317-stuck")
	eng.mu.Unlock()
	if open == 0 || !tracked {
		t.Fatalf("plan-only turn closed the incident: open=%d tracked=%v", open, tracked)
	}
	if len(pm.texts) != 0 {
		t.Fatalf("plan-only turn emitted a close notice: %v", pm.texts)
	}

	// The forwarded plan sentence and the sentinel notice are the other
	// prompts in the sequence. After the no-tool reply, neither is sent.
	// This server has no registry: a missed withhold would panic in send
	// rather than launch a provider.
	hold := &Server{idleActivity: NewIdleActivityTracker()}
	var sent []string
	hold.notifyJevon = func(body string) { sent = append(sent, body) }
	hold.idleActivity.NoteTerminalTurn("jevons", reply, 0)
	if err := hold.deliverSentinel("jevons", "sentinel", "idle:mm2-t65-keys-doors"); err != nil {
		t.Fatal(err)
	}
	hold.notifyTurn("mm2-t65-keys-doors", reply, 0)
	if len(sent) != 0 {
		t.Fatalf("prompts after the no-tool reply: %q", sent)
	}

	// A tool-using turn is work again: the hold clears and the pulse can close.
	activity.NoteTerminalTurn("jv-t317-stuck", "Read the brief.", 1)
	got := activity.Get("jv-t317-stuck")
	if got.PlanOnly || !got.SubstantivePulse {
		t.Fatalf("tool turn = plan %v pulse %v", got.PlanOnly, got.SubstantivePulse)
	}
}

func TestT869ProseWorkingDoesNotSatisfy(t *testing.T) {
	now := time.Unix(30_000, 0)
	s, activity := impatienceFixture(t, now)
	rep := &recordingRepressure{}
	eng := NewImpatienceEngine(ImpatienceEngineArgs{
		Sinks: converge.Sinks{RePressure: rep, Overseer: &recordingOverseer{}, Human: &recordingHuman{}},
	})
	s.SetImpatienceEngine(eng)
	s.SetIdlePressureHooks(IdlePressureHooks{MissionOpen: func(string) bool { return true }})
	running := func(name string) bool { return name == "jv-t317-stuck" }
	s.idlePressureSweep(idlePressureDeps{Now: now, Running: running})
	s.idlePressureSweep(idlePressureDeps{Now: now.Add(converge.RepressureAfter), Running: running})

	activity.Observe("jv-t317-stuck", claudia.Event{Type: "assistant", Text: "I'll inspect."})
	if !activity.Get("jv-t317-stuck").ProseWorking {
		t.Fatal("assistant prose must be marked prose-working")
	}
	before := len(rep.agents)
	s.idlePressureSweep(idlePressureDeps{Now: now.Add(converge.RepressureAfter + time.Minute), Running: running})
	eng.mu.Lock()
	open := eng.set.Len()
	tracked := eng.ladder.Tracked("jv-t317-stuck")
	eng.mu.Unlock()
	if open == 0 || !tracked {
		t.Fatalf("prose without a tool closed the incident: open=%d tracked=%v", open, tracked)
	}
	if len(rep.agents) != before {
		t.Fatalf("prose armed another repressure: %v", rep.agents)
	}
}
