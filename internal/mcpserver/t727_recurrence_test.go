// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Two instants standing for two genuinely distinct events.
var (
	t727EventA = time.Date(2026, 9, 21, 3, 0, 0, 0, time.UTC)
	t727EventB = t727EventA.Add(11 * time.Minute)
)

// 🎯T727 — a byte-identical recurrence is distinguishable from an echo, or
// the fleet knowingly accepts losing it.
//
// 🎯T717 made an occurrence mandatory at the notifyFleetHealth door. That
// guarantees a value is SUPPLIED; it does not guarantee the value MOVES when
// the event is new. Four emitters passed a key already in the prose (a name,
// a worker, a joined set), so a genuine second event still rendered
// byte-identical and T428 collapsed it as status=suppressed_replay.
//
// THE DISTINCTION THIS TARGET RESTS ON, stated because the obvious fix is a
// trap: stamping a timestamp or counter into every notice makes every notice
// unique and defeats 🎯T428 and 🎯T568 for the real echoes they exist to
// suppress. So the discriminator is chosen per emitter, from how the emitter
// is TRIGGERED — not applied as a blanket mechanism.
//
//	EDGE-TRIGGERED — the call site fires once per genuinely new event and
//	never re-renders a past one: an agent start, a parent kill, a spawn that
//	actually succeeded. There is no echo for a clock to defeat here, because
//	the emitter never offers the same event twice. The event's own instant is
//	therefore an honest discriminator, and fleetHealthEventOccurrence is it.
//
//	LEVEL-TRIGGERED — the call site re-reports a STANDING condition on every
//	pass: the dead-agent set, re-read by the periodic health hook, by
//	handleAgentList and by ensureAgentProcess. Consecutive passes over one
//	unchanged condition ARE echoes and must stay a single batch — T717's
//	audit records that as wanted behaviour, not a defect. A clock here is
//	precisely the trap. The discriminator must be a GENERATION: stable while
//	the condition persists, moving only when it lapses and returns.
//
// A level-triggered emitter reaching for the clock helper is the mutation the
// acceptance names, and TestT727LevelTriggeredSweepStaysOneBatch is the tape
// it goes RED on.

// t727EdgeOccurrenceFiles is every file allowed to build an occurrence from
// the clock, with the reason that emitter is edge-triggered. Adding an entry
// is a CLAIM: this call site fires once per new event and never re-reads a
// standing condition. If that is wrong, the clock makes every pass unique and
// T428 quietly stops collapsing real echoes — the failure this target exists
// to avoid. Classify the trigger before you add a line here.
var t727EdgeOccurrenceFiles = map[string]string{
	"mcp_health.go":       "noteAgentMCPHealth is reached only from handleAgentStart (agents.go): one notice per jevons_agent_start, never a re-read of a standing MCP fault.",
	"sendq_survive.go":    "restartHeldSendqForDrain is reached only from the parent-kill handler (agents.go): one notice per kill, never a re-read of the held-sendq set.",
	"frontier_consume.go": "the notice sits inside case FrontierConsumeSpawn, after a spawn actually succeeded: one notice per spawn, not one per sweep of the leaf.",
}

// t727LevelTriggeredFile carries the one level-triggered emitter, which must
// keep a generation and must NOT reach for the clock.
const t727LevelTriggeredFile = "fleet_health.go"

func t727PackageSources(t *testing.T) map[string]string {
	t.Helper()
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	out := map[string]string{}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		out[name] = string(b)
	}
	if len(out) == 0 {
		t.Fatal("no package sources found — the ratchet would pass vacuously")
	}
	return out
}

// The ratchet. It fails in both directions on purpose: an emitter that stops
// using the clock has silently gone back to a stable key (the T717 bug), and
// an emitter that starts using it has made a classification claim nobody
// wrote down.
func TestT727EdgeTriggeredOccurrenceCallSitesAreClassified(t *testing.T) {
	t.Parallel()
	const helper = "fleetHealthEventOccurrence("
	srcs := t727PackageSources(t)

	// The helper's own definition lives here; it is not a call site.
	const helperHome = "agents.go"

	for name, src := range srcs {
		if name == helperHome {
			continue
		}
		uses := strings.Contains(src, helper)
		reason, allowed := t727EdgeOccurrenceFiles[name]
		switch {
		case uses && !allowed:
			t.Errorf("%s builds a fleet-health occurrence from the clock but is not classified.\n"+
				"🎯T727: a clock is honest ONLY for an emitter that fires once per genuinely new\n"+
				"event and never re-renders a past one. If this call site sweeps a standing\n"+
				"condition, every pass becomes unique and 🎯T428 stops collapsing real echoes —\n"+
				"use a generation instead (see deadAgentOccurrence). If it is edge-triggered,\n"+
				"add %q to t727EdgeOccurrenceFiles with the reason.", name, name)
		case !uses && allowed:
			t.Errorf("%s no longer builds its occurrence from the clock, but is classified as\n"+
				"edge-triggered: %s\n"+
				"🎯T727: an occurrence keyed on something already in the prose renders the second\n"+
				"genuine event byte-identical to the first, and 🎯T428 collapses it as\n"+
				"status=suppressed_replay — the overseer never hears the recurrence. Restore the\n"+
				"moving discriminator, or reclassify this emitter and say why the loss is accepted.",
				name, reason)
		}
	}

	// The level-triggered emitter must stay on a generation.
	lvl, ok := srcs[t727LevelTriggeredFile]
	if !ok {
		t.Fatalf("%s is gone; the level-triggered classification is stale", t727LevelTriggeredFile)
	}
	if strings.Contains(lvl, helper) {
		t.Errorf("%s builds the dead-agent occurrence from the clock.\n"+
			"🎯T727: sweepDeadAgents re-reports a STANDING dead set on every health hook,\n"+
			"agent_list and ensureAgentProcess call. A clock makes each of those passes a new\n"+
			"batch, so one death is announced over and over — 🎯T717's audit records that\n"+
			"list-call echoes staying a single batch is WANTED. Keep the generation.",
			t727LevelTriggeredFile)
	}
	if !strings.Contains(lvl, "deadAgentStreak") {
		t.Errorf("%s no longer keeps a death generation (deadAgentStreak).\n"+
			"🎯T727: without it the same seat dying twice as stopped, either side of a\n"+
			"recovery, renders byte-identical and the second death is suppressed as a replay.",
			t727LevelTriggeredFile)
	}
}

