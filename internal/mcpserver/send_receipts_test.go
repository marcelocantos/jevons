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
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/sendq"
)

func receiptHeldEntry(t *testing.T, q *sendq.Store, name, owner string, uncertain bool) sendq.Entry {
	t.Helper()
	e, _, claimed, err := q.AppendAndClaimWithRequestID(name, "identical payload", owner, time.Now().Add(-time.Hour))
	if err != nil || !claimed {
		t.Fatalf("admit: %+v claimed=%v err=%v", e, claimed, err)
	}
	e, err = q.BindReceipt(name, e, "original-generation")
	if err != nil {
		t.Fatal(err)
	}
	if uncertain {
		if err := q.Resolve(name, e, sendq.Unverified, "accepted acknowledgement lost"); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func heldReceipt(q claudia.ReceiptQuery, state claudia.ReceiptState) *claudia.RequestReceipt {
	r := &claudia.RequestReceipt{Seat: q.Seat, SessionID: q.SessionID, RequestID: q.RequestID,
		AttemptID: "claudia-attempt", State: state, TurnIDs: []string{}, Reason: "exact recorded outcome",
		AcceptedAt: "2026-10-16T01:00:00Z", UpdatedAt: "2026-10-16T01:00:01Z"}
	if state != claudia.ReceiptAccepted {
		r.TurnIDs = []string{"original-turn"}
	}
	return r
}

func receiptJournal(t *testing.T, s *Server, dir string) *eventlog.Journal {
	t.Helper()
	j, err := eventlog.Open(eventlog.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	s.SetEventJournal(j)
	t.Cleanup(func() { _ = j.Close() })
	return j
}

func parkReceiptFleet(t *testing.T, s *Server, dir string) *fleetintent.Store {
	t.Helper()
	intent, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := intent.SetFleet(fleetintent.Parked, "owner", "fleet remains parked", time.Now()); err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(intent)
	return intent
}

func TestHeldReceiptsSweepSettlesOriginalGenerationWhileParked(t *testing.T) {
	for _, state := range []claudia.ReceiptState{claudia.ReceiptAccepted, claudia.ReceiptStarted, claudia.ReceiptCompleted, claudia.ReceiptCancelled, claudia.ReceiptFailed} {
		for _, uncertain := range []bool{true, false} {
			label := string(state) + "/orphaned-attempt"
			if uncertain {
				label = string(state) + "/uncertain"
			}
			t.Run(label, func(t *testing.T) {
				const name = "worker"
				dir := t.TempDir()
				before, _, _ := t418Daemon(t, dir)
				e := receiptHeldEntry(t, before.sendQueue(), name, "owner-question", uncertain)
				// Reconstruction is the ACK-loss/crash boundary. Today's registry
				// points at a replacement; it must not choose the query generation.
				s, sender, up := t418Daemon(t, dir)
				reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
				if err != nil {
					t.Fatal(err)
				}
				if err := reg.Register(claudia.AgentDef{Name: name, SessionID: "replacement-generation", WorkDir: dir}); err != nil {
					t.Fatal(err)
				}
				s.registry = reg
				intent := parkReceiptFleet(t, s, dir)
				j := receiptJournal(t, s, dir)
				if _, _, err := s.sendQueue().Append(name, "still intentionally pending", time.Now()); err != nil {
					t.Fatal(err)
				}
				queries := 0
				s.querySendReceipt = func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
					queries++
					if q.Seat != name || q.SessionID != e.SessionID || q.RequestID != e.TransportRequestID {
						t.Fatalf("queried replacement/other input: %+v", q)
					}
					return heldReceipt(q, state), nil
				}
				s.SweepSendBacklogs()
				s.SweepSendBacklogs()
				rows, err := s.sendQueue().Snapshot(name)
				if err != nil || len(rows) != 1 || rows[0].State != sendq.Pending || rows[0].ID == e.ID || queries != 1 {
					t.Fatalf("wrong obligation settled: %+v queries=%d err=%v", rows, queries, err)
				}
				if len(sender.delivered()) != 0 || len(up.all()) != 0 || intent.FleetState() != fleetintent.Parked || reg.Def(name).SessionID != "replacement-generation" {
					t.Fatal("receipt recovery dispatched, notified, or changed intent/generation")
				}
				events, err := eventlog.Tail(j.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
				if err != nil || len(events) != 1 {
					t.Fatalf("missing durable outcome: %+v %v", events, err)
				}
				fields := events[0].Fields
				r, ok := fields["receipt"].(map[string]any)
				if !ok || r["state"] != string(state) || r["reason"] != "exact recorded outcome" || r["attempt_id"] != "claudia-attempt" || fields["attempt_id"] != e.AttemptID || fields["transport_request_id"] != e.TransportRequestID || fields["session_id"] != e.SessionID || fields["text"] != e.Text || fields["transport_accepted"] != true {
					t.Fatalf("lost typed receipt/provenance: %+v", fields)
				}
			})
		}
	}
}

func TestHeldReceiptsRejectMissingUnknownCorruptOrForeignEvidence(t *testing.T) {
	variants := []struct {
		name  string
		alter func(*claudia.RequestReceipt) (*claudia.RequestReceipt, error)
	}{
		{"error", func(r *claudia.RequestReceipt) (*claudia.RequestReceipt, error) {
			return r, errors.New("journal corrupt or transport unavailable")
		}},
		{"missing", func(*claudia.RequestReceipt) (*claudia.RequestReceipt, error) { return nil, nil }},
		{"unknown", func(r *claudia.RequestReceipt) (*claudia.RequestReceipt, error) {
			r.State, r.Reason = claudia.ReceiptUnknown, "not_found"
			return r, nil
		}},
		{"wrong-seat", func(r *claudia.RequestReceipt) (*claudia.RequestReceipt, error) { r.Seat = "other"; return r, nil }},
		{"wrong-generation", func(r *claudia.RequestReceipt) (*claudia.RequestReceipt, error) {
			r.SessionID = "replacement"
			return r, nil
		}},
		{"wrong-request", func(r *claudia.RequestReceipt) (*claudia.RequestReceipt, error) { r.RequestID = "other"; return r, nil }},
		{"wrong-state", func(r *claudia.RequestReceipt) (*claudia.RequestReceipt, error) {
			r.State = "probably-done"
			return r, nil
		}},
		{"incomplete", func(r *claudia.RequestReceipt) (*claudia.RequestReceipt, error) { r.AcceptedAt = ""; return r, nil }},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			dir := t.TempDir()
			s, sender, up := t418Daemon(t, dir)
			e := receiptHeldEntry(t, s.sendQueue(), "a", "", true)
			parkReceiptFleet(t, s, dir)
			receiptJournal(t, s, dir)
			s.querySendReceipt = func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
				return variant.alter(heldReceipt(q, claudia.ReceiptCompleted))
			}
			s.SweepSendBacklogs()
			s.drainAgentSendQueue("a")
			rows, err := s.sendQueue().Snapshot("a")
			if err != nil || len(rows) != 1 || rows[0].ID != e.ID || rows[0].AttemptID != e.AttemptID || rows[0].State != sendq.Uncertain || len(sender.delivered()) != 0 || len(up.all()) != 0 {
				t.Fatalf("uncertainty changed or dispatched: %+v %v", rows, err)
			}
		})
	}
}

