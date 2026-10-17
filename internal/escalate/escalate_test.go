// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package escalate

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

var ownerLadder = Ladder{
	{Mode: claudia.DeliverySteer},
	{Mode: claudia.DeliveryInterrupt, After: time.Minute},
}

// 🎯T931: the ladder holds only rungs the seat can run.
func TestT931FitKeepsOnlySupportedRungs(t *testing.T) {
	claude := claudia.ProviderTurnCaps(claudia.ProviderClaude)
	if got := Fit(ownerLadder, claude); got != nil {
		t.Fatalf("Claude Code cannot steer, so a steer-first ladder cannot start: got %+v", got)
	}
	grok := claudia.ProviderTurnCaps(claudia.ProviderGrok)
	if got := Fit(ownerLadder, grok); len(got) != 2 || got[0] != ownerLadder[0] || got[1] != ownerLadder[1] {
		t.Fatalf("a seat that can steer and interrupt keeps the whole ladder: got %+v", got)
	}
	noInterrupt := claudia.TurnCaps{CanSteer: true}
	if got := Fit(ownerLadder, noInterrupt); len(got) != 1 || got[0] != ownerLadder[0] {
		t.Fatalf("a seat that cannot interrupt loses the interrupt rung: got %+v", got)
	}
	submitFirst := Ladder{{Mode: claudia.DeliverySubmit}, {Mode: claudia.DeliveryInterrupt, After: time.Minute}}
	if got := Fit(submitFirst, claude); len(got) != 2 {
		t.Fatalf("a submit-first ladder needs no steer: got %+v", got)
	}
	if got := Fit(nil, grok); got != nil {
		t.Fatalf("no ladder stays no ladder: got %+v", got)
	}
}

// The refusal from 2026-09-29, verbatim as the broker relayed it, is read as
// "never started"; a report that merely quotes it is not. SteerUnsupported
// is the steer half of that answer: idle is not a steer refusal, and a
// rendered mention is not the sentinel.
func TestT931NotStartedReadsTheRelayedRefusal(t *testing.T) {
	for _, c := range []struct {
		err   error
		start bool
		steer bool
	}{
		{errors.New("broker protocol: agent_failed: steer unsupported: claude provider: provider has no steer mechanism (policy queue_until_idle)"), true, true},
		{errors.New("broker protocol: agent_failed: no turn in flight to steer"), true, false},
		{fmt.Errorf("%w: claude provider: provider has no steer mechanism", claudia.ErrSteerUnsupported), true, true},
		{claudia.ErrSteerUnsupported, true, true},
		{claudia.ErrTurnIdle, true, false},
		{errors.New("broker protocol: agent_failed: claude process not running"), false, false},
		{errors.New("broker protocol: agent_failed: provider response quoted: steer unsupported: x"), false, false},
		{errors.New("steer unsupported: but not relayed and not the sentinel"), false, false},
		{fmt.Errorf("relayed: %w", errors.New("broker protocol: agent_failed: steer unsupported")), false, false},
		{nil, false, false},
	} {
		if got := NotStarted(c.err); got != c.start {
			t.Errorf("NotStarted(%v) = %v, want %v", c.err, got, c.start)
		}
		if got := SteerUnsupported(c.err); got != c.steer {
			t.Errorf("SteerUnsupported(%v) = %v, want %v", c.err, got, c.steer)
		}
	}
}

type capsSeat struct{ caps claudia.TurnCaps }

func (s capsSeat) TurnCaps() claudia.TurnCaps { return s.caps }

func TestT931CapsOf(t *testing.T) {
	if _, ok := CapsOf(struct{}{}); ok {
		t.Fatal("a seat without TurnCaps cannot say")
	}
	want := claudia.ProviderTurnCaps(claudia.ProviderClaude)
	if got, ok := CapsOf(capsSeat{want}); !ok || got != want {
		t.Fatalf("CapsOf = %+v, %v", got, ok)
	}
}
