// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

// 🎯T729 — a seat whose CLI stalled on startup is retried, not retired.
//
// The specimen, three eventlog lines on ge-t190-desktop-cook (2026-09-20,
// ~/.jevons/logs/events.jsonl:352002-352004):
//
//	15:19:48Z seat_stop      opening brief proven undelivered; seat released (🎯T387)
//	15:19:50Z unbriefed_seat retired a seat whose opening brief never landed (🎯T433)
//	15:19:50Z start error    …send failed: Agent CLI stalled on startup (startup_stall / splash)
//
// The first two lines claim the agent was handed a brief and did not answer.
// The third says the pane was still drawing its splash screen — there was no
// composer for the brief to land in, and nothing whatever is known about that
// agent. 🎯T190 had landed code at 37515a9 and filed no oracle evidence,
// because the seat that would have filed it was retired two seconds after it
// failed to start.
//
// These oracles drive the REAL path: claudia's own ready-timeout wording goes
// into the sender, agent_send renders it as owner copy ("send failed: %s"),
// and the start path re-reads that string. That round trip is where the class
// used to be lost, so a fixture that injected a pre-classified error would
// have passed against the broken tree.

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/seatstop"
)

// t729StallErr is claudia's error for the specimen: a ready timeout whose
// last frame is Claude Code's splash screen, no composer drawn.
const t729StallErr = "claude not ready (splash): last frame: " +
	"▐▛███▛█   Claude Code v2.1.278\n" +
	"▝▜██████▀  Opus 5 (1M context) with high effort · Claude Max\n" +
	"  ▝▝ ▝▝    ~/work/github.com/squz/ge\n"

// t729Sender stalls on its first stalls sends, then behaves.
type t729Sender struct {
	stalls   int
	attempts int
	sent     []string
}

func (f *t729Sender) Alive() bool { return true }

func (f *t729Sender) Send(text string) error {
	f.attempts++
	if f.attempts <= f.stalls {
		return errors.New(t729StallErr)
	}
	f.sent = append(f.sent, text)
	return nil
}

func (f *t729Sender) Interrupt() error { return nil }

type t729Harness struct {
	s       *Server
	reg     *claudia.Registry
	sender  *t729Sender
	removes []map[string]any
	stops   []map[string]any
}

const t729Seat = "ge-t190-desktop-cook"

// newT729Harness mints the specimen seat with a sender that stalls `stalls`
// times. The retry grace is zeroed so the wait is not in the oracle's way;
// the retry COUNT is left at the product default on purpose — a mutation
// that stops retrying must go red here.
func newT729Harness(t *testing.T, stalls int) *t729Harness {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: t729Seat, WorkDir: dir, SessionID: "c130d2bc",
		Purpose: claudia.PurposeWork, Parent: "ge-po", Provider: "claude",
		TargetID: "T190",
	}); err != nil {
		t.Fatal(err)
	}
	h := &t729Harness{sender: &t729Sender{stalls: stalls}}
	h.reg = reg
	h.s = New(dir, nil, nil)
	h.s.SetRegistry(reg)
	h.s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		if name != t729Seat {
			return nil, false, fmt.Errorf("unknown %s", name)
		}
		return h.sender, false, nil
	})
	grace := time.Duration(0)
	h.s.startStallGrace = &grace
	h.s.eventLogger = func(component, decision string, fields map[string]any) {
		if component != compAgentLifecycle {
			return
		}
		switch decision {
		case fleetlog.Decision:
			h.removes = append(h.removes, fields)
		case "seat_stop":
			h.stops = append(h.stops, fields)
		}
	}
	return h
}

// witnessAfter yields non-positive evidence until the nth observation, then
// the payload-seen positive. A stalled attempt must not be rescued by a
// witness that was already positive.
func (h *t729Harness) witnessAfter(n int) {
	seen := 0
	h.s.SetTurnWitness(func(string, string) turnWatch {
		seen++
		positive := seen >= n
		return func() TurnEvidence {
			if positive {
				return TurnEvidence{
					Observed: true, Durable: true, PayloadSeen: true,
					Detail: "transcript gained a user message carrying this payload",
				}
			}
			return TurnEvidence{
				Observed: true, Durable: true, TranscriptAbsent: true,
				Detail: "no transcript was ever created (the CLI never drew a composer)",
			}
		}
	})
}

