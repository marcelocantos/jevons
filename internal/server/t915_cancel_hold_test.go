// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/converge"
)

// t915Server is a Server whose overseer deliveries are recorded instead of
// typed into a pane. The recorder is safe to read while a drain runs.
type t915Server struct {
	*Server
	mu        sync.Mutex
	delivered []string
}

func newT915Server(t *testing.T) *t915Server {
	t.Helper()
	ts := &t915Server{Server: New("test", t.TempDir())}
	ts.notifySender = func(text string) error {
		ts.mu.Lock()
		ts.delivered = append(ts.delivered, text)
		ts.mu.Unlock()
		return nil
	}
	return ts
}

func (ts *t915Server) got() []string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return slices.Clone(ts.delivered)
}

// seal is the terminal stop of whatever turn is in flight.
func (ts *t915Server) seal() {
	ts.Server.mu.Lock()
	ts.waiting = false
	ts.overseerOwnerTurn = false
	ts.overseerOwnerTurnText = ""
	ts.Server.mu.Unlock()
	ts.drainOverseerNotes()
}

const t915RestartBrief = "[event: daemon-restarted]\n[Who you are — from the fleet registry] NAME: jevons"

// TestT915OwnerCancelHoldsQueuedNoteBehindOwnersNextSend is the J3 specimen
// (sessions 5b6758ac / e11042f2): the post-boot daemon-restarted brief queues
// behind a held owner turn, the owner cancels, and the owner's replacement
// must be the next turn the overseer runs — not the brief.
func TestT915OwnerCancelHoldsQueuedNoteBehindOwnersNextSend(t *testing.T) {
	for _, path := range []struct {
		name   string
		cancel func(s *Server)
	}{
		{"mux", func(s *Server) { s.interruptMuxSeat(s.overseerAgentName()) }},
		{"ws_chat", func(s *Server) {
			if !s.handleChatControlFrame(context.Background(), nil, nil, `{"type":"interrupt"}`) {
				t.Fatal("interrupt frame not consumed as a control frame")
			}
		}},
	} {
		t.Run(path.name, func(t *testing.T) {
			ts := newT915Server(t)
			long := userTurnPrefix + "hold on a tool until released"
			if err := ts.SendToOverseerAs(long, "om-1"); err != nil {
				t.Fatal(err)
			}
			if err := ts.SendToOverseer(t915RestartBrief); err != nil {
				t.Fatal(err)
			}
			if got := ts.got(); len(got) != 1 || got[0] != long {
				t.Fatalf("before cancel delivered=%q, want only the long owner turn", got)
			}

			path.cancel(ts.Server)

			if got := ts.got(); len(got) != 1 {
				t.Fatalf("the cancel ran a queued note as the next turn: delivered=%q", got)
			}

			replacement := userTurnPrefix + "Reply with exactly: ok"
			if err := ts.SendToOverseerAs(replacement, "om-2"); err != nil {
				t.Fatal(err)
			}
			got := ts.got()
			if len(got) != 2 || got[1] != replacement {
				t.Fatalf("owner's next send is not the next turn: delivered=%q", got)
			}

			// The brief is held, not lost: it follows the owner's turn.
			ts.seal()
			got = ts.got()
			if len(got) != 3 || got[2] != t915RestartBrief {
				t.Fatalf("held note did not follow the owner's turn: delivered=%q", got)
			}
		})
	}
}

// TestT915OwnerCancelDropsReinjectedCopyOfCancelledPrompt is the e9620c51
// specimen: an owner-health re-injection of the prompt the owner then
// cancelled sat in the queue and re-ran the hold after the cancel. The
// daemon's copy is dropped; a copy the owner sent themselves is kept.
func TestT915OwnerCancelDropsReinjectedCopyOfCancelledPrompt(t *testing.T) {
	ts := newT915Server(t)
	long := userTurnPrefix + "hold on a tool until released"
	if err := ts.SendToOverseerAs(long, "om-1"); err != nil {
		t.Fatal(err)
	}
	// owner-health requeue_owner_send: same wire text, no message id.
	if err := ts.SendToOverseer(long); err != nil {
		t.Fatal(err)
	}
	// The owner deliberately says it again.
	if err := ts.SendToOverseerAs(long, "om-3"); err != nil {
		t.Fatal(err)
	}

	ts.interruptMuxSeat(ts.overseerAgentName())

	ts.Server.mu.RLock()
	copies := 0
	for _, n := range ts.notifyQueue {
		if n == long {
			copies++
		}
	}
	ts.Server.mu.RUnlock()
	if copies != 1 {
		t.Fatalf("after cancel %d queued copies of the cancelled prompt, want 1 (the owner's own)", copies)
	}

	replacement := userTurnPrefix + "Reply with exactly: ok"
	if err := ts.SendToOverseerAs(replacement, "om-4"); err != nil {
		t.Fatal(err)
	}
	if got := ts.got(); len(got) != 2 || got[1] != replacement {
		t.Fatalf("owner's next send is not the next turn: delivered=%q", got)
	}
	ts.seal()
	if got := ts.got(); len(got) != 3 || got[2] != long {
		t.Fatalf("the owner's own repeat should follow: delivered=%q", got)
	}
	ts.seal()
	if got := ts.got(); len(got) != 3 {
		t.Fatalf("the re-injected copy ran after all: delivered=%q", got)
	}
}

