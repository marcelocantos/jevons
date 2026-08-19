// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

// 🎯T518 — a start that answered queued or delivered_unconfirmed is not
// retired as unbriefed_seat.
//
// The live specimen: on 2026-08-18T11:16Z the seat jv-t515-relayrecord was
// removed with reason unbriefed_seat seven minutes after a start whose brief
// was in flight — the daemon's own queue held it for delivery at the next
// turn boundary, and releaseUnbriefedSeat destroyed the seat and the held
// brief together. The remint that followed then raced the auto-spawn.
//
// The rule these oracles pin: queued / delivered_unconfirmed mean the brief
// was ACCEPTED (or the instrument could not decide) — never that no brief
// exists. Only positive evidence of no brief (a clean "sent" whose watch saw
// no user message and no queue record, 🎯T387) still reaps.

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

func t518Harness(t *testing.T) (*Server, *claudia.Registry, *fakeSender) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t518-w", WorkDir: dir, SessionID: "s518",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSender{alive: true}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		if name != "jv-t518-w" {
			return nil, false, fmt.Errorf("unknown %s", name)
		}
		return fs, false, nil
	})
	return s, reg, fs
}

// The pure classifier: the two middle verdicts of the 🎯T416 contract (plus
// the interrupt variant of queued) are in flight; everything else is not.
func TestT518StartVerdictInFlight(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"queued", "interrupted_queued", "delivered_unconfirmed"} {
		if !StartVerdictInFlight(status, nil) {
			t.Errorf("StartVerdictInFlight(%q, nil) = false, want true", status)
		}
	}
	for _, status := range []string{"sent", "rehydrated_sent", "interrupted_sent", "", "not_submitted"} {
		if StartVerdictInFlight(status, nil) {
			t.Errorf("StartVerdictInFlight(%q, nil) = true, want false", status)
		}
	}
	// A send error is decided by ConfirmTurnBegan's evidence rules (🎯T429),
	// never re-classified as in flight here.
	if StartVerdictInFlight("queued", fmt.Errorf("boom")) {
		t.Error("a send error must not classify as in flight")
	}
}

// A start whose brief lands in the daemon's own queue (a turn was in flight)
// answers "queued": the error is marked in-flight and the teardown fork keeps
// the seat registered — no unbriefed_seat removal.
func TestT518QueuedStartBriefKeepsSeat(t *testing.T) {
	s, reg, fs := t518Harness(t)
	// The daemon believes a turn is running, so deliverToSender takes the
	// queue arm and answers "queued" without ever touching the sender.
	s.noteTurnInFlight("jv-t518-w")
	s.EnsureAgentEventsWired("jv-t518-w")

	err := s.deliverStartPrompt("jv-t518-w", "Execute 🎯T518.")
	if err == nil {
		t.Fatal("a queued brief has not begun a turn; deliverStartPrompt must still error")
	}
	if !BriefInFlight(err) {
		t.Fatalf("queued verdict not marked in flight: %v", err)
	}
	if len(fs.sent) != 0 {
		t.Fatalf("queued arm must hold the brief, not paste it: %v", fs.sent)
	}
	if n := s.pendingAgentSends("jv-t518-w"); n != 1 {
		t.Fatalf("pending sends = %d, want 1 (the held brief)", n)
	}

	released, kept := s.startBriefFailureTeardown("jv-t518-w", false, err)
	if released || !kept {
		t.Fatalf("teardown fork = released=%v kept=%v, want kept", released, kept)
	}
	if reg.Def("jv-t518-w") == nil {
		t.Fatal("seat retired as unbriefed while its brief sat in the queue — the 🎯T518 incident")
	}
}

// The delivered_unconfirmed verdict — brief handed over, instrument undecided
// — keeps the seat the same way. Driven through the fork with the typed error
// the start path would produce for that status.
func TestT518UnconfirmedStartBriefKeepsSeat(t *testing.T) {
	s, reg, _ := t518Harness(t)
	err := &briefInFlightError{status: "delivered_unconfirmed", err: fmt.Errorf(
		"start brief to %q in flight, not yet a turn (status=delivered_unconfirmed): undecided", "jv-t518-w")}
	if !StartVerdictInFlight("delivered_unconfirmed", nil) {
		t.Fatal("delivered_unconfirmed must classify in flight")
	}
	released, kept := s.startBriefFailureTeardown("jv-t518-w", false, err)
	if released || !kept {
		t.Fatalf("teardown fork = released=%v kept=%v, want kept", released, kept)
	}
	if reg.Def("jv-t518-w") == nil {
		t.Fatal("seat retired on an undecided verdict")
	}
}

// The control (acceptance clause 1, second half): positive evidence of no
// brief still reaps. A clean "sent" whose watch saw no user message and no
// queue record is 🎯T387's phantom seat, and the fork must release it.
func TestT518ControlProvenNoBriefStillReaps(t *testing.T) {
	s, reg, _ := t518Harness(t)
	s.SetTurnWitness(witnessYielding(TurnEvidence{
		Observed: true, Durable: true, TranscriptAbsent: true,
		Detail: "no transcript was ever created (no user message, no queue record)",
	}))

	err := s.deliverStartPrompt("jv-t518-w", "Execute 🎯T518 control.")
	if err == nil {
		t.Fatal("sent-with-no-evidence must fail confirmation (🎯T387)")
	}
	if BriefInFlight(err) {
		t.Fatalf("proven-absent brief wrongly marked in flight: %v", err)
	}
	released, kept := s.startBriefFailureTeardown("jv-t518-w", false, err)
	if !released || kept {
		t.Fatalf("teardown fork = released=%v kept=%v, want released", released, kept)
	}
	if reg.Def("jv-t518-w") != nil {
		t.Fatal("phantom seat kept: proven no-brief must still reap unbriefed (🎯T387)")
	}
}

// Acceptance clause 2 — the remint race. After a queued start keeps the
// seat, a second start of the same name goes through the same registry row:
// existed=true and the same session, never a parallel row.
func TestT518RemintDoesNotMintParallelRow(t *testing.T) {
	s, reg, _ := t518Harness(t)
	s.noteTurnInFlight("jv-t518-w")
	s.EnsureAgentEventsWired("jv-t518-w")
	err := s.deliverStartPrompt("jv-t518-w", "Execute 🎯T518.")
	if !BriefInFlight(err) {
		t.Fatalf("fixture: queued start not in flight: %v", err)
	}
	if _, kept := s.startBriefFailureTeardown("jv-t518-w", false, err); !kept {
		t.Fatal("fixture: seat not kept")
	}
	before := reg.Def("jv-t518-w")
	if before == nil {
		t.Fatal("fixture: seat missing")
	}

	// The second start of the same name resolves to the SAME row.
	def, existed, _, serr := s.stitchAgentStart(
		"jv-t518-w", before.WorkDir, "", "", "", "jevons-po", "work", "T518", "")
	if serr != nil {
		t.Fatalf("second start: %v", serr)
	}
	if !existed {
		t.Fatal("second start minted a fresh row for a live seat")
	}
	if def.SessionID != before.SessionID {
		t.Fatalf("second start rotated the session: %q → %q (parallel seat)",
			before.SessionID, def.SessionID)
	}
}