func TestHeldReceiptsDrainSeparatesIdenticalInputsAndLeavesLegacyUnbound(t *testing.T) {
	dir := t.TempDir()
	s, sender, _ := t418Daemon(t, dir)
	first := receiptHeldEntry(t, s.sendQueue(), "a", "owner-A", true)
	second := receiptHeldEntry(t, s.sendQueue(), "a", "owner-B", true)
	legacy, _, claimed, err := s.sendQueue().AppendAndClaimWithRequestID("a", first.Text, "legacy-owner-C", time.Now())
	if err != nil || !claimed {
		t.Fatal(err)
	}
	if err := s.sendQueue().Resolve("a", legacy, sendq.Unverified, "legacy send without receipt binding"); err != nil {
		t.Fatal(err)
	}
	receiptJournal(t, s, dir)
	queries := map[string]int{}
	s.querySendReceipt = func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		queries[q.RequestID]++
		state := claudia.ReceiptUnknown
		if q.RequestID == first.TransportRequestID {
			state = claudia.ReceiptCompleted
		}
		return heldReceipt(q, state), nil
	}
	s.drainAgentSendQueue("a")
	rows, err := s.sendQueue().Snapshot("a")
	if err != nil || len(rows) != 2 || rows[0].ID != second.ID || rows[1].ID != legacy.ID || len(queries) != 2 || len(sender.delivered()) != 0 {
		t.Fatalf("same text or unbound ID became evidence: %+v queries=%v err=%v", rows, queries, err)
	}
}

