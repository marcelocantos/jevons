// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/delivery"
)

// poQueueSender is the seat shape 🎯T711 is about: a Claude-shaped process
// that ACCEPTS text while its own turn is running — into its client queue,
// returning nil — instead of bouncing it with "prompt already in flight".
// Every PO is this shape, and is mid-turn nearly all the time.
//
// fakeSender (agent_send_test.go) is the other shape, the bouncing Grok ACP
// one. The old interrupt path only ever reached proc.Interrupt() through a
// bounce, so it worked on that fake and on nothing the owner actually talks to.
type poQueueSender struct {
	alive    bool
	turnOpen bool

	// ops is the order the process saw things happen in — the whole point of
	// this target is that "interrupt" comes before "send".
	ops        []string
	sent       []string
	openAtSend []bool
	interrupts int

	// interruptErr makes the cancel fail, for the 🎯T424 arm.
	interruptErr error
}

func (f *poQueueSender) Alive() bool { return f.alive }

func (f *poQueueSender) Send(text string) error {
	f.ops = append(f.ops, "send")
	f.openAtSend = append(f.openAtSend, f.turnOpen)
	f.sent = append(f.sent, text)
	return nil
}

func (f *poQueueSender) Interrupt() error {
	f.ops = append(f.ops, "interrupt")
	f.interrupts++
	if f.interruptErr != nil {
		return f.interruptErr
	}
	f.turnOpen = false
	return nil
}

// TestT711InterruptCutsTheTurnBeforeTheTextIsOffered is the target itself:
// mode=interrupt acts on the process first, so the payload is handed to a seat
// whose turn has already been cut. RED before the fix, where the cancel was
// conditional on a bounce this seat never produces: interrupts stayed 0, the
// text went into the CLI's queue behind the live turn, and the caller was told
// queued / client_queue.
func TestT711InterruptCutsTheTurnBeforeTheTextIsOffered(t *testing.T) {
	s := &Server{}
	const po = "ge-po"
	fs := &poQueueSender{alive: true, turnOpen: true}
	// The daemon knows this PO is working — phase=working in the specimen.
	s.noteTurnInFlight(po)

	res, err := deliverObservedToSenderMode(s, po, "owner doctrine", delivery.ModeInterrupt, fs, false, confirmHere)
	if err != nil {
		t.Fatalf("interrupt send: %v", err)
	}
	if fs.interrupts != 1 {
		t.Fatalf("interrupts=%d, want 1 — mode=interrupt never cut the turn on the process", fs.interrupts)
	}
	if got := strings.Join(fs.ops, ","); got != "interrupt,send" {
		t.Fatalf("process saw %q, want \"interrupt,send\" — the cut must precede the offer", got)
	}
	if len(fs.openAtSend) != 1 || fs.openAtSend[0] {
		t.Fatalf("the text was offered while the turn was still open (openAtSend=%v)", fs.openAtSend)
	}
	if res.Status == "queued" {
		t.Fatalf("status=queued: interrupt degraded to a queue (%s)", res.Message)
	}
	if res.Mechanism == delivery.MechanismClientQueue {
		t.Fatalf("mechanism=%s: the turn ran on and the message waited behind it", res.Mechanism)
	}
	if res.Mechanism != delivery.MechanismSessionCancelPrompt {
		t.Fatalf("mechanism=%q, want %q", res.Mechanism, delivery.MechanismSessionCancelPrompt)
	}
	if res.Queued != 0 {
		t.Fatalf("queued=%d, want 0 — nothing should be sitting in the daemon backlog", res.Queued)
	}
	if len(fs.sent) != 1 || fs.sent[0] != "owner doctrine" {
		t.Fatalf("sent=%v, want the payload delivered once", fs.sent)
	}
}

// TestT711SubmitStillQueuesBehindTheTurn is the control: the fix must not turn
// ordinary sends into cancels. A submit to the same working seat still takes
// the daemon's own queue and never touches the process.
func TestT711SubmitStillQueuesBehindTheTurn(t *testing.T) {
	s := &Server{}
	const po = "ge-po"
	fs := &poQueueSender{alive: true, turnOpen: true}
	s.noteTurnInFlight(po)

	res, err := deliverObservedToSenderMode(s, po, "routine note", delivery.ModeSubmit, fs, false, confirmHere)
	if err != nil {
		t.Fatalf("submit send: %v", err)
	}
	if fs.interrupts != 0 {
		t.Fatalf("interrupts=%d, want 0 — a plain submit must not cut a turn", fs.interrupts)
	}
	if len(fs.sent) != 0 {
		t.Fatalf("sent=%v, want nothing offered to a seat known to be in flight", fs.sent)
	}
	if res.Status != "queued" || res.Mechanism != delivery.MechanismClientQueue {
		t.Fatalf("status=%q mechanism=%q, want queued/%s", res.Status, res.Mechanism, delivery.MechanismClientQueue)
	}
}

// TestT711InterruptFailureIsStillNotAQueue keeps 🎯T424 intact across the
// reorder: when the daemon knows a turn is in flight and the cancel fails, the
// caller gets an error naming it — never a cheerful enqueue.
func TestT711InterruptFailureIsStillNotAQueue(t *testing.T) {
	s := &Server{}
	const po = "ge-po"
	fs := &poQueueSender{alive: true, turnOpen: true, interruptErr: fmt.Errorf("session/cancel refused")}
	s.noteTurnInFlight(po)

	res, err := deliverObservedToSenderMode(s, po, "owner doctrine", delivery.ModeInterrupt, fs, false, confirmHere)
	if err == nil {
		t.Fatalf("want an error, got status=%q mechanism=%q", res.Status, res.Mechanism)
	}
	if !strings.Contains(err.Error(), "🎯T424") {
		t.Fatalf("error should name 🎯T424: %v", err)
	}
	if len(fs.sent) != 0 {
		t.Fatalf("sent=%v, want nothing delivered after a failed cut", fs.sent)
	}
	if res.Queued != 0 {
		t.Fatalf("queued=%d, want 0 — a failed interrupt must not become a queue", res.Queued)
	}
}

// TestT711RefusedCancelOnAnUnobservedSeatStillDelivers is the other side of
// that rule. With no observed turn, a refused cancel may mean only that there
// was nothing to cancel, and refusing the send there would break interrupt as
// a safe default on any seat this daemon has not watched.
func TestT711RefusedCancelOnAnUnobservedSeatStillDelivers(t *testing.T) {
	s := &Server{}
	const po = "ge-po"
	fs := &poQueueSender{alive: true, interruptErr: fmt.Errorf("no prompt in flight")}
	if got := s.flightState(po); got != FlightUnknown {
		t.Fatalf("flight=%v, want unknown for this fixture", got)
	}

	res, err := deliverObservedToSenderMode(s, po, "owner doctrine", delivery.ModeInterrupt, fs, false, confirmHere)
	if err != nil {
		t.Fatalf("a refused cancel on an unobserved seat must not fail the send: %v", err)
	}
	if len(fs.sent) != 1 {
		t.Fatalf("sent=%v, want the payload delivered once", fs.sent)
	}
	if res.Mechanism == delivery.MechanismClientQueue {
		t.Fatalf("mechanism=%s, want the text offered rather than held", res.Mechanism)
	}
}
