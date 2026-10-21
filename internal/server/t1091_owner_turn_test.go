// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T1091, first half. Isolate jevons-journey-2006972306: the research brief
// (fn-1792452685664-1) began a turn at 10:31:25; the daemon bounced at
// 10:31:38 and J20's park re-minted the overseer at 10:31:39 on the same
// session, whose sidecar kept running that turn. The new server held no turn,
// so J30's owner turn drained as if the seat were idle; the sidecar accepted
// it with no turn of its own (queued behind the research turn), and the owner
// waited six minutes. The owner turn must preempt that turn (🎯T291), and the
// preempted turn's own terminal must not end the owner's.
func TestT1091OwnerTurnQueuedBehindAnUnheldTurnPreemptsIt(t *testing.T) {
	s, _, sent := replacedOverseerServer(t)
	s.ownerQueuedBehindGrace = time.Millisecond
	var interrupts atomic.Int32
	s.ownerInterruptSeam = func() (bool, error) {
		interrupts.Add(1)
		return false, nil // a live seat answers an abort with its turn end
	}

	// The server holds no turn: the research turn began under the previous
	// daemon (or the process this one replaced).
	const owner = userTurnPrefix + "Reply with exactly: react-token"
	if err := s.SendToOverseerAs(owner, "om-j30"); err != nil {
		t.Fatal(err)
	}
	if len(*sent) != 1 || (*sent)[0] != owner {
		t.Fatalf("owner turn handoff = %v", *sent)
	}

	// The provider takes the prompt but opens no turn for it.
	s.DeliverOverseerEvent(claudia.Event{
		Type: "progress", ProgressType: claudia.ProgressPromptAccepted, RequestID: "om-j30",
		Receipt: &claudia.RequestReceipt{RequestID: "om-j30", State: claudia.ReceiptAccepted},
	})
	deadline := time.Now().Add(2 * time.Second)
	for interrupts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if interrupts.Load() != 1 {
		t.Fatalf("owner turn queued behind an unheld turn: interrupts = %d, want 1 (🎯T291 preempt)", interrupts.Load())
	}

	// A fleet note arrives while the owner turn waits.
	if err := s.SendToOverseer("[worker] fleet note"); err != nil {
		t.Fatal(err)
	}

	// The preempted research turn ends. That is not the owner's turn.
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "Retrying the locked note reads.", TurnID: "t-research", RequestID: "fn-research"})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn", TurnID: "t-research", RequestID: "fn-research"})
	s.mu.Lock()
	waiting, ownerTurn := s.waiting, s.overseerOwnerTurn
	s.mu.Unlock()
	if !waiting || !ownerTurn {
		t.Fatalf("after the preempted turn ended: waiting=%v owner_turn=%v, want the owner turn still held", waiting, ownerTurn)
	}
	if len(*sent) != 1 {
		t.Fatalf("handoffs after the preempted turn ended = %v, want the fleet note held behind the owner turn", *sent)
	}

	// The owner's own turn runs and ends; then the fleet note goes.
	s.DeliverOverseerEvent(claudia.Event{Type: "receipt", RequestID: "om-j30",
		Receipt: &claudia.RequestReceipt{RequestID: "om-j30", State: claudia.ReceiptStarted}})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "react-token", TurnID: "t-owner", RequestID: "om-j30"})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn", TurnID: "t-owner", RequestID: "om-j30"})
	s.mu.Lock()
	ownerTurn = s.overseerOwnerTurn
	s.mu.Unlock()
	if ownerTurn {
		t.Fatal("owner turn still held after its own terminal")
	}
	if len(*sent) != 2 || (*sent)[1] != "[worker] fleet note" {
		t.Fatalf("handoffs after the owner turn ended = %v, want the fleet note next", *sent)
	}
}