// THE MECHANISM THAT WAS MISSING. agenterr.ClassifyText documents itself as
// reading "provider/ACP failure text OR owner-visible copy", and agent_send
// hands the start path nothing but that copy ("send failed: %s"). Every other
// class survives the trip because its copy quotes the raw error; the stall's
// copy replaces it, keeping only the last frame — so "claude not ready
// (splash)" and "ready pattern did not match within" both vanish and the
// class evaporated exactly where the spawn path reads it.
func TestT729StallClassSurvivesItsOwnOwnerCopy(t *testing.T) {
	t.Parallel()
	raw := t729StallErr
	if got := agenterr.ClassifyText(raw); got != agenterr.ClassStartupStall {
		t.Fatalf("fixture: raw claudia error classifies %q, want startup_stall", got)
	}
	copyText := agenterr.OwnerCopy(agenterr.ClassStartupStall, raw)
	if got := agenterr.ClassifyText(copyText); got != agenterr.ClassStartupStall {
		t.Fatalf("owner copy does not classify back: %q → %q\ncopy: %s",
			agenterr.ClassStartupStall, got, copyText)
	}
	// The sub-reason survives too, so a second rendering still says /splash
	// rather than degrading to the generic settings-notice wording.
	if !strings.Contains(agenterr.OwnerCopy(agenterr.ClassStartupStall, copyText), "startup_stall / splash") {
		t.Fatalf("sub-reason lost on round trip: %s",
			agenterr.OwnerCopy(agenterr.ClassStartupStall, copyText))
	}
	// And the whole wrapped shape the start path actually sees.
	wrapped := fmt.Sprintf("start prompt not delivered to %q: send failed: %s", t729Seat, copyText)
	if !startBriefNeverReached(errors.New(wrapped)) {
		t.Fatalf("the specimen's own error is not read as never-ready:\n%s", wrapped)
	}
}

// ACCEPTANCE 1 — a CLI that stalls on startup causes a RETRY. The seat that
// needed a few more seconds gets them, begins its turn, and there is no
// removal of any kind: not unbriefed_seat, not startup_stall.
func TestT729StalledStartIsRetriedNotRetired(t *testing.T) {
	h := newT729Harness(t, 1)
	h.witnessAfter(2)

	if err := h.s.deliverStartPrompt(t729Seat, "Execute 🎯T190."); err != nil {
		t.Fatalf("a stall that clears on retry must deliver: %v", err)
	}
	if h.sender.attempts != 2 {
		t.Fatalf("sender attempts = %d, want 2 (one stall, one retry)", h.sender.attempts)
	}
	if h.reg.Def(t729Seat) == nil {
		t.Fatal("the seat was retired although its brief landed on retry — the 🎯T729 incident")
	}
	if len(h.removes) != 0 {
		t.Fatalf("a recovered stall removed the seat: %+v", h.removes)
	}
	if !h.s.agentHasTurnBegan(t729Seat) {
		t.Fatal("brief landed but the turn was not marked begun")
	}
	// The retry re-sends the SAME composed text. Recomposing would drop the
	// inject-once fleet brief, so the worker would start life without it.
	if len(h.sender.sent) != 1 {
		t.Fatalf("sent payloads = %d, want 1", len(h.sender.sent))
	}
	if !strings.Contains(h.sender.sent[0], "Execute 🎯T190.") {
		t.Fatalf("retry lost the mission text: %q", truncate(h.sender.sent[0], 200))
	}
}

// ACCEPTANCE 2 + HERMETIC 1 — a fixture reporting startup_stall produces NO
// unbriefed_seat retirement in the same pass. The seat still goes (a
// never-ready row would consume the leaf exactly as 🎯T433 describes), but
// under its own reason: "briefed and ignored it" and "never reached it
// because the process never became ready" are different findings.
func TestT729PersistentStallNeverRetiresAsUnbriefed(t *testing.T) {
	h := newT729Harness(t, 99) // never becomes ready
	h.witnessAfter(99)

	err := h.s.deliverStartPrompt(t729Seat, "Execute 🎯T190.")
	if err == nil {
		t.Fatal("a brief that never landed must still fail loudly (🎯T305)")
	}
	if !startBriefNeverReached(err) {
		t.Fatalf("persistent stall not read as never-ready: %v", err)
	}
	if got := classifyStartBriefFailure(err); got != startBriefNeverReady {
		t.Fatalf("fork = %q, want never_ready", got)
	}
	// It was retried before being given up on, and the error says so — a
	// parent told "the seat is retried" must not read that as advice.
	if !strings.Contains(err.Error(), "retried once after startup_stall") {
		t.Fatalf("give-up error does not record the retry: %v", err)
	}

	released, kept := h.s.startBriefFailureTeardown(t729Seat, false, err)
	if kept {
		t.Fatal("a CLI that never became ready is not an in-flight brief")
	}
	if !released {
		t.Fatal("a never-ready seat must not be left registered to consume the leaf (🎯T433)")
	}

	if len(h.removes) != 1 {
		t.Fatalf("removal events = %d, want 1: %+v", len(h.removes), h.removes)
	}
	if got := h.removes[0]["reason"]; got != fleetlog.ReasonStartupStall {
		t.Fatalf("removal reason = %v, want %q — THE SPECIMEN: a launch that "+
			"never completed was journalled as a worker that ignored its brief",
			got, fleetlog.ReasonStartupStall)
	}
	if got := h.removes[0]["reason"]; got == fleetlog.ReasonUnbriefedSeat {
		t.Fatal("unbriefed_seat retirement on a startup_stall")
	}
	if len(h.stops) != 1 {
		t.Fatalf("seat_stop events = %d, want 1: %+v", len(h.stops), h.stops)
	}
	if got := h.stops[0]["source"]; got != string(seatstop.SourceStartupStall) {
		t.Fatalf("seat_stop source = %v, want %q", got, seatstop.SourceStartupStall)
	}
	if got, _ := h.stops[0]["reason"].(string); strings.Contains(got, "proven undelivered") {
		t.Fatalf("the stop still claims the brief was proven undelivered: %q", got)
	}
}

