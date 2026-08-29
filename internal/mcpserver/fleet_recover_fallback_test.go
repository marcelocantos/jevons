// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
)

// rateLimited is a seat whose last terminal failed with a quota refusal
// and which is otherwise a healthy recover candidate.
func rateLimited(strikes int, fallback string) FleetRecoverObs {
	return FleetRecoverObs{
		Name:             "jv-t999-seat",
		Purpose:          "work",
		ProcessRunning:   true,
		HasOpenMission:   true,
		NeedsRecover:     true,
		FailureClass:     agenterr.ClassRateLimit,
		Model:            "claude-fable-5",
		RateLimitStrikes: strikes,
		FallbackModel:    fallback,
	}
}

func TestRepressureFirstThenSwitchTheModel(t *testing.T) {
	// One refusal may be a busy backend; re-pressure is still right.
	act, reason := ClassifyFleetRecover(rateLimited(1, "claude-opus-5"))
	if act != FleetRecoverRebrief {
		t.Fatalf("first strike: got %s (%s), want rebrief", act, reason)
	}
	// By the second, re-pressure has demonstrably not worked. This is the
	// 2026-08-30 loop: rebrief → rate_limit → rebrief, for hours.
	act, reason = ClassifyFleetRecover(rateLimited(FallbackAfterStrikes, "claude-opus-5"))
	if act != FleetRecoverFallback {
		t.Fatalf("second strike: got %s (%s), want fallback_model", act, reason)
	}
	if reason != "rate_limit_exhausted:claude-fable-5→claude-opus-5" {
		t.Fatalf("reason must name both models, got %q", reason)
	}
}

func TestNoRungLeftKeepsRebriefingRatherThanInventingAModel(t *testing.T) {
	// Bottom of the ladder: there is nothing to switch to, so the seat
	// keeps the transient-retry behaviour instead of being launched
	// against an empty model id.
	act, _ := ClassifyFleetRecover(rateLimited(9, ""))
	if act != FleetRecoverRebrief {
		t.Fatalf("no fallback: got %s, want rebrief", act)
	}
}

func TestOtherFailuresNeverSwitchTheModel(t *testing.T) {
	// A backend outage is not the model's fault; swapping would spend
	// capability to fix something a retry fixes.
	o := rateLimited(9, "claude-opus-5")
	o.FailureClass = agenterr.ClassBackendUnavailable
	if act, _ := ClassifyFleetRecover(o); act != FleetRecoverRebrief {
		t.Fatalf("backend_unavailable: got %s, want rebrief", act)
	}
	// Auth still fails closed.
	o.FailureClass = agenterr.ClassAuth
	if act, _ := ClassifyFleetRecover(o); act != FleetRecoverSkip {
		t.Fatalf("auth: got %s, want skip", act)
	}
}

func TestStuckBusyOutranksTheLadder(t *testing.T) {
	// A seat mid-turn is unstuck, not re-modelled: the strikes describe
	// an older terminal, and interrupting is the cheaper move.
	o := rateLimited(9, "claude-opus-5")
	o.PromptInFlight = true
	o.SinceProgress = 5 * time.Minute
	if act, _ := ClassifyFleetRecover(o); act != FleetRecoverUnstick {
		t.Fatalf("in-flight: got %s, want unstick", act)
	}
}

func TestStrikesCountRefusalsNotDeliveries(t *testing.T) {
	// The trap this exists to avoid: a delivered re-brief looks like
	// progress, but delivering to an exhausted model is precisely the
	// move that fails. Only a terminal that says something else clears.
	tr := &IdleActivityTracker{}
	tr.NoteTerminalOutcome("seat", "Error: rate limit exceeded for this model")
	tr.NoteTerminalOutcome("seat", "Error: rate limit exceeded for this model")
	if got := tr.Get("seat").RateLimitStrikes; got != 2 {
		t.Fatalf("two refusals = %d strikes, want 2", got)
	}
	tr.ClearRecover("seat", time.Now()) // a re-brief was delivered
	if got := tr.Get("seat").RateLimitStrikes; got != 2 {
		t.Fatalf("delivery must not clear strikes, got %d", got)
	}
	tr.NoteModelSwitched("seat", time.Now())
	if got := tr.Get("seat").RateLimitStrikes; got != 0 {
		t.Fatalf("a model switch resets strikes, got %d", got)
	}
}

func TestFallbackWithoutASwitcherIsReportedNotSilent(t *testing.T) {
	// A misconfigured daemon must say so rather than quietly degrade to
	// the loop this target removes.
	rep := switchSeatModel(claudia.AgentDef{Name: "jv-t999-seat"}, FleetRecoverSweepArgs{},
		rateLimited(2, "claude-opus-5"), FleetRecoverReport{Name: "jv-t999-seat"},
		"rate_limit_exhausted", time.Now())
	if rep.Delivered || rep.Error != "no_model_switcher" {
		t.Fatalf("got delivered=%v err=%q", rep.Delivered, rep.Error)
	}
}
