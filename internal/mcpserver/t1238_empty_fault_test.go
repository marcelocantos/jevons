// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestT1238DeliveredEmptyRebriefFaultsOnce(t *testing.T) {
	s, po, child := t1233Server(t)
	s.idleActivity = NewIdleActivityTracker()
	var err error
	s.idleNudgeLedger, err = OpenIdleNudgeLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink := s.agentEventSink(t1233Child)
	// The fake accepts the original prompt and the recovery prompt; the
	// backend ends both turns without any assistant content or finish report.
	if _, err := s.deliverByName(t1233Child, "work", OriginAgent, false); err != nil {
		t.Fatal(err)
	}
	sink(claudia.Event{Type: "assistant", StopReason: "length"})
	if line, ok := s.EmptyTurnFault(t1233Child); ok {
		t.Fatalf("premature fault: %s", line)
	}
	child.inFlight = false
	if _, err := s.deliverByName(t1233Child, "RECOVER rebrief", OriginAgent, false); err != nil {
		t.Fatal(err)
	}
	s.idleActivity.ClearRecover(t1233Child, time.Now())
	if err := s.idleNudgeLedger.Record(t1233Child, time.Now()); err != nil {
		t.Fatal(err)
	}
	if line := s.EmptyTurnBackoff(t1233Child); !strings.Contains(line, "repressure refused: backoff") {
		t.Fatalf("backoff row=%q", line)
	}
	sink(claudia.Event{Type: "assistant", StopReason: "length"})
	line, ok := s.EmptyTurnFault(t1233Child)
	if !ok || !strings.Contains(line, "2 consecutive delivered turns") {
		t.Fatalf("fault=%q ok=%v", line, ok)
	}
	s.SetSeatAliveFn(func(name string) bool { return name == t1233Child || name == t1233PO })
	s.Seats().Observe(seatstate.Observation{Name: t1233Child, Alive: seatstate.Yes, Source: "test", QueueDepth: seatstate.QueueUnknown})
	list, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if body := toolText(list); !strings.Contains(body, "faulted") || !strings.Contains(body, "terminal_empty") {
		t.Fatalf("list missing fault: %s", body)
	}
	if len(po.sent) != 1 || !strings.Contains(po.sent[0], "seat-fault") || !strings.Contains(po.sent[0], "stopReason length") {
		t.Fatalf("parent notices: %v", po.sent)
	}
	sink(claudia.Event{Type: "assistant", StopReason: "max_tokens"})
	if len(po.sent) != 1 {
		t.Fatalf("duplicate notice: %v", po.sent)
	}
}

func TestT1238AnsweredSeatRemainsIdle(t *testing.T) {
	s, po, _ := t1233Server(t)
	s.idleActivity = NewIdleActivityTracker()
	sink := s.agentEventSink(t1233Child)
	sink(claudia.Event{Type: "assistant", StopReason: "max_tokens"})
	s.idleActivity.ClearRecover(t1233Child, time.Now())
	sink(claudia.Event{Type: "assistant", Text: "I am working on it.", StopReason: "end_turn"})
	if _, ok := s.EmptyTurnFault(t1233Child); ok {
		t.Fatal("answer marked faulted")
	}
	if got := s.idleActivity.Get(t1233Child); got.EmptyStreak != 0 || got.Phase != "idle" {
		t.Fatalf("answer activity=%+v", got)
	}
	if len(po.sent) != 1 || strings.Contains(po.sent[0], "seat-fault") {
		t.Fatalf("parent notices=%v", po.sent)
	}
}

// omp duplicates a tool step: an empty stop followed by toolUse with the
// same timestamp. Neither record is a terminal silent answer.
func TestT1238CodexStopToolUsePairNotFaulted(t *testing.T) {
	s, po, _ := t1233Server(t)
	s.idleActivity = NewIdleActivityTracker()
	sink := s.agentEventSink(t1233Child)
	const stamp = `{"timestamp":"2026-10-27T15:34:21.882Z"}`
	for i := 0; i < 3; i++ {
		sink(claudia.Event{Type: "assistant", StopReason: "end_turn", Raw: []byte(stamp)}) // omp stop spool projection
		sink(claudia.Event{Type: "assistant", StopReason: "toolUse", Raw: []byte(stamp)})
	}
	if a := s.idleActivity.Get(t1233Child); a.EmptyStreak != 0 {
		t.Fatalf("tool steps counted as empty: %+v", a)
	}
	if _, ok := s.EmptyTurnFault(t1233Child); ok || len(po.sent) != 0 {
		t.Fatalf("false fault=%v parent=%v", ok, po.sent)
	}
}
