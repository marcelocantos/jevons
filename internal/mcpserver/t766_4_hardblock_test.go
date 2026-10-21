// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// A terminal assistant event reaches RefusalHold and blocked_provider only
// when Event.Reason is a spend cause. The same sentence does neither.
func TestT7664TerminalSpendCauseHardBlocks(t *testing.T) {
	s, _ := t406Server(t)
	s.idleActivity = NewIdleActivityTracker()
	spend := "You've hit your monthly spend limit. Run /usage-credits to manage your limit."
	s.agentEventSink("w")(claudia.Event{Type: "assistant", Text: spend, StopReason: "end_turn"})
	if act := s.idleActivity.Get("w"); act.RefusalHold || act.FailureClass != agenterr.ClassNone {
		t.Fatalf("prose latched: %+v", act)
	}
	if got := s.fleetIntent().FleetState(); got != fleetintent.Working {
		t.Fatalf("prose fleet=%s", got)
	}

	s.agentEventSink("w")(claudia.Event{
		Type: "assistant", Text: spend, StopReason: "end_turn", Reason: agenterr.CauseRateLimit,
	})
	if act := s.idleActivity.Get("w"); act.RefusalHold || act.RateLimitStrikes != 1 || act.FailureClass != agenterr.ClassRateLimit {
		t.Fatalf("rate-limit cause: %+v", act)
	}
	if got := s.fleetIntent().FleetState(); got != fleetintent.Working {
		t.Fatalf("rate_limit fleet=%s", got)
	}

	s.agentEventSink("w")(claudia.Event{
		Type: "assistant", Text: spend, StopReason: "end_turn", Reason: agenterr.CauseSpendWall,
	})
	act := s.idleActivity.Get("w")
	if !act.RefusalHold || act.FailureClass != agenterr.ClassSpendWall {
		t.Fatalf("spend cause: %+v", act)
	}
	if act.NeedsRecover {
		t.Fatal("spend wall must not rebrief")
	}
	if got := s.fleetIntent().FleetState(); got != fleetintent.BlockedProvider {
		t.Fatalf("spend cause fleet=%s", got)
	}
}

// Published TaskEvent errors are sentences. The fold handleJwork runs does
// not classify them, and the fleet stays working.
func TestT7664JworkErrorSentenceDoesNotHardBlock(t *testing.T) {
	s, _ := t406Server(t)
	events := make(chan claudia.TaskEvent, 3)
	events <- claudia.TaskEvent{Type: claudia.TaskEventError, ErrorMsg: "You've hit your monthly spend limit"}
	events <- claudia.TaskEvent{Type: claudia.TaskEventError, ErrorMsg: "Error: you've hit your monthly spend limit"}
	events <- claudia.TaskEvent{Type: claudia.TaskEventError, ErrorMsg: "Internal error"}
	close(events)
	fold := s.foldJworkEvents("w", 0, &heartbeatTracker{}, events)
	if fold.errClass.IsFailure() {
		t.Fatalf("published error sentence classified as %s", fold.errClass)
	}
	failClass, _ := s.settleJworkFailure("w", fold, "Internal error")
	if failClass.IsFailure() {
		t.Fatalf("settle classified the sentence as %s", failClass)
	}
	if got := s.fleetIntent().FleetState(); got != fleetintent.Working {
		t.Fatalf("fleet=%s", got)
	}
}
