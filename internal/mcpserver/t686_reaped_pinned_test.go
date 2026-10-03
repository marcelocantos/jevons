// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T686 — register → prompt in flight → uncertain sendq hold → reap, and
// fleet health afterwards raises no PINNED alert for that name.
//
// The live specimen (2026-09-20 cl-t81-grok-billing) is the fixture: a
// broker-wrapped Cursor ACP in-flight error left sendq 3a62e454aeb3
// uncertain, the seat was reaped, and SweepSendBacklogs kept composing
// PINNED. Kill is a no-op on the reaped name and must not be the fix.

const t686Seat = "cl-t81-grok-billing-fixture"

const t686UncertainDetail = "broker protocol: agent_failed: cursor acp: prompt already in flight; the agent published a session event"

func t686PlantUncertain(t *testing.T, s *Server, name string) sendq.Entry {
	t.Helper()
	if _, err := s.enqueueAgentSend(name, "spawn-brief while prompt in flight"); err != nil {
		t.Fatal(err)
	}
	e, claimed, err := s.sendQueue().ClaimFront(name)
	if err != nil || !claimed {
		t.Fatalf("claim: claimed=%v err=%v", claimed, err)
	}
	if err := s.sendQueue().Resolve(name, e, sendq.Unverified, t686UncertainDetail); err != nil {
		t.Fatal(err)
	}
	entries, err := s.sendQueue().Snapshot(name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Uncertain || entries[0].AttemptID == "" {
		t.Fatalf("uncertain hold missing: %+v %v", entries, err)
	}
	return entries[0]
}

func TestT686ReapedUncertainHoldDoesNotRaisePinnedAlert(t *testing.T) {
	s, dir := t401Server(t)
	t401RegisterWork(t, s, t686Seat)
	up := &upward{}
	s.SetOverseerDeliver(up.deliver)
	s.noteTurnInFlight(t686Seat)
	entry := t686PlantUncertain(t, s, t686Seat)

	t401ReapWithReport(t, s, dir, t686Seat,
		"Done. SHA abcdef1234567890. GATE t686-fix exit=0 GREEN id=deadbeef.")
	if s.registry.Def(t686Seat) != nil {
		t.Fatal("fixture must be reaped before the health sweep")
	}

	// Kill is the documented no-op, not the remedy: it must not discard the
	// hold and must not be what makes the PINNED alert go away.
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": t686Seat, "actor": "jevons"}
	kres, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if kres.IsError {
		t.Fatalf("kill of a reaped name is a no-op success, got %s", toolText(kres))
	}
	if !strings.Contains(toolText(kres), "already not registered") {
		t.Fatalf("kill must name the no-op, got %s", toolText(kres))
	}

	now := time.Date(2026, 9, 20, 0, 51, 31, 0, time.UTC)
	s.SetSweepClock(func() time.Time { return now })
	// T582 incident cadence: the idle loop fires every ~30s for minutes.
	for range 26 {
		s.SweepSendBacklogs()
		now = now.Add(30 * time.Second)
	}

	msgs := up.all()
	for _, m := range msgs {
		if strings.Contains(m, "PINNED "+t686Seat) {
			t.Fatalf("reaped seat must not raise PINNED:\n%s", m)
		}
	}
	if !containsLine(msgs, "reaped-with-reason") {
		t.Fatalf("want a T401 closed-address notice, not silence-without-naming; got %v", msgs)
	}
	closed := 0
	for _, m := range msgs {
		if strings.Contains(m, "reaped-with-reason") && strings.Contains(m, t686Seat) {
			closed++
			if !strings.Contains(m, entry.ID) || !strings.Contains(m, entry.AttemptID) {
				t.Fatalf("closed-address notice must name the hold:\n%s", m)
			}
			if !strings.Contains(m, "kill is a no-op") {
				t.Fatalf("closed-address notice must not prescribe kill:\n%s", m)
			}
		}
	}
	if closed != 1 {
		t.Fatalf("closed-address notice repeated %d times: %v", closed, msgs)
	}

	entries, err := s.sendQueue().Snapshot(t686Seat)
	if err != nil || len(entries) != 1 || entries[0].ID != entry.ID || entries[0].State != sendq.Uncertain {
		t.Fatalf("uncertain hold must survive reap and kill: %+v %v", entries, err)
	}

	list, err := observedAgentList(s, context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	body := toolText(list)
	if strings.Contains(body, t686Seat) && strings.Contains(body, "PINNED "+t686Seat) {
		t.Fatalf("agent_list must not show PINNED for the reaped name:\n%s", body)
	}
	if !strings.Contains(body, "finished and reaped") || !strings.Contains(body, t686Seat) {
		t.Fatalf("agent_list must still name the closed address:\n%s", body)
	}
}

func TestT686LiveUncertainSeatStillPinned(t *testing.T) {
	s, _ := t401Server(t)
	t401RegisterWork(t, s, t686Seat)
	up := &upward{}
	s.SetOverseerDeliver(up.deliver)
	s.noteTurnInFlight(t686Seat)
	entry := t686PlantUncertain(t, s, t686Seat)

	s.SweepSendBacklogs()
	if !containsLine(up.all(), "PINNED "+t686Seat) {
		t.Fatalf("live uncertain seat must still PINNED; got %v", up.all())
	}
	found := false
	for _, m := range up.all() {
		if strings.Contains(m, "PINNED "+t686Seat) && strings.Contains(m, entry.ID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("PINNED must name the live seat and hold; got %v", up.all())
	}
	if s.registry.Def(t686Seat) == nil {
		t.Fatal("control must leave the live seat registered")
	}
}
