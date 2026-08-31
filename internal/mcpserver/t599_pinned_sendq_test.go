// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T599 — an undeliverable sendq message never makes a seat unkillable, and
// a pinned seat says so instead of looking busy.
//
// Tape: one seat + one undeliverable message → overseer-kill ok (held
// messages discarded, one eventlog record naming count and reason),
// parent-kill refused-with-override, agent_list PINNED with the blocking
// message named; a normal queued message stays parent-killable once drained.

const t599Seat = "jv-t530-drain" // reuse the T530 fixture tree

// t599PlantUndeliverable enqueues one message and pins the seat on it, the
// way repeated drain failure does.
func t599PlantUndeliverable(t *testing.T, s *Server) sendq.Entry {
	t.Helper()
	if _, err := s.enqueueAgentSend(t599Seat, "re-brief the overseer ruled must not be delivered"); err != nil {
		t.Fatal(err)
	}
	entries, err := s.sendQueue().Snapshot(t599Seat)
	if err != nil || len(entries) != 1 {
		t.Fatalf("snapshot=%v err=%v", entries, err)
	}
	for range SendqPinFailureThreshold {
		s.noteSendqDeliveryFailure(t599Seat, entries[0], "provider cannot accept it")
	}
	return entries[0]
}

// Clause 1: an overseer kill always succeeds — the T530 hold yields, the held
// messages are discarded, and one eventlog record names count and reason.
func TestT599OverseerKillDiscardsHeldSendq(t *testing.T) {
	s, dir := t530Server(t)
	t530RegisterTree(t, s, dir)
	t599PlantUndeliverable(t, s)

	type logged struct {
		decision string
		fields   map[string]any
	}
	var records []logged
	s.SetEventLogger(func(component, decision string, fields map[string]any) {
		if component == compAgentLifecycle {
			records = append(records, logged{decision: decision, fields: fields})
		}
	})

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": t599Seat, "actor": "jevons"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("overseer kill must always succeed; got refusal: %s", toolText(res))
	}
	body := toolText(res)
	if !strings.Contains(body, "Discarded 1 held sendq message") || !strings.Contains(body, "T599") {
		t.Fatalf("kill reply must name the discard:\n%s", body)
	}
	if s.registry.Def(t599Seat) != nil {
		t.Fatal("seat must be deregistered after overseer kill")
	}
	if depth := s.pendingAgentSends(t599Seat); depth != 0 {
		t.Fatalf("held sendq must be discarded; depth=%d", depth)
	}
	if _, pinned := s.sendqPinFor(t599Seat); pinned {
		t.Fatal("pin must clear with the discard")
	}
	discards := 0
	for _, r := range records {
		if r.decision != "kill_discard_sendq" {
			continue
		}
		discards++
		if r.fields["discarded"] != 1 {
			t.Fatalf("discard record count=%v want 1", r.fields["discarded"])
		}
		reason, _ := r.fields["reason"].(string)
		if !strings.Contains(reason, "T530") || !strings.Contains(reason, "T599") {
			t.Fatalf("discard record reason=%q must name the hold and the override", reason)
		}
	}
	if discards != 1 {
		t.Fatalf("want exactly one kill_discard_sendq eventlog record, got %d", discards)
	}
}

// Clause 2: a parent kill still respects the guard, and the refusal names the
// drain path and the explicit override instead of leaving the caller stuck.
func TestT599ParentKillRefusalNamesOverrideAndDrainPath(t *testing.T) {
	s, dir := t530Server(t)
	t530RegisterTree(t, s, dir)
	t599PlantUndeliverable(t, s)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": t599Seat, "actor": "jevons-po"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("parent kill of a held seat must still be refused")
	}
	body := toolText(res)
	for _, want := range []string{"T530", "Drain path", "jevons_agent_start", "Override", "overseer kill discards", "T599"} {
		if !strings.Contains(body, want) {
			t.Fatalf("refusal missing %q:\n%s", want, body)
		}
	}
	if s.registry.Def(t599Seat) == nil {
		t.Fatal("refused kill must leave the seat registered")
	}
	if depth := s.pendingAgentSends(t599Seat); depth != 1 {
		t.Fatalf("refused kill must not touch the queue; depth=%d", depth)
	}
}

// Clause 3: a pinned seat is reported PINNED with the blocking message named,
// not as ordinary running/idle.
func TestT599AgentListReportsPinnedSeat(t *testing.T) {
	s, dir := t530Server(t)
	t530RegisterTree(t, s, dir)
	entry := t599PlantUndeliverable(t, s)

	req := mcp.CallToolRequest{}
	res, err := s.handleAgentList(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	body := toolText(res)
	var seatRow, pinRow string
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, t599Seat) {
			seatRow = line
			if i+1 < len(lines) {
				pinRow = lines[i+1]
			}
		}
	}
	if seatRow == "" {
		t.Fatalf("seat missing from agent_list:\n%s", body)
	}
	if !strings.Contains(seatRow, "PINNED") {
		t.Fatalf("pinned seat must not present as ordinary running/idle:\n%s", seatRow)
	}
	if !strings.Contains(pinRow, entry.ID) || !strings.Contains(pinRow, "cannot be delivered") {
		t.Fatalf("pin detail must name the blocking message:\n%s", pinRow)
	}
}

// Fleet health carries the same PINNED account (once, at pin time).
func TestT599PinNotifiesFleetHealth(t *testing.T) {
	s, dir := t530Server(t)
	t530RegisterTree(t, s, dir)

	up := &upward{}
	s.SetOverseerDeliver(up.deliver)
	entry := t599PlantUndeliverable(t, s)

	found := false
	for _, m := range up.all() {
		if strings.Contains(m, "PINNED") && strings.Contains(m, t599Seat) && strings.Contains(m, entry.ID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("fleet health must report the pinned seat; got %v", up.all())
	}
}

// Control: below the threshold there is no pin, a delivered message clears
// the count, and a normal queued message stays parent-killable once drained.
func TestT599NormalQueueIsNotPinnedAndDrainsKillable(t *testing.T) {
	s, dir := t530Server(t)
	t530RegisterTree(t, s, dir)

	if _, err := s.enqueueAgentSend(t599Seat, "ordinary queued brief"); err != nil {
		t.Fatal(err)
	}
	entries, err := s.sendQueue().Snapshot(t599Seat)
	if err != nil || len(entries) != 1 {
		t.Fatalf("snapshot=%v err=%v", entries, err)
	}
	for range SendqPinFailureThreshold - 1 {
		s.noteSendqDeliveryFailure(t599Seat, entries[0], "transient")
	}
	if _, pinned := s.sendqPinFor(t599Seat); pinned {
		t.Fatal("below-threshold failures must not pin")
	}
	res, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(toolText(res), "PINNED") {
		t.Fatalf("unpinned queue must not show PINNED:\n%s", toolText(res))
	}

	// Drain the queue (the documented path), then the parent kill goes
	// through as it always did.
	if e := s.dequeueAgentSend(t599Seat); e.Text == "" {
		t.Fatal("expected the queued message to drain")
	}
	s.clearSendqPin(t599Seat)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": t599Seat, "actor": "jevons-po"}
	kres, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if kres.IsError {
		t.Fatalf("drained seat must stay parent-killable: %s", toolText(kres))
	}
	if s.registry.Def(t599Seat) != nil {
		t.Fatal("seat must be gone after the drained parent kill")
	}
}