func TestHeldReceiptsEvidenceWriteFailureRetainsObligation(t *testing.T) {
	for _, closed := range []bool{false, true} {
		dir := t.TempDir()
		s, sender, up := t418Daemon(t, dir)
		e := receiptHeldEntry(t, s.sendQueue(), "a", "", true)
		parkReceiptFleet(t, s, dir)
		if closed {
			j := receiptJournal(t, s, dir)
			if err := j.Close(); err != nil {
				t.Fatal(err)
			}
		}
		// A logger callback that cannot acknowledge fsync is insufficient.
		s.SetEventLogger(func(string, string, map[string]any) { t.Fatal("best-effort logger used as durable evidence") })
		s.querySendReceipt = func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
			return heldReceipt(q, claudia.ReceiptFailed), nil
		}
		s.SweepSendBacklogs()
		rows, err := s.sendQueue().Snapshot("a")
		if err != nil || len(rows) != 1 || rows[0].ID != e.ID || len(sender.delivered()) != 0 || len(up.all()) != 0 {
			t.Fatalf("lost obligation on evidence failure: %+v %v", rows, err)
		}
		receiptJournal(t, s, dir)
		s.SweepSendBacklogs()
		if rows, err := s.sendQueue().Snapshot("a"); err != nil || len(rows) != 0 {
			t.Fatalf("did not recover when journal restored: %+v %v", rows, err)
		}
	}
}

func TestHeldReceiptsUnreadableEvidenceRetainsObligation(t *testing.T) {
	for _, torn := range []bool{true, false} {
		label := "oversized-receipt"
		if torn {
			label = "torn-existing-journal"
		}
		t.Run(label, func(t *testing.T) {
			dir := t.TempDir()
			s, sender, up := t418Daemon(t, dir)
			e := receiptHeldEntry(t, s.sendQueue(), "a", "", true)
			parkReceiptFleet(t, s, dir)
			path := eventlog.DefaultPath(dir)
			var original []byte
			if torn {
				original = []byte(`{"decision":"previous-event","fields":`)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, original, 0o600); err != nil {
					t.Fatal(err)
				}
				j, err := eventlog.Open(path)
				if err == nil || j != nil {
					if j != nil {
						_ = j.Close()
					}
					t.Fatalf("torn evidence accepted: journal=%v err=%v", j, err)
				}
				s.SetEventJournal(j)
			} else {
				receiptJournal(t, s, dir)
			}
			s.querySendReceipt = func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
				r := heldReceipt(q, claudia.ReceiptFailed)
				if !torn {
					// Exceeds the journal reader's existing 1 MiB line cap.
					r.Reason = strings.Repeat("x", 1<<20)
				}
				return r, nil
			}
			s.SweepSendBacklogs()
			s.drainAgentSendQueue("a")
			rows, err := s.sendQueue().Snapshot("a")
			if err != nil || len(rows) != 1 || rows[0].ID != e.ID || rows[0].AttemptID != e.AttemptID || rows[0].State != sendq.Uncertain || len(sender.delivered()) != 0 || len(up.all()) != 0 {
				t.Fatalf("lost or dispatched obligation without readable evidence: %+v %v", rows, err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != string(original) {
				t.Fatalf("failed evidence write changed original journal: len=%d err=%v", len(data), err)
			}
		})
	}
}