// TestT915OwnerCancelHoldLapses: an owner who cancels and sends nothing does
// not strand the fleet's notes.
func TestT915OwnerCancelHoldLapses(t *testing.T) {
	ts := newT915Server(t)
	ts.ownerCancelHoldWindow = 20 * time.Millisecond
	if err := ts.SendToOverseerAs(userTurnPrefix+"long", "om-1"); err != nil {
		t.Fatal(err)
	}
	if err := ts.SendToOverseer(t915RestartBrief); err != nil {
		t.Fatal(err)
	}
	ts.interruptMuxSeat(ts.overseerAgentName())
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := ts.got(); len(got) == 2 {
			if got[1] != t915RestartBrief {
				t.Fatalf("after the window delivered=%q", got)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("hold never lapsed: delivered=%q", ts.got())
}

// TestT915DaemonReinjectionDoesNotReleaseHold: only the owner's own send
// (it carries a message id) ends the hold.
func TestT915DaemonReinjectionDoesNotReleaseHold(t *testing.T) {
	ts := newT915Server(t)
	if err := ts.SendToOverseerAs(userTurnPrefix+"long", "om-1"); err != nil {
		t.Fatal(err)
	}
	ts.interruptMuxSeat(ts.overseerAgentName())
	if err := ts.SendToOverseer(userTurnPrefix + "earlier words"); err != nil {
		t.Fatal(err)
	}
	if got := ts.got(); len(got) != 1 {
		t.Fatalf("an id-less owner re-injection ran through the hold: delivered=%q", got)
	}
}

// TestT915FleetChewSettlesOnOwnerInterrupt: a Claude seat emits no terminal
// event when interrupted, so the owner's message queued behind an
// interrupted fleet chew waited on a stop that never came. The server ends
// the chew itself and the owner's turn goes at once. A seat that does emit
// its stop is left to it.
func TestT915FleetChewSettlesOnOwnerInterrupt(t *testing.T) {
	for _, tc := range []struct {
		name      string
		settles   bool
		wantOwner bool
	}{
		{"claude_settles", true, true},
		{"grok_waits_for_stop", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := newT915Server(t)
			interrupts := 0
			ts.ownerInterruptSeam = func() (bool, error) {
				interrupts++
				return tc.settles, nil
			}
			if err := ts.SendToOverseer(t915RestartBrief); err != nil {
				t.Fatal(err)
			}
			owner := userTurnPrefix + "Reply with exactly: ok"
			if err := ts.SendToOverseerAs(owner, "om-1"); err != nil {
				t.Fatal(err)
			}
			if interrupts != 1 {
				t.Fatalf("fleet chew interrupted %d times, want 1", interrupts)
			}
			got := ts.got()
			if tc.wantOwner {
				if len(got) != 2 || got[1] != owner {
					t.Fatalf("owner not delivered after the chew settled: delivered=%q", got)
				}
				if !ts.overseerWorkingLevel() {
					t.Fatal("owner turn in flight must light working level")
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("owner sent over an unsettled turn: delivered=%q", got)
			}
			ts.seal()
			if got := ts.got(); len(got) != 2 || got[1] != owner {
				t.Fatalf("owner not delivered on the terminal stop: delivered=%q", got)
			}
		})
	}
}

// TestT915OwnerArrivingMidFleetSendGoesNext is the race in the J3 log
// (18:52:52.646): the owner's send lands while a fleet batch is still being
// handed to a Claude seat. When that hand-over returns, the chew is
// interrupted and settled, and the owner goes next.
func TestT915OwnerArrivingMidFleetSendGoesNext(t *testing.T) {
	ts := newT915Server(t)
	ts.ownerInterruptSeam = func() (bool, error) { return true, nil }
	inSend := make(chan struct{})
	release := make(chan struct{})
	record := ts.notifySender
	ts.notifySender = func(text string) error {
		if text == t915RestartBrief {
			close(inSend)
			<-release
		}
		return record(text)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = ts.SendToOverseer(t915RestartBrief)
	}()
	<-inSend
	owner := userTurnPrefix + "Reply with exactly: ok"
	if err := ts.SendToOverseerAs(owner, "om-1"); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	if got := ts.got(); len(got) != 2 || got[1] != owner {
		t.Fatalf("owner did not follow the interrupted fleet batch: delivered=%q", got)
	}
}

// TestT915RequeueSkipsBatchMidHandover is the other half of e9620c51:
// owner-health re-injected the owner's prompt while the drain was still
// handing that same prompt over, which is how the second copy existed.
func TestT915RequeueSkipsBatchMidHandover(t *testing.T) {
	ts := newT915Server(t)
	text := "hold on a tool until released"
	wire := userTurnPrefix + text
	inSend := make(chan struct{})
	release := make(chan struct{})
	record := ts.notifySender
	ts.notifySender = func(s string) error {
		close(inSend)
		<-release
		return record(s)
	}
	ts.NoteOwnerSend(text, "echo")
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = ts.SendToOverseerAs(wire, "om-1")
	}()
	<-inSend
	err := ownerActuator{ts.Server}.requeueOwnerSend(converge.OwnerGap{Kind: converge.OwnerGapSendNotDelivered}, time.Now())
	close(release)
	<-done
	if !errors.Is(err, converge.ErrOwnerStepNotApplicable) {
		t.Fatalf("requeue during hand-over = %v, want not-applicable", err)
	}
	ts.Server.mu.RLock()
	defer ts.Server.mu.RUnlock()
	if slices.Contains(ts.notifyQueue, wire) {
		t.Fatalf("a second copy of the prompt was queued: %q", ts.notifyQueue)
	}
}

func TestT915InterruptEmitsNoStop(t *testing.T) {
	for _, tc := range []struct {
		p    claudia.Provider
		want bool
	}{
		{claudia.ProviderClaude, true},
		{"", true},
		{claudia.ProviderGrok, false},
		{claudia.ProviderCursor, false},
	} {
		if got := interruptEmitsNoStop(tc.p); got != tc.want {
			t.Errorf("interruptEmitsNoStop(%q) = %v, want %v", tc.p, got, tc.want)
		}
	}
}
