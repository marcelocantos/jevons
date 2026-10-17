// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/claudiawire"
)

// installOwnerReceiptSession gives the overseer a registry generation so a
// drain can read a receipt. It does not install a query.
func installOwnerReceiptSession(t *testing.T, s *Server) {
	t.Helper()
	name := s.overseerAgentName()
	s.SetOverseerName(name)
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, SessionID: "gen-overseer", TargetID: "T-overseer-mission",
		Provider: claudia.ProviderGrok, WorkDir: dir,
	}); err != nil {
		t.Fatal(err)
	}
	s.SetRegistry(reg)
}

// installSettlingOwnerReceipt is the ack the send-landed gap and the
// owner-visible delivered frame now require: a completed receipt whose
// Reason is prose and whose Cause is empty.
func installSettlingOwnerReceipt(t *testing.T, s *Server) {
	t.Helper()
	installOwnerReceiptSession(t, s)
	name := s.overseerAgentName()
	s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		if q.RequestID == "" || q.SessionID != "gen-overseer" || q.Seat != name {
			t.Errorf("drain receipt query = %+v", q)
		}
		return durableOwnerReceipt(q, claudiawire.ReceiptCompleted, "exact recorded outcome"), nil
	})
}

func drainReceiptServer(t *testing.T) (*Server, *[]overseerIDCall) {
	t.Helper()
	s := &Server{}
	s.SetOverseerName("jevons")
	calls := &[]overseerIDCall{}
	s.overseerSendSeam = func(text, id string) error {
		*calls = append(*calls, overseerIDCall{text: text, id: id})
		return nil
	}
	return s, calls
}

func sendErrorStates(lines chan string) []string {
	var out []string
	for {
		select {
		case l := <-lines:
			var m map[string]any
			if json.Unmarshal([]byte(l), &m) == nil && m["type"] == "send_error" {
				out = append(out, fmt.Sprint(m["state"]))
			}
		default:
			return out
		}
	}
}

func TestT1074DrainNilErrorDoesNotConfirm(t *testing.T) {
	s, calls := drainReceiptServer(t)
	installOwnerReceiptSession(t, s)
	lines := make(chan string, 8)
	s.chatListeners = append(s.chatListeners, lines)
	var queries []claudiawire.ReceiptQuery
	s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		queries = append(queries, q)
		return durableOwnerReceipt(q, claudiawire.ReceiptUnknown, "not_found"), nil
	})
	s.overseerSendSeam = func(text, id string) error {
		*calls = append(*calls, overseerIDCall{text: text, id: id})
		if len(*calls) == 1 {
			return fmt.Errorf("broker protocol: not_owner")
		}
		return nil
	}
	wire := userTurnPrefix + "beta"
	if err := s.SendToOverseerAs(wire, "om-beta"); err != nil {
		t.Fatal(err)
	}
	s.drainOverseerNotes()
	if len(*calls) != 2 || (*calls)[1].id != "om-beta" {
		t.Fatalf("handoff = %+v", *calls)
	}
	if len(queries) != 1 || queries[0].RequestID != "om-beta" || queries[0].SessionID != "gen-overseer" || queries[0].Seat != "jevons" {
		t.Fatalf("query = %+v", queries)
	}
	if ownerSendDelivered(s) {
		t.Fatal("unknown receipt confirmed owner delivery")
	}
	states := sendErrorStates(lines)
	if !containsString(states, "undelivered") || containsString(states, "delivered") {
		t.Fatalf("frames = %v", states)
	}
	s.mu.Lock()
	queued := len(s.notifyQueue)
	s.mu.Unlock()
	if queued != 0 {
		t.Fatalf("queue still holds %d after the write returned", queued)
	}
}

func TestT1074DrainProseReasonDoesNotInventCause(t *testing.T) {
	s, calls := drainReceiptServer(t)
	installOwnerReceiptSession(t, s)
	var queries []claudiawire.ReceiptQuery
	var seen *claudiawire.RequestReceipt
	s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		queries = append(queries, q)
		seen = durableOwnerReceipt(q, claudiawire.ReceiptCompleted, "401 unauthorized")
		return seen, nil
	})
	if err := s.SendToOverseerAs(userTurnPrefix+"beta", "om-beta"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].id != "om-beta" {
		t.Fatalf("handoff = %+v", *calls)
	}
	if len(queries) != 1 || queries[0].RequestID != "om-beta" || queries[0].SessionID != "gen-overseer" || queries[0].Seat != "jevons" {
		t.Fatalf("query = %+v", queries)
	}
	if seen == nil || seen.Cause != "" || seen.Reason != "401 unauthorized" || seen.State != claudiawire.ReceiptCompleted {
		t.Fatalf("receipt = %+v", seen)
	}
	if !ownerSendDelivered(s) {
		t.Fatal("settling receipt with prose reason did not confirm owner delivery")
	}
}

func TestT1074DrainNilQueryDoesNotConfirm(t *testing.T) {
	s, calls := drainReceiptServer(t)
	installOwnerReceiptSession(t, s)
	done := make(chan struct{})
	var err error
	go func() {
		err = s.SendToOverseerAs(userTurnPrefix+"beta", "om-beta")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("nil receipt query blocked the drain")
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].id != "om-beta" {
		t.Fatalf("handoff = %+v", *calls)
	}
	if ownerSendDelivered(s) {
		t.Fatal("nil query confirmed owner delivery")
	}
}

func TestT1074DrainMissingSessionDoesNotQuery(t *testing.T) {
	s, calls := drainReceiptServer(t)
	called := 0
	s.SetSendReceiptQuery(func(context.Context, claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		called++
		return nil, fmt.Errorf("should not query")
	})
	if err := s.SendToOverseerAs(userTurnPrefix+"beta", "om-beta"); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatalf("queries = %d", called)
	}
	if len(*calls) != 1 || (*calls)[0].id != "om-beta" {
		t.Fatalf("handoff = %+v", *calls)
	}
	if ownerSendDelivered(s) {
		t.Fatal("missing session confirmed owner delivery")
	}
}

func TestT1074DrainFleetNoteDoesNotConfirmOwnerDelivery(t *testing.T) {
	s, calls := drainReceiptServer(t)
	installOwnerReceiptSession(t, s)
	called := 0
	s.SetSendReceiptQuery(func(context.Context, claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		called++
		return nil, fmt.Errorf("fleet note has no owner delivery")
	})
	if err := s.SendToOverseer("[worker] fleet note"); err != nil {
		t.Fatal(err)
	}
	if called != 0 || ownerSendDelivered(s) {
		t.Fatalf("fleet note queried=%d delivered=%v calls=%+v", called, ownerSendDelivered(s), *calls)
	}
	if len(*calls) != 1 || (*calls)[0].id == "" || strings.HasPrefix((*calls)[0].id, "om-") {
		t.Fatalf("fleet handoff = %+v", *calls)
	}
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
