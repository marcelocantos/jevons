// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
)

// cursorRateLimitEvent is the claudia v0.66.0 Cursor/OMP turn-end shape:
// Connect error resource_exhausted → Event.Reason rate_limit, IsError,
// StopReason end_turn. Specimen text matches claudia's
// TestOMPCursorResourceExhaustedIsRateLimit.
func cursorRateLimitEvent() claudia.Event {
	const msg = `Connect error resource_exhausted: Error [details: aiserver.v1.ErrorDetails: {"error":"ERROR_RESOURCE_EXHAUSTED","details":{"title":"Unable to reach the model provider","detail":"We're having trouble connecting to the model provider. This might be temporary - please try again in a moment."},"isExpected":true}]`
	return claudia.Event{
		Type:       "assistant",
		Text:       "provider refused the turn: " + msg,
		IsError:    true,
		StopReason: "end_turn",
		Reason:     agenterr.CauseRateLimit,
	}
}

func t1230Sweep(t *testing.T, s *Server, ledger *IdleNudgeLedger, now time.Time, push FleetRecoverPusher, switchTo *string) []FleetRecoverReport {
	t.Helper()
	return SweepFleetRecover(FleetRecoverSweepArgs{
		Reg:            s.registry,
		Activity:       s.idleActivity,
		Ledger:         ledger,
		ProcessRunning: func(string) bool { return true },
		PromptInFlight: func(string) bool { return false },
		Push:           push,
		SwitchModel: func(_, model string) error {
			if switchTo != nil {
				*switchTo = model
			}
			return nil
		},
		Now:          now,
		OverseerName: "jevons",
	})
}

func t1230Pushed(reps []FleetRecoverReport, name string) (FleetRecoverAction, bool) {
	for _, r := range reps {
		if r.Name == name && r.Delivered {
			return r.Action, true
		}
	}
	return "", false
}

func t1230Action(reps []FleetRecoverReport, name string) FleetRecoverAction {
	for _, r := range reps {
		if r.Name == name {
			return r.Action
		}
	}
	return ""
}

// TestT1230CursorRateLimitGoesThroughLadder drives a typed Cursor
// rate_limit through the real seat terminal path (agentEventSink) into
// ApplyTerminalCause / RateLimitStrikes / SweepFleetRecover.
//
// The event is Cursor-shaped; the seat sits on the claude model ladder so
// persistence is fallback_model rather than another submit into the same
// quota. Owner notification fires only at FallbackAfterStrikes, not on
// the first refusal.
func TestT1230CursorRateLimitGoesThroughLadder(t *testing.T) {
	s, n := t406Server(t)
	s.idleActivity = NewIdleActivityTracker()
	const name = "jv-t1230-cursor-wall"
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, Purpose: claudia.PurposeWork, TargetID: "T1230",
		SessionID: "s1", AutoStart: true,
		Provider: claudia.ProviderClaude, Model: "claude-fable-5",
	}); err != nil {
		t.Fatal(err)
	}
	s.registry = reg
	ledger, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	s.idleActivity.now = func() time.Time { return now }

	var pushed int
	push := func(string, bool, string, string) error {
		pushed++
		return nil
	}
	var switched string
	sink := s.agentEventSink(name)

	sink(cursorRateLimitEvent())
	act := s.idleActivity.Get(name)
	if act.FailureClass != agenterr.ClassRateLimit || act.RateLimitStrikes != 1 || !act.NeedsRecover {
		t.Fatalf("first Cursor refusal: %+v", act)
	}
	n.mu.Lock()
	firstNotices := n.n
	n.mu.Unlock()
	if firstNotices != 0 {
		t.Fatalf("owner notified on first refusal: n=%d kind=%q", firstNotices, n.kind)
	}

	reps := t1230Sweep(t, s, ledger, now, push, &switched)
	if act, ok := t1230Pushed(reps, name); !ok || act != FleetRecoverRebrief {
		t.Fatalf("first recover: pushed=%v action=%s reps=%+v", ok, act, reps)
	}
	if pushed != 1 {
		t.Fatalf("first recover pushes=%d want 1", pushed)
	}

	// Immediate re-sweep must honour the ladder backoff (2 minutes).
	reps = t1230Sweep(t, s, ledger, now.Add(time.Second), push, &switched)
	if act, ok := t1230Pushed(reps, name); ok {
		t.Fatalf("immediate resubmit during backoff: action=%s", act)
	}
	if got := t1230Action(reps, name); got != FleetRecoverSkip {
		t.Fatalf("in-backoff action=%s want skip", got)
	}
	if pushed != 1 {
		t.Fatalf("backoff pushes=%d want 1", pushed)
	}
	need := NextNudgeBackoff(1, DefaultFleetRecoverBackoffs)
	if need != 2*time.Minute {
		t.Fatalf("ladder backoff after first recover=%s want 2m", need)
	}

	sink(cursorRateLimitEvent())
	act = s.idleActivity.Get(name)
	if act.RateLimitStrikes != FallbackAfterStrikes || act.FailureClass != agenterr.ClassRateLimit {
		t.Fatalf("second Cursor refusal did not accumulate strikes: %+v", act)
	}
	n.mu.Lock()
	if n.n != 1 || n.kind != rateLimitPersistedKind {
		t.Fatalf("owner notice at persistence: n=%d kind=%q text=%q", n.n, n.kind, n.text)
	}
	n.mu.Unlock()

	// Still inside backoff: no second submit into the wall.
	reps = t1230Sweep(t, s, ledger, now.Add(time.Second), push, &switched)
	if _, ok := t1230Pushed(reps, name); ok {
		t.Fatalf("resubmit while persisted-and-backing-off: reps=%+v", reps)
	}
	if pushed != 1 {
		t.Fatalf("persisted-in-backoff pushes=%d want 1", pushed)
	}

	later := now.Add(need)
	reps = t1230Sweep(t, s, ledger, later, push, &switched)
	if act, ok := t1230Pushed(reps, name); !ok || act != FleetRecoverFallback {
		t.Fatalf("after ladder: pushed=%v action=%s switched=%q reps=%+v", ok, act, switched, reps)
	}
	if switched != "claude-opus-5" {
		t.Fatalf("fallback model=%q want claude-opus-5", switched)
	}
}

func TestT1230FirstRefusalDoesNotNotifyOwner(t *testing.T) {
	s, n := t406Server(t)
	s.idleActivity = NewIdleActivityTracker()
	s.agentEventSink("w")(cursorRateLimitEvent())
	if got := s.idleActivity.Get("w").RateLimitStrikes; got != 1 {
		t.Fatalf("strikes=%d", got)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.n != 0 {
		t.Fatalf("first refusal notified owner: n=%d kind=%q", n.n, n.kind)
	}
}
