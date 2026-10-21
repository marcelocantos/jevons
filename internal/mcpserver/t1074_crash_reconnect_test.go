// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/claudiawire"
)

// TestT1074StalePredecessorDoesNotTakeNextRequest drives drain into the real
// seat send. A late event from the turn that already stopped is published
// inside the next send, before that send's acceptance. It keeps the first id.
func TestT1074StalePredecessorDoesNotTakeNextRequest(t *testing.T) {
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	dir := t.TempDir()
	const name = "jv-t1074-pred"
	const session = "gen-pred"
	const firstID = "host-A"
	const secondID = "host-B"
	const text = "same text"

	s := &Server{}
	s.SetSendQueueDir(dir)
	s.SetTurnWitness(func(_, _ string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, Durable: true, PayloadSeen: true}
		}
	})
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: session, Materialized: true, Provider: claudia.ProviderGrok,
	}); err != nil {
		t.Fatal(err)
	}
	s.registry = reg

	var seat *claudia.Agent
	var phase int
	seat, err = claudia.StartStub(context.Background(), claudia.Config{
		Name: name, SessionID: session, Provider: claudia.ProviderGrok, Model: "grok-3",
		WorkDir: dir, TermLogPath: "-",
	}, &claudia.StubAgentOps{
		Send: func(string) error {
			phase++
			if phase == 1 {
				seat.PublishEvent(claudia.Event{Type: "progress", TurnID: "1", ProgressType: claudia.ProgressPromptAccepted})
				seat.PublishEvent(claudia.Event{Type: "assistant", TurnID: "1", Text: "done", StopReason: "end_turn"})
				return nil
			}
			seat.PublishEvent(claudia.Event{Type: "assistant", TurnID: "1", Text: "late predecessor"})
			seat.PublishEvent(claudia.Event{Type: "progress", TurnID: "2", ProgressType: claudia.ProgressPromptAccepted})
			seat.PublishEvent(claudia.Event{Type: "assistant", TurnID: "2", Text: "second", StopReason: "end_turn"})
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { seat.Stop() })
	if seat.SessionID() != session {
		t.Fatalf("stub reminted session %s", seat.SessionID())
	}
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return seat, false, nil })
	receiptJournal(t, s, dir)
	lane := bindReceiptLane(t, s, dir)

	var mu sync.Mutex
	var got []claudia.Event
	seat.SubscribeEvents(func(ev claudia.Event) {
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
		if ev.IsTerminalStop() && ev.RequestID != "" {
			lane.publish(name, session, ev.RequestID, claudiawire.ReceiptCompleted, "", "", []string{ev.TurnID})
		}
	})

	if _, _, err := s.sendQueue().AppendWithRequestID(name, text, firstID, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.drainAgentSendQueue(name)
	if _, _, err := s.sendQueue().AppendWithRequestID(name, text, secondID, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.drainAgentSendQueue(name)
	if phase != 2 {
		t.Fatalf("sends=%d", phase)
	}
	mu.Lock()
	events := append([]claudia.Event(nil), got...)
	mu.Unlock()
	var late, acceptedB, terminalA, terminalB claudia.Event
	var sawLate, sawAcceptedB, sawA, sawB bool
	for _, ev := range events {
		switch {
		case ev.TurnID == "1" && ev.Text == "late predecessor":
			late, sawLate = ev, true
		case ev.TurnID == "2" && ev.ProgressType == claudia.ProgressPromptAccepted:
			acceptedB, sawAcceptedB = ev, true
		case ev.TurnID == "1" && ev.IsTerminalStop():
			terminalA, sawA = ev, true
		case ev.TurnID == "2" && ev.IsTerminalStop():
			terminalB, sawB = ev, true
		}
	}
	if !sawA || !sawB || !sawLate || !sawAcceptedB {
		t.Fatalf("missing events: %+v", events)
	}
	if terminalA.RequestID != firstID || late.RequestID != firstID {
		t.Fatalf("predecessor took the next id: terminal=%+v late=%+v", terminalA, late)
	}
	if acceptedB.RequestID != secondID || terminalB.RequestID != secondID {
		t.Fatalf("second send lost its id: accepted=%+v terminal=%+v", acceptedB, terminalB)
	}
	rows, err := s.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 0 {
		t.Fatalf("delivered requests still queued: %+v %v", rows, err)
	}
}

// TestT1074UnboundTurnDoesNotTakeNextRequest drives drain into the real seat
// send. An event whose turn id this process never accepted is published
// inside the next send, before that send's prompt_accepted. It must leave
// the queued host id for the acceptance.
func TestT1074UnboundTurnDoesNotTakeNextRequest(t *testing.T) {
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	dir := t.TempDir()
	const name = "jv-t1074-unbound"
	const session = "gen-unbound"
	const firstID = "host-A"
	const secondID = "host-B"
	const text = "same text"

	s := &Server{}
	s.SetSendQueueDir(dir)
	s.SetTurnWitness(func(_, _ string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, Durable: true, PayloadSeen: true}
		}
	})
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: session, Materialized: true, Provider: claudia.ProviderGrok,
	}); err != nil {
		t.Fatal(err)
	}
	s.registry = reg

	var seat *claudia.Agent
	var phase int
	seat, err = claudia.StartStub(context.Background(), claudia.Config{
		Name: name, SessionID: session, Provider: claudia.ProviderGrok, Model: "grok-3",
		WorkDir: dir, TermLogPath: "-",
	}, &claudia.StubAgentOps{
		Send: func(string) error {
			phase++
			if phase == 1 {
				seat.PublishEvent(claudia.Event{Type: "progress", TurnID: "1", ProgressType: claudia.ProgressPromptAccepted})
				seat.PublishEvent(claudia.Event{Type: "assistant", TurnID: "1", Text: "done", StopReason: "end_turn"})
				return nil
			}
			seat.PublishEvent(claudia.Event{Type: "assistant", TurnID: "fresh-9", Text: "unbound"})
			seat.PublishEvent(claudia.Event{Type: "system", TurnID: "fresh-err", Text: "nope", IsError: true})
			seat.PublishEvent(claudia.Event{Type: "progress", TurnID: "2", ProgressType: claudia.ProgressPromptAccepted})
			seat.PublishEvent(claudia.Event{Type: "assistant", TurnID: "2", Text: "second", StopReason: "end_turn"})
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { seat.Stop() })
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return seat, false, nil })
	receiptJournal(t, s, dir)
	lane := bindReceiptLane(t, s, dir)

	var mu sync.Mutex
	var got []claudia.Event
	seat.SubscribeEvents(func(ev claudia.Event) {
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
		if ev.IsTerminalStop() && ev.RequestID != "" {
			lane.publish(name, session, ev.RequestID, claudiawire.ReceiptCompleted, "", "", []string{ev.TurnID})
		}
	})

	if _, _, err := s.sendQueue().AppendWithRequestID(name, text, firstID, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.drainAgentSendQueue(name)
	if _, _, err := s.sendQueue().AppendWithRequestID(name, text, secondID, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.drainAgentSendQueue(name)
	if phase != 2 {
		t.Fatalf("sends=%d", phase)
	}
	mu.Lock()
	events := append([]claudia.Event(nil), got...)
	mu.Unlock()
	var unbound, failed, acceptedB, terminalA, terminalB claudia.Event
	var sawUnbound, sawFailed, sawAccepted, sawA, sawB bool
	for _, ev := range events {
		switch {
		case ev.TurnID == "fresh-9":
			unbound, sawUnbound = ev, true
		case ev.TurnID == "fresh-err":
			failed, sawFailed = ev, true
		case ev.TurnID == "2" && ev.ProgressType == claudia.ProgressPromptAccepted:
			acceptedB, sawAccepted = ev, true
		case ev.TurnID == "1" && ev.IsTerminalStop():
			terminalA, sawA = ev, true
		case ev.TurnID == "2" && ev.IsTerminalStop():
			terminalB, sawB = ev, true
		}
	}
	if !sawUnbound || !sawFailed || !sawAccepted || !sawA || !sawB {
		t.Fatalf("missing events: %+v", events)
	}
	if unbound.RequestID != "" || failed.RequestID != "" {
		t.Fatalf("unbound turn took the pending id: unbound=%+v failed=%+v", unbound, failed)
	}
	if terminalA.RequestID != firstID || acceptedB.RequestID != secondID || terminalB.RequestID != secondID {
		t.Fatalf("acceptance lost its id: terminalA=%+v accepted=%+v terminalB=%+v", terminalA, acceptedB, terminalB)
	}
	rows, err := s.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 0 {
		t.Fatalf("delivered requests still queued: %+v %v", rows, err)
	}
}