// THE MUTATION TAPE the acceptance names. Consecutive sweeps of one unchanged
// dead set are echoes and stay a single batch. Swap deadAgentOccurrence for
// anything that moves per call — a clock, a counter — and this goes RED.
func TestT727LevelTriggeredSweepStaysOneBatch(t *testing.T) {
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})
	reps := []DeadAgentReport{{Name: "jv-t727-echo-recurrence"}}

	// The health hook, an agent_list call, and an ensureAgentProcess call all
	// sweep the same standing condition within one spell.
	s.notifyDeadAgents(reps)
	s.notifyDeadAgents(reps)
	s.notifyDeadAgents(reps)
	if len(inbox.texts) != 1 {
		t.Fatalf("three sweeps of one unchanged dead set delivered %d batches; want 1.\n"+
			"A discriminator that moves per CALL rather than per INCIDENT announces the same\n"+
			"death once per sweep — 🎯T727's trap, and what 🎯T428 exists to collapse.\n%v",
			len(inbox.texts), inbox.texts)
	}
}

// The other half of the same tape: the condition lapsing and returning IS a
// new incident, and it must reach the overseer.
func TestT727SecondDeathAfterRecoveryReachesOverseer(t *testing.T) {
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})
	reps := []DeadAgentReport{{Name: "jv-t727-echo-recurrence"}}

	s.notifyDeadAgents(reps)
	s.notifyDeadAgents(nil)  // recovered: the streak lapses
	s.notifyDeadAgents(reps) // died again: a genuinely new incident

	if len(inbox.texts) != 2 {
		t.Fatalf("second death after recovery delivered %d batches; want 2\n%v",
			len(inbox.texts), inbox.texts)
	}
	if inbox.texts[0] == inbox.texts[1] {
		t.Fatal("the two death notices are byte-identical — the generation never reached the wire, " +
			"so 🎯T428 will collapse the second death as a replay of the first")
	}
}

// A replayed echo of the SECOND incident is still suppressed. Distinguishing a
// recurrence must not cost the replay guard: both halves hold at once.
func TestT727EchoOfASecondIncidentIsStillSuppressed(t *testing.T) {
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})
	reps := []DeadAgentReport{{Name: "jv-t727-echo-recurrence"}}

	s.notifyDeadAgents(reps)
	s.notifyDeadAgents(nil)
	s.notifyDeadAgents(reps)
	if len(inbox.texts) != 2 {
		t.Fatalf("setup: want 2 batches, got %d\n%v", len(inbox.texts), inbox.texts)
	}

	res, err := s.deliverByName("jevons", inbox.texts[1], OriginAgent, false)
	if err != nil {
		t.Fatalf("re-offer of the second incident: %v", err)
	}
	if res.Status != StatusSuppressedReplay {
		t.Fatalf("echo of the second incident status=%q want %s", res.Status, StatusSuppressedReplay)
	}
}

// The edge-triggered discriminator moves between events and refuses when it
// cannot be built. Paired with the ratchet above, which is what binds the four
// emitters to it.
func TestT727EdgeOccurrenceMovesBetweenEvents(t *testing.T) {
	t.Parallel()
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})

	// Same operator-facing names, two genuinely distinct events.
	const line = "🎯T530 parent kill: restarted 1 held-sendq seat(s) for drain under \"jevons-po\": jv-t727."
	s.notifyFleetHealth(fleetHealthEventOccurrence("jv-t727", t727EventA), line)
	s.notifyFleetHealth(fleetHealthEventOccurrence("jv-t727", t727EventB), line)
	if len(inbox.texts) != 2 {
		t.Fatalf("two distinct drain-restarts of the same cohort delivered %d batches; want 2\n%v",
			len(inbox.texts), inbox.texts)
	}

	// And an unbuildable occurrence refuses rather than sending an
	// undiscriminated notice — empty occurrence is refused at the door.
	before := len(inbox.texts)
	s.notifyFleetHealth(fleetHealthEventOccurrence("", t727EventA), line)
	if len(inbox.texts) != before {
		t.Fatal("an occurrence with no stable part was delivered anyway")
	}
}