// HERMETIC 2, THE CONTROL — a seat that genuinely came up unbriefed still
// retires as unbriefed_seat. This is the guard 🎯T729 must not delete: a
// clean "sent" to a ready pane whose watch then saw nothing at all is
// 🎯T387's phantom seat, and it is reaped exactly as before.
func TestT729ControlGenuineUnbriefedSeatStillRetires(t *testing.T) {
	h := newT729Harness(t, 0) // the CLI is fine; the agent simply does nothing
	h.witnessAfter(99)        // no user message, no queue record, ever

	err := h.s.deliverStartPrompt(t729Seat, "Execute 🎯T190.")
	if err == nil {
		t.Fatal("sent-with-no-evidence must fail confirmation (🎯T387)")
	}
	if startBriefNeverReached(err) {
		t.Fatalf("a ready pane that ignored its brief must NOT read as never-ready: %v", err)
	}
	if h.sender.attempts != 1 {
		t.Fatalf("sender attempts = %d, want 1 — only a stall is retried", h.sender.attempts)
	}

	released, kept := h.s.startBriefFailureTeardown(t729Seat, false, err)
	if !released || kept {
		t.Fatalf("teardown = released=%v kept=%v, want released", released, kept)
	}
	if h.reg.Def(t729Seat) != nil {
		t.Fatal("phantom seat kept: proven no-brief must still reap (🎯T387)")
	}
	if len(h.removes) != 1 || h.removes[0]["reason"] != fleetlog.ReasonUnbriefedSeat {
		t.Fatalf("control lost its own reason: %+v", h.removes)
	}
	if len(h.stops) != 1 || h.stops[0]["source"] != string(seatstop.SourceUnbriefed) {
		t.Fatalf("control seat_stop = %+v, want source unbriefed", h.stops)
	}
}

// The in-flight fork is untouched by 🎯T729: a held brief is still kept, and
// it is never retried (a re-send stacks a second copy — 🎯T416 / 🎯T518).
func TestT729InFlightBriefIsNeitherRetriedNorReleased(t *testing.T) {
	h := newT729Harness(t, 0)
	err := &briefInFlightError{status: "queued", err: errors.New("held for the next turn boundary")}
	if got := classifyStartBriefFailure(err); got != startBriefInFlight {
		t.Fatalf("fork = %q, want in_flight", got)
	}
	released, kept := h.s.startBriefFailureTeardown(t729Seat, false, err)
	if released || !kept {
		t.Fatalf("teardown = released=%v kept=%v, want kept (🎯T518)", released, kept)
	}
	if len(h.removes) != 0 {
		t.Fatalf("an in-flight brief was removed: %+v", h.removes)
	}
}

// ACCEPTANCE 3 — the parent LEARNS that its spawn died, with the reason. The
// two failures ask opposite things of the reader: "its brief did not land"
// points at the worker, a stall points at the launch and asks only for
// another spawn. ge-po discovering later that 🎯T190 has no worker is the
// outcome this clause exists to prevent.
func TestT729ParentNoticeNamesTheStall(t *testing.T) {
	t.Parallel()
	stallErr := fmt.Sprintf("start prompt not delivered to %q: send failed: %s",
		t729Seat, agenterr.OwnerCopy(agenterr.ClassStartupStall, t729StallErr))
	notice := FormatSpawnFailureNotice("T190", t729Seat, stallErr)
	for _, want := range []string{"🎯T190", t729Seat, "startup_stall", "failed LAUNCH", "Spawn 🎯T190 again"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("parent notice missing %q:\n%s", want, notice)
		}
	}

	// The control notice must NOT claim a stall — a worker that ignored its
	// brief is a different investigation.
	plain := FormatSpawnFailureNotice("T190", t729Seat,
		"start prompt not delivered: turn not begun: send reported sent but the agent did nothing")
	if strings.Contains(plain, "VERDICT startup_stall") {
		t.Fatalf("an ordinary undelivered brief was reported as a stall:\n%s", plain)
	}
}
