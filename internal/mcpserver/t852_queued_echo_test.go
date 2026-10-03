// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/delivery"
)

// 🎯T852: a busy seat's owner message is RECORDED and SAID, not just held.
//
// J31's aside arm holds a tool call open and submits an owner follow-up over
// the real mux (🎯T813). Two things have to be true of the daemon's answer, and
// the 2026-09-22 diagnosis of the J31 red read the logs as though neither was:
//
//  1. the queue is on the record — `agent_send … status=queued` through
//     logAgentSendOutcome/logAgentSendResult, so an operator can tell a message
//     the daemon is holding from one that was never offered to it at all; and
//  2. the owner's turn is admitted through the canonical recorder BEFORE the
//     caller is told it queued, so the seat's transcript carries the echo the
//     owner would see rather than going silent until the turn ends.
//
// Neither is incidental to the queue arm: the queue is the one outcome where
// nothing reaches the provider, so the record and the log line are the ONLY
// owner-visible trace the message exists. Both are pinned here because the
// admission and the queue live in different files (deliverByNameWith records,
// deliverToSenderMode queues) and nothing else fails when one of them stops.
func TestT852BusySeatQueuesOnTheRecordAndAdmitsTheOwnerTurn(t *testing.T) {
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const seat = "react-aside-t852"
	const payload = "ACK-t852-owner-interleave"

	// A seat mid-turn: the tool call J31 holds open, seen from the daemon.
	held := &fakeSender{alive: true, inFlight: true}
	s, _ := chainServer(t, map[string]*fakeSender{seat: held})
	s.stateDir = t.TempDir()
	s.noteTurnInFlight(seat)

	type admission struct {
		name, text string
		origin     SendOrigin
		queued     int
	}
	var admitted []admission
	s.SetAgentRequestRecorder(func(name, text string, origin SendOrigin) error {
		// Recorded before the text is queued, and before it is offered to the
		// process: the journal is the admission, not a receipt.
		admitted = append(admitted, admission{name, text, origin, observedPendingSends(s, seat)})
		return nil
	})

	res, err := s.DeliverAgentMessageMode(seat, payload, OriginOwner, delivery.ModeSubmit)
	if err != nil {
		t.Fatalf("owner submit to a busy seat: %v", err)
	}
	if res.Status != "queued" {
		t.Fatalf("status=%q want queued (message=%q)", res.Status, res.Message)
	}
	if len(held.sent) != 0 {
		t.Fatalf("a turn was in flight but the text was offered to the process anyway: %v", held.sent)
	}
	if n := observedPendingSends(s, seat); n != 1 {
		t.Fatalf("daemon queue depth=%d want 1 — the owner message is not held anywhere", n)
	}

	// Clause 2: admitted once, as the owner, with the owner's own words, and
	// while the queue was still empty (so the record precedes the hold).
	if len(admitted) != 1 {
		t.Fatalf("admissions=%d want 1: %+v", len(admitted), admitted)
	}
	if got := admitted[0]; got.name != seat || got.text != payload ||
		got.origin != OriginOwner || got.queued != 0 {
		t.Fatalf("admission=%+v want {%q %q owner 0}", got, seat, payload)
	}

	// Clause 1: the queue is on the record, with the mode and mechanism that
	// say the DAEMON holds it rather than the agent's own composer (🎯T418).
	var lines []map[string]any
	for _, r := range cap.snapshot() {
		if r.Message != "agent_send" {
			continue
		}
		m := attrsMap(r)
		if m["name"] == seat {
			lines = append(lines, m)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("agent_send log lines for %q = %d, want 1: %+v", seat, len(lines), lines)
	}
	got := lines[0]
	for key, want := range map[string]any{
		"status":    "queued",
		"mode":      string(delivery.ModeSubmit),
		"mechanism": delivery.MechanismClientQueue,
		"queued":    int64(1),
	} {
		if got[key] != want {
			t.Errorf("log %s=%v (%T) want %v", key, got[key], got[key], want)
		}
	}
	if flight, _ := got["flight"].(string); !strings.Contains(flight, "in_flight") {
		t.Errorf("log flight=%v, want the in-flight reading the queue was decided on", got["flight"])
	}
}
