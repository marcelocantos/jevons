// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sticky

import (
	"errors"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/converge"
)

// The surface must satisfy the ladder's sink, or the top rung has nowhere to
// go. Compile-time, because a runtime discovery here means a shipped daemon
// with an unreachable human rung.
var _ converge.HumanSink = (*Sticky)(nil)

type call struct {
	op    string // show | withdraw
	group string
	body  string
}

type fakeBackend struct {
	calls   []call
	showErr error
	remErr  error
}

func (f *fakeBackend) Show(group, title, message string) error {
	if f.showErr != nil {
		return f.showErr
	}
	f.calls = append(f.calls, call{op: "show", group: group, body: message})
	return nil
}

func (f *fakeBackend) Withdraw(group string) error {
	if f.remErr != nil {
		return f.remErr
	}
	f.calls = append(f.calls, call{op: "withdraw", group: group})
	return nil
}

func (f *fakeBackend) ops() []string {
	var out []string
	for _, c := range f.calls {
		out = append(out, c.op)
	}
	return out
}

// A raise reaches the OS and is recorded as lit; the clear withdraws exactly
// that agent's group. This is the 🎯T319 auto-clear path end to end at the
// surface: ActClearHuman → the alert leaves the screen, no ack involved.
func TestRaiseThenClearWithdrawsTheSameGroup(t *testing.T) {
	be := &fakeBackend{}
	s := New(be)

	if err := s.RaiseImpatienceAlert("jv-x", "jv-x is stuck for 45m"); err != nil {
		t.Fatalf("raise: %v", err)
	}
	if got := s.Lit(); len(got) != 1 || got[0] != "jv-x" {
		t.Fatalf("want jv-x lit, got %v", got)
	}
	if err := s.ClearImpatienceAlert("jv-x"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := s.Lit(); len(got) != 0 {
		t.Fatalf("want nothing lit after clear, got %v", got)
	}

	if got := be.ops(); len(got) != 2 || got[0] != "show" || got[1] != "withdraw" {
		t.Fatalf("want show then withdraw, got %v", got)
	}
	if be.calls[0].group != be.calls[1].group {
		t.Fatalf("withdraw hit a different group: %q vs %q", be.calls[0].group, be.calls[1].group)
	}
	if !strings.Contains(be.calls[0].group, "jv-x") {
		t.Fatalf("group does not identify the agent: %q", be.calls[0].group)
	}
}

// Re-fires of the human rung must land on one alert, not a growing pile. The
// group is stable across re-fires (the OS replaces), and a byte-identical
// repeat does not even reach the OS.
func TestRepeatRaiseCoalescesAndKeepsOneGroup(t *testing.T) {
	be := &fakeBackend{}
	s := New(be)
	const same = "jv-x is stuck for 45m"

	for i := 0; i < 3; i++ {
		if err := s.RaiseImpatienceAlert("jv-x", same); err != nil {
			t.Fatalf("raise %d: %v", i, err)
		}
	}
	if len(be.calls) != 1 {
		t.Fatalf("identical repeats should not re-show: %d calls", len(be.calls))
	}

	// A grown dwell is new information and must re-show — on the same group.
	if err := s.RaiseImpatienceAlert("jv-x", "jv-x is stuck for 1h15m"); err != nil {
		t.Fatalf("raise updated: %v", err)
	}
	if len(be.calls) != 2 {
		t.Fatalf("changed text should re-show: %d calls", len(be.calls))
	}
	if be.calls[0].group != be.calls[1].group {
		t.Fatalf("re-fire changed group: %q vs %q", be.calls[0].group, be.calls[1].group)
	}
	if got := s.Lit(); len(got) != 1 {
		t.Fatalf("want a single lit agent, got %v", got)
	}
}

// Clearing an agent that was never lit must not touch the OS. The ladder only
// emits ActClearHuman for agents it lit, but a daemon restart can leave the
// two disagreeing, and a blind withdraw would be a claim about a screen we do
// not own.
func TestClearUnlitAgentDoesNotTouchTheOS(t *testing.T) {
	be := &fakeBackend{}
	s := New(be)
	if err := s.ClearImpatienceAlert("jv-never"); err != nil {
		t.Fatalf("clear unlit should be a no-op, got %v", err)
	}
	if len(be.calls) != 0 {
		t.Fatalf("clear unlit reached the OS: %v", be.calls)
	}
}

// A failed raise must not be recorded as lit: the owner saw nothing, so the
// next tick has to try again rather than believe an alert that never posted.
func TestFailedRaiseLeavesAgentUnlitSoTheNextTickRetries(t *testing.T) {
	boom := errors.New("notifier exploded")
	be := &fakeBackend{showErr: boom}
	s := New(be)

	err := s.RaiseImpatienceAlert("jv-x", "stuck")
	if err == nil {
		t.Fatal("want the backend error surfaced, got nil")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("want wrapped backend error, got %v", err)
	}
	if got := s.Lit(); len(got) != 0 {
		t.Fatalf("failed raise recorded as lit: %v", got)
	}

	be.showErr = nil
	if err := s.RaiseImpatienceAlert("jv-x", "stuck"); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	if got := s.Lit(); len(got) != 1 {
		t.Fatalf("retry did not light the alert: %v", got)
	}
}

// A failed withdraw keeps the agent lit. Dropping it would strand a "stuck"
// alert on the owner's screen with nothing left that knows to remove it.
func TestFailedClearKeepsAgentLit(t *testing.T) {
	be := &fakeBackend{}
	s := New(be)
	if err := s.RaiseImpatienceAlert("jv-x", "stuck"); err != nil {
		t.Fatalf("raise: %v", err)
	}
	be.remErr = errors.New("remove failed")
	if err := s.ClearImpatienceAlert("jv-x"); err == nil {
		t.Fatal("want the withdraw error surfaced, got nil")
	}
	if got := s.Lit(); len(got) != 1 {
		t.Fatalf("failed withdraw dropped tracking: %v", got)
	}
}

// Distinct agents get distinct groups, and clearing one leaves the other up.
func TestAgentsAreIndependentAlerts(t *testing.T) {
	be := &fakeBackend{}
	s := New(be)
	for _, a := range []string{"jv-x", "jv-y"} {
		if err := s.RaiseImpatienceAlert(a, a+" is stuck"); err != nil {
			t.Fatalf("raise %s: %v", a, err)
		}
	}
	if Group("jv-x") == Group("jv-y") {
		t.Fatal("agents share a notification group; one clear would wipe both")
	}
	if err := s.ClearImpatienceAlert("jv-x"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := s.Lit(); len(got) != 1 || got[0] != "jv-y" {
		t.Fatalf("want only jv-y still lit, got %v", got)
	}
}

// Shutdown must not strand alerts: ClearAll withdraws everything raised.
func TestClearAllWithdrawsEveryLitAlert(t *testing.T) {
	be := &fakeBackend{}
	s := New(be)
	for _, a := range []string{"jv-x", "jv-y", "jv-z"} {
		if err := s.RaiseImpatienceAlert(a, "stuck"); err != nil {
			t.Fatalf("raise %s: %v", a, err)
		}
	}
	if errs := s.ClearAll(); len(errs) != 0 {
		t.Fatalf("clear all: %v", errs)
	}
	if got := s.Lit(); len(got) != 0 {
		t.Fatalf("alerts survived shutdown: %v", got)
	}
	withdrawn := 0
	for _, c := range be.calls {
		if c.op == "withdraw" {
			withdrawn++
		}
	}
	if withdrawn != 3 {
		t.Fatalf("want 3 withdrawals, got %d", withdrawn)
	}
}

// An unwired surface reports rather than swallowing: a nil *Sticky is what a
// daemon holds when the OS backend could not be built, and the alert it is
// handed must not vanish quietly.
func TestNilSurfaceReportsInsteadOfSwallowing(t *testing.T) {
	var s *Sticky
	if err := s.RaiseImpatienceAlert("jv-x", "stuck"); err == nil {
		t.Fatal("nil surface swallowed a raise")
	}
	if err := s.ClearImpatienceAlert("jv-x"); err == nil {
		t.Fatal("nil surface swallowed a clear")
	}
	if New(nil) != nil {
		t.Fatal("New(nil) must yield an unwired surface, not a live one")
	}
}