func TestHeldReceiptsSweepSharesDeadlineAcrossSeatsAndDrains(t *testing.T) {
	dir := t.TempDir()
	s, sender, _ := t418Daemon(t, dir)
	for _, name := range []string{"a", "b"} {
		receiptHeldEntry(t, s.sendQueue(), name, "", true)
		if _, _, err := s.sendQueue().Append(name, "new pending input", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var first time.Time
	queries := map[string]int{}
	s.querySendReceipt = func(ctx context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("receipt query is unbounded")
		}
		if first.IsZero() {
			first = deadline
		} else if !deadline.Equal(first) {
			t.Fatalf("sweep renewed timeout for %s: %v != %v", q.Seat, deadline, first)
		}
		queries[q.Seat]++
		return nil, context.DeadlineExceeded
	}
	s.SweepSendBacklogs()
	if queries["a"] != 2 || queries["b"] != 2 || len(sender.delivered()) != 2 {
		t.Fatalf("did not exercise sweep and flow-past drain for both seats: queries=%v delivered=%v", queries, sender.delivered())
	}
}

func TestHeldReceiptsSweepRefreshesArrivalsDuringQuery(t *testing.T) {
	dir := t.TempDir()
	s, sender, _ := t418Daemon(t, dir)
	receiptHeldEntry(t, s.sendQueue(), "a", "", true)
	receiptJournal(t, s, dir)
	s.querySendReceipt = func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		if _, _, err := s.sendQueue().Append("a", "arrived during receipt query", time.Now()); err != nil {
			t.Fatal(err)
		}
		return heldReceipt(q, claudia.ReceiptCompleted), nil
	}
	s.SweepSendBacklogs()
	got := sender.delivered()
	if len(got) != 1 || got[0] != "arrived during receipt query" {
		t.Fatalf("sweep used its stale empty cohort: %v", got)
	}
}

func TestHeldReceiptsCannotSettleActiveOrRequeuedAttempt(t *testing.T) {
	for _, active := range []bool{true, false} {
		dir := t.TempDir()
		s, _, _ := t418Daemon(t, dir)
		e := receiptHeldEntry(t, s.sendQueue(), "a", "", !active)
		parkReceiptFleet(t, s, dir)
		receiptJournal(t, s, dir)
		s.querySendReceipt = func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
			if !active {
				if _, err := s.sendQueue().Reconcile("a", e.ID, e.AttemptID, sendq.ReconcileRequeue, "owner", "explicit requeue while query was pending", time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			return heldReceipt(q, claudia.ReceiptAccepted), nil
		}
		s.SweepSendBacklogs()
		rows, err := s.sendQueue().Snapshot("a")
		if err != nil || len(rows) != 1 || rows[0].ID != e.ID {
			t.Fatalf("query settled changed/active attempt: %+v %v", rows, err)
		}
	}
}

func TestHeldReceiptsReapedSeatUsesPublicQueryWithoutLoading(t *testing.T) {
	dir := t.TempDir()
	before, _, _ := t418Daemon(t, dir)
	e := receiptHeldEntry(t, before.sendQueue(), "reaped-seat", "", false)
	s, sender, up := t418Daemon(t, dir)
	receiptJournal(t, s, dir)
	intent, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := intent.SetAgent("reaped-seat", fleetintent.Reaped, "product", "old seat removed", time.Now()); err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(intent)
	// Only the read-only local receipt wire exists. No loaded seat, broker,
	// credential provider or model is supplied by this fixture.
	sockDir, err := os.MkdirTemp("", "jv-receipt-")
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
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	t.Setenv("CLAUDIA_OMP_SOCKET", socket)
	wire := make(chan map[string]any, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			return
		}
		var msg map[string]any
		if json.Unmarshal(line, &msg) != nil {
			return
		}
		wire <- msg
		q := claudia.ReceiptQuery{Seat: "reaped-seat", SessionID: e.SessionID, RequestID: e.TransportRequestID}
		_ = json.NewEncoder(conn).Encode(map[string]any{"type": "receipt", "rpc_id": msg["rpc_id"], "request_id": e.TransportRequestID, "receipt": heldReceipt(q, claudia.ReceiptCancelled)})
	}()
	s.SweepSendBacklogs()
	select {
	case msg := <-wire:
		if msg["op"] != "receipt" || msg["session_id"] != e.SessionID || msg["request_id"] != e.TransportRequestID || msg["seat"] != "reaped-seat" || msg["token"] != nil || msg["provider"] != nil || msg["model"] != nil {
			t.Fatalf("recovery attempted model/credential operation: %+v", msg)
		}
	default:
		t.Fatal("production Claudia query did not reach fixture")
	}
	rows, err := s.sendQueue().Snapshot("reaped-seat")
	if err != nil || len(rows) != 0 || len(sender.delivered()) != 0 || len(up.all()) != 0 || intent.AgentState("reaped-seat") != fleetintent.Reaped {
		t.Fatalf("reaped recovery altered lifecycle: %+v %v", rows, err)
	}
}
