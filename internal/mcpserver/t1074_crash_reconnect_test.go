// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// TestT1074InterruptedReceiptIsNotReplayedAcrossRestart is the crash between
// acceptance and receipt. The first daemon bound host-crash and died before a
// receipt came back. After restart the sidecar reports that dispatch as
// unknown/interrupted. Drain must not send it again, must keep that id, and
// must still send the accepted-but-not-started follow-up once.
func TestT1074InterruptedReceiptIsNotReplayedAcrossRestart(t *testing.T) {
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	dir := t.TempDir()
	const name = "jv-t1074-crash"
	const session = "gen-crash"
	const crashedID = "host-crash"
	const followID = "host-follow"
	const text = "same text"

	before, _, _ := t418Daemon(t, dir)
	crashed, _, claimed, err := before.sendQueue().AppendAndClaimWithRequestID(name, text, crashedID, time.Now())
	if err != nil || !claimed {
		t.Fatalf("admit crash: claimed=%v err=%v", claimed, err)
	}
	crashed, err = before.sendQueue().BindReceipt(name, crashed, session)
	if err != nil || crashed.TransportRequestID != crashedID || crashed.SessionID != session {
		t.Fatalf("bind: %+v %v", crashed, err)
	}
	if _, _, err := before.sendQueue().AppendWithRequestID(name, text, followID, time.Now()); err != nil {
		t.Fatal(err)
	}

	// A test-named TempDir exceeds the macOS unix socket path limit.
	sockDir, err := os.MkdirTemp("", "jv-t1074-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	socket := filepath.Join(sockDir, "r.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	t.Setenv("CLAUDIA_OMP_SOCKET", socket)
	var mu sync.Mutex
	var queries []claudia.ReceiptQuery
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				var msg map[string]any
				if json.Unmarshal(line, &msg) != nil {
					return
				}
				q := claudia.ReceiptQuery{
					Seat:      strField(msg["seat"]),
					SessionID: strField(msg["session_id"]),
					RequestID: strField(msg["request_id"]),
				}
				mu.Lock()
				queries = append(queries, q)
				mu.Unlock()
				receipt := heldReceipt(q, claudia.ReceiptUnknown)
				receipt.Reason = "interrupted"
				_ = json.NewEncoder(conn).Encode(map[string]any{
					"type": "receipt", "rpc_id": msg["rpc_id"], "request_id": q.RequestID, "receipt": receipt,
				})
			}(conn)
		}
	}()

	after, _, _ := t418Daemon(t, dir)
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: session, Materialized: true, Provider: claudia.ProviderGrok,
	}); err != nil {
		t.Fatal(err)
	}
	after.registry = reg
	sender := &requestIDSender{}
	setObservedSenderResolver(after, func(string) (agentSender, bool, error) { return sender, false, nil })

	after.drainAgentSendQueue(name)
	after.drainAgentSendQueue(name)
	calls, legacy := sender.snapshot()
	if len(calls) != 1 || calls[0] != followID+":"+text || legacy != 0 {
		t.Fatalf("replayed or dropped the follow-up: calls=%v legacy=%d", calls, legacy)
	}
	rows, err := after.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 1 || rows[0].ID != crashed.ID || rows[0].RequestID != crashedID || rows[0].TransportRequestID != crashedID || rows[0].SessionID != session || rows[0].State == sendq.Pending {
		t.Fatalf("interrupted receipt was reminted or released: %+v %v", rows, err)
	}
	mu.Lock()
	seen := append([]claudia.ReceiptQuery(nil), queries...)
	mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("drain did not query the durable receipt")
	}
	for _, q := range seen {
		if q.Seat != name || q.SessionID != session || q.RequestID != crashedID {
			t.Fatalf("query was not the original interrupted request: %+v", seen)
		}
	}
	if _, _, err = after.sendQueue().AppendWithRequestID(name, text, crashedID, time.Now()); err == nil {
		t.Fatal("reoffering the interrupted id was admitted")
	}
	var duplicate *sendq.DuplicateRequestIDError
	if !errors.As(err, &duplicate) || duplicate.Existing.ID != crashed.ID || duplicate.Conflict {
		t.Fatalf("duplicate=%+v err=%v", duplicate, err)
	}
	after.drainAgentSendQueue(name)
	calls, _ = sender.snapshot()
	if len(calls) != 1 {
		t.Fatalf("reoffer executed: %v", calls)
	}
}

func strField(v any) string {
	s, _ := v.(string)
	return s
}

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

	var mu sync.Mutex
	var got []claudia.Event
	seat.SubscribeEvents(func(ev claudia.Event) {
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
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