// The sidecar's acceptance can reach the event path before the drain's
// handoff returns (J30: accepted at 10:31:55.277, drain logged at .289). The
// queued-behind fact must not be lost to that order.
func TestT1091QueuedBehindAcceptanceBeforeTheHandoffReturnsStillPreempts(t *testing.T) {
	s, _, _ := replacedOverseerServer(t)
	s.ownerQueuedBehindGrace = time.Millisecond
	var interrupts atomic.Int32
	s.ownerInterruptSeam = func() (bool, error) { interrupts.Add(1); return false, nil }
	s.overseerSendSeam = func(_, id string) error {
		s.DeliverOverseerEvent(claudia.Event{
			Type: "progress", ProgressType: claudia.ProgressPromptAccepted, RequestID: id,
			Receipt: &claudia.RequestReceipt{RequestID: id, State: claudia.ReceiptAccepted},
		})
		return nil
	}
	if err := s.SendToOverseerAs(userTurnPrefix+"hello", "om-early"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for interrupts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if n := interrupts.Load(); n != 1 {
		t.Fatalf("interrupts = %d, want 1 for an acceptance that beat the handoff's return", n)
	}
}

// An owner prompt the provider starts at once is not queued behind anything:
// no preempt, and its own terminal ends it.
func TestT1091OwnerTurnThatStartsIsNotPreempted(t *testing.T) {
	s, _, _ := replacedOverseerServer(t)
	s.ownerQueuedBehindGrace = time.Millisecond
	var interrupts atomic.Int32
	s.ownerInterruptSeam = func() (bool, error) { interrupts.Add(1); return false, nil }
	if err := s.SendToOverseerAs(userTurnPrefix+"hello", "om-1"); err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{
		Type: "progress", ProgressType: claudia.ProgressPromptAccepted, RequestID: "om-1", TurnID: "t-1",
		Receipt: &claudia.RequestReceipt{RequestID: "om-1", State: claudia.ReceiptAccepted},
	})
	time.Sleep(20 * time.Millisecond)
	if n := interrupts.Load(); n != 0 {
		t.Fatalf("interrupts = %d for an owner turn that began at once", n)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn", TurnID: "t-1", RequestID: "om-1"})
	s.mu.Lock()
	ownerTurn := s.overseerOwnerTurn
	s.mu.Unlock()
	if ownerTurn {
		t.Fatal("owner turn still held after its own terminal")
	}
}

// A queued owner prompt that the provider starts within the grace (it found
// the seat idle after all) is not preempted.
func TestT1091QueuedOwnerTurnThatStartsWithinGraceIsNotPreempted(t *testing.T) {
	s, _, _ := replacedOverseerServer(t)
	s.ownerQueuedBehindGrace = 50 * time.Millisecond
	var interrupts atomic.Int32
	s.ownerInterruptSeam = func() (bool, error) { interrupts.Add(1); return false, nil }
	if err := s.SendToOverseerAs(userTurnPrefix+"hello", "om-2"); err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{
		Type: "progress", ProgressType: claudia.ProgressPromptAccepted, RequestID: "om-2",
		Receipt: &claudia.RequestReceipt{RequestID: "om-2", State: claudia.ReceiptAccepted},
	})
	s.DeliverOverseerEvent(claudia.Event{Type: "receipt", RequestID: "om-2",
		Receipt: &claudia.RequestReceipt{RequestID: "om-2", State: claudia.ReceiptStarted}})
	time.Sleep(150 * time.Millisecond)
	if n := interrupts.Load(); n != 0 {
		t.Fatalf("interrupts = %d for an owner turn that started within the grace", n)
	}
}

// A queued owner request the provider closes unrun (its receipt cancelled)
// is reported to the owner with Resend, not left to vanish with the turn it
// waited behind.
func TestT1091QueuedOwnerTurnClosedUnrunIsReported(t *testing.T) {
	s, _, sent := replacedOverseerServer(t)
	s.ownerQueuedBehindGrace = time.Hour
	lines := make(chan string, 32)
	s.chatListeners = append(s.chatListeners, lines)
	if err := s.SendToOverseerAs(userTurnPrefix+"hello", "om-drop"); err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{
		Type: "progress", ProgressType: claudia.ProgressPromptAccepted, RequestID: "om-drop",
		Receipt: &claudia.RequestReceipt{RequestID: "om-drop", State: claudia.ReceiptAccepted},
	})
	sendErrorFrames(lines)
	s.DeliverOverseerEvent(claudia.Event{Type: "receipt", RequestID: "om-drop",
		Receipt: &claudia.RequestReceipt{RequestID: "om-drop", State: claudia.ReceiptCancelled}})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn", TurnID: "t-other", RequestID: "fn-other"})
	var reported bool
	for _, f := range sendErrorFrames(lines) {
		if f["msg_id"] == "om-drop" && f["state"] == "undelivered" {
			reported = true
		}
	}
	if !reported {
		t.Fatal("a queued owner turn closed unrun was not reported")
	}
	s.mu.Lock()
	ownerTurn := s.overseerOwnerTurn
	s.mu.Unlock()
	if ownerTurn {
		t.Fatal("owner turn still held after its request closed unrun and the turn ahead ended")
	}
	if err := s.ResendOwnerMessage("om-drop"); err != nil || len(*sent) != 2 {
		t.Fatalf("Resend: err=%v handoffs=%v", err, *sent)
	}
}

func sendErrorFrames(lines chan string) []map[string]any {
	var out []map[string]any
	for {
		select {
		case l := <-lines:
			var m map[string]any
			if json.Unmarshal([]byte(l), &m) == nil && m["type"] == "send_error" {
				out = append(out, m)
			}
		default:
			return out
		}
	}
}

// 🎯T1091, second half. Full suite on d1d1baa7: owner turn
// om-1792456240499-12 was in flight at 11:30:41 when J20's park stopped the
// overseer. Its receipt had settled, the seat was stopped before it answered,
// and the re-attach settled the turn: nothing re-offered it and nothing told
// the owner. The owner must be told, with Resend, and nothing may resend it
// unasked (🎯T416).
func TestT1091OwnerTurnLostToAReplacedOverseerIsReportedWithResend(t *testing.T) {
	s, _, sent := replacedOverseerServer(t)
	lines := make(chan string, 32)
	s.chatListeners = append(s.chatListeners, lines)
	s.mu.Lock()
	s.proc = claudia.NewStubAgent(func(string) error { return nil }) // the process the park stops
	s.mu.Unlock()

	const owner = userTurnPrefix + "what is the fleet doing?"
	if err := s.SendToOverseerAs(owner, "om-lost"); err != nil {
		t.Fatal(err)
	}
	if len(*sent) != 1 {
		t.Fatalf("owner handoff = %v", *sent)
	}
	sendErrorFrames(lines) // drop anything the handoff said

	// The park stopped that process; the recovery sweep's replacement attaches.
	if err := s.cockpitAttach(&cockpitState{}); err != nil {
		t.Fatal(err)
	}
	if len(*sent) != 1 {
		t.Fatalf("handoffs after re-attach = %v, want the owner turn not resent unasked (🎯T416)", *sent)
	}
	var report map[string]any
	for _, f := range sendErrorFrames(lines) {
		if f["msg_id"] == "om-lost" && f["state"] == "undelivered" {
			report = f
		}
	}
	if report == nil {
		t.Fatal("no undelivered frame for the owner turn the replaced overseer never answered")
	}
	if r, _ := report["reason"].(string); r == "" {
		t.Fatalf("report has no reason: %v", report)
	}

	// The owner presses Resend: it goes once, under the same id.
	if err := s.ResendOwnerMessage("om-lost"); err != nil {
		t.Fatalf("resend: %v", err)
	}
	if len(*sent) != 2 || (*sent)[1] != owner {
		t.Fatalf("handoffs after Resend = %v, want the owner turn once more", *sent)
	}
	if err := s.ResendOwnerMessage("om-lost"); err == nil {
		t.Fatal("a second Resend of a message already resent was accepted")
	}
	if len(*sent) != 2 {
		t.Fatalf("handoffs after a second Resend = %v", *sent)
	}
}

// When the replaced overseer's turn answers after all (the sidecar kept it),
// the report flips to delivered and Resend no longer offers a duplicate.
func TestT1091LateAnswerWithdrawsTheResendOffer(t *testing.T) {
	s, _, sent := replacedOverseerServer(t)
	lines := make(chan string, 32)
	s.chatListeners = append(s.chatListeners, lines)
	s.mu.Lock()
	s.proc = claudia.NewStubAgent(func(string) error { return nil })
	s.mu.Unlock()
	if err := s.SendToOverseerAs(userTurnPrefix+"status?", "om-late"); err != nil {
		t.Fatal(err)
	}
	if err := s.cockpitAttach(&cockpitState{}); err != nil {
		t.Fatal(err)
	}
	sendErrorFrames(lines)
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "all quiet", TurnID: "t-old", RequestID: "om-late"})
	var flipped bool
	for _, f := range sendErrorFrames(lines) {
		if f["msg_id"] == "om-late" && f["state"] == "delivered" {
			flipped = true
		}
	}
	if !flipped {
		t.Fatal("a late answer did not flip the unanswered report")
	}
	if err := s.ResendOwnerMessage("om-late"); err == nil {
		t.Fatal("Resend accepted for a message the overseer answered")
	}
	if len(*sent) != 1 {
		t.Fatalf("handoffs = %v", *sent)
	}
}
