// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/brokerfixture/controllane"
	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// replicaSeat is the production handoff target. Drain and steer must use the
// request-id methods. The seat's receipts are published by a real broker
// process (the seat pump's RecordObservedReceipt, in the broker), and only
// when the test has armed a receipt.
type replicaSeat struct {
	t       *testing.T
	seat    string
	session string
	text    string
	// lane is the broker that publishes the seat's receipts.
	lane *controllane.Lane

	mu      sync.Mutex
	publish bool
	state   claudia.ReceiptState
	cause   claudia.ReceiptCause
	reason  string
	turnIDs []string
	ids     []string
}

func (p *replicaSeat) Alive() bool      { return true }
func (p *replicaSeat) Interrupt() error { return nil }

func (p *replicaSeat) Send(string) error {
	p.t.Errorf("untagged Send")
	return errors.New("untagged Send")
}

func (p *replicaSeat) SendModeString(string, string) (string, string, error) {
	p.t.Errorf("untagged steer")
	return "", "", errors.New("untagged steer")
}

func (p *replicaSeat) SendWithRequestID(text, id string) error {
	p.note(text, id)
	return nil
}

func (p *replicaSeat) SendModeWithRequestIDString(text, mode, id string) (string, string, error) {
	if mode != string(delivery.ModeSteer) {
		p.t.Errorf("steer seam mode = %q", mode)
	}
	p.note(text, id)
	return "steer", delivery.PhaseInTurn, nil
}

func (p *replicaSeat) arm(publish bool, state claudia.ReceiptState, cause claudia.ReceiptCause, reason string, turns []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.publish = publish
	p.state = state
	p.cause = cause
	p.reason = reason
	p.turnIDs = append([]string{}, turns...)
}

func (p *replicaSeat) note(text, id string) {
	if text != p.text {
		p.t.Errorf("sent %q", text)
	}
	p.mu.Lock()
	p.ids = append(p.ids, id)
	publish := p.publish
	receipt := claudia.RequestReceipt{
		RequestID: id, SessionID: p.session, Seat: p.seat,
		State: p.state, AttemptID: "attempt-" + id,
		TurnIDs: append([]string{}, p.turnIDs...),
		Cause:   p.cause, Reason: p.reason,
		AcceptedAt: "2026-10-19T00:00:00.000000000Z",
		UpdatedAt:  "2026-10-19T00:00:00.000000000Z",
	}
	p.mu.Unlock()
	if publish {
		// The pump publishes before the send returns. Settlement then reads
		// the replica, which is the order startCostGuard's query sees: the
		// lane returns once the receipt is in Jevons' replica.
		p.lane.PublishReceipt(receipt)
	}
}

func (p *replicaSeat) sentIDs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.ids...)
}

// TestT10743HostRequestSurvivesSteerRetryRestart is the production receipt
// path: Jevons' replica linked to a real broker process, NewCollector, and
// SetSendReceiptQuery(ReplicatedReceipt), the installer inside
// startCostGuard. It does not stub that query, and it does not run the
// collector loop.
func TestT10743HostRequestSurvivesSteerRetryRestart(t *testing.T) {
	const (
		name    = "jv-t1074-3"
		session = "gen-t1074-3"
		mission = "T1074"
		text    = "same text"
		hostID  = "host-A"
	)
	dir := t.TempDir()
	ctx := context.Background()

	store, err := cost.OpenStore(filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: session, TargetID: mission,
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}

	lane := controllane.Start(t, dir, store)
	collector := lane.Collector
	reopen := false
	bindPlane := func(s *Server) {
		t.Helper()
		if reopen {
			collector = lane.Reopen()
		}
		reopen = true
		s.SetSendReceiptQuery(collector.ReplicatedReceipt)
	}
	var journal *eventlog.Journal
	wire := func(s *Server, seat *replicaSeat) {
		t.Helper()
		s.registry = reg
		s.SetRequestMissionRecorder(store.RecordRequestMission)
		s.SetEventJournal(journal)
		setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return seat, false, nil })
		bindPlane(s)
	}

	firstSeat := &replicaSeat{t: t, seat: name, session: session, text: text, lane: lane}
	first, _, _ := t418Daemon(t, dir)
	journal = receiptJournal(t, first, filepath.Join(dir, "journal"))
	wire(first, firstSeat)

	// Acceptance with no receipt is the crash between queue-hold and a
	// durable fact. The host id is already the transport id.
	firstSeat.arm(false, "", "", "", nil)
	if _, _, err := first.sendQueue().AppendWithRequestID(name, text, hostID, time.Now()); err != nil {
		t.Fatal(err)
	}
	first.drainAgentSendQueue(name)
	if got := firstSeat.sentIDs(); len(got) != 1 || got[0] != hostID {
		t.Fatalf("first send = %v, want [%s]", got, hostID)
	}
	held, err := first.sendQueue().Snapshot(name)
	if err != nil || len(held) != 1 || held[0].State != sendq.Uncertain ||
		held[0].RequestID != hostID || held[0].TransportRequestID != hostID || held[0].SessionID != session {
		t.Fatalf("crash hold = %+v %v", held, err)
	}

	// The same host id is refused while that row is held, and a drain does
	// not execute it again. A confirmed row is not a permanent tombstone.
	_, _, err = first.sendQueue().AppendWithRequestID(name, text, hostID, time.Now())
	var dup *sendq.DuplicateRequestIDError
	if !errors.As(err, &dup) || dup.RequestID != hostID {
		t.Fatalf("reoffer while held = %v", err)
	}
	first.drainAgentSendQueue(name)
	if got := firstSeat.sentIDs(); len(got) != 1 {
		t.Fatalf("held id executed twice: %v", got)
	}

	// A second daemon reads the same queue. Its sender has not executed
	// anything. The receipt is still absent, so the held row stays held.
	restartedSeat := &replicaSeat{t: t, seat: name, session: session, text: text, lane: lane}
	restarted, _, _ := t418Daemon(t, dir)
	wire(restarted, restartedSeat)
	restarted.drainAgentSendQueue(name)
	if got := restartedSeat.sentIDs(); len(got) != 0 {
		t.Fatalf("restart replayed the held request: %v", got)
	}
	if got := firstSeat.sentIDs(); len(got) != 1 {
		t.Fatalf("original execution changed across restart: %v", got)
	}
	_, _, err = restarted.sendQueue().AppendWithRequestID(name, text, hostID, time.Now())
	dup = nil
	if !errors.As(err, &dup) || dup.RequestID != hostID {
		t.Fatalf("reoffer after restart = %v", err)
	}
	held, err = restarted.sendQueue().Snapshot(name)
	if err != nil || len(held) != 1 || held[0].State != sendq.Uncertain || held[0].TransportRequestID != hostID {
		t.Fatalf("restart lost the held host id: %+v %v", held, err)
	}

	// The receipt arrives after the crash, through the rebound plane. Prose
	// that says unauthorized is not a typed cause. Settlement removes the
	// obligation and does not send again.
	lane.PublishReceipt(claudia.RequestReceipt{
		RequestID: hostID, SessionID: session, Seat: name,
		State: claudia.ReceiptCompleted, AttemptID: "attempt-" + hostID,
		TurnIDs: []string{"turn-A"}, Reason: "401 unauthorized",
		AcceptedAt: "2026-10-19T00:00:00.000000000Z",
		UpdatedAt:  "2026-10-19T00:00:00.000000000Z",
	})
	restarted.drainAgentSendQueue(name)
	if got := restartedSeat.sentIDs(); len(got) != 0 {
		t.Fatalf("settlement sent the crashed request: %v", got)
	}
	if held, err = restarted.sendQueue().Snapshot(name); err != nil || len(held) != 0 {
		t.Fatalf("completed receipt left the queue held: %+v %v", held, err)
	}

	// Steer of the same text is a second request. Its receipt is accepted
	// and carries no cause.
	restartedSeat.arm(true, claudia.ReceiptAccepted, "", "folded", nil)
	res, err := deliverObservedToSenderMode(restarted, name, text, delivery.ModeSteer, restartedSeat, false, confirmHere)
	if err != nil || res.Status != "steered" {
		t.Fatalf("steer: status=%q err=%v", res.Status, err)
	}
	steered := restartedSeat.sentIDs()
	if len(steered) != 1 || steered[0] == hostID || !strings.HasPrefix(steered[0], "sendq:") {
		t.Fatalf("steer id = %v", steered)
	}
	if held, err = restarted.sendQueue().Snapshot(name); err != nil || len(held) != 0 {
		t.Fatalf("steer left the queue: %+v %v", held, err)
	}

	// Later request C repeats the text under a new id and fails with a typed
	// auth cause. The prose on A does not move onto C.
	restartedSeat.arm(true, claudia.ReceiptFailed, claudia.ReceiptCauseAuth, "provider stopped", []string{"turn-C"})
	if _, _, err := restarted.sendQueue().Append(name, text, time.Now()); err != nil {
		t.Fatal(err)
	}
	restarted.drainAgentSendQueue(name)
	sent := restartedSeat.sentIDs()
	if len(sent) != 2 || sent[1] == hostID || sent[1] == sent[0] || !strings.HasPrefix(sent[1], "sendq:") {
		t.Fatalf("later request ids = %v", sent)
	}
	if held, err = restarted.sendQueue().Snapshot(name); err != nil || len(held) != 0 {
		t.Fatalf("later request left the queue: %+v %v", held, err)
	}
	steerID, laterID := sent[0], sent[1]

	events, err := eventlog.Tail(journal.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
	if err != nil || len(events) != 3 {
		t.Fatalf("journal: %+v %v", events, err)
	}
	byID := map[string]map[string]any{}
	for _, ev := range events {
		id, _ := ev.Fields["transport_request_id"].(string)
		byID[id] = ev.Fields
	}
	assertCause := func(id, wantCause, wantReason, wantState, wantOwner string) {
		t.Helper()
		fields := byID[id]
		if fields == nil {
			t.Fatalf("journal missing %s", id)
		}
		if fields["receipt_cause"] != wantCause || fields["owner_request_id"] != wantOwner {
			t.Fatalf("id %s cause/owner: %+v", id, fields)
		}
		body, _ := fields["receipt"].(map[string]any)
		gotCause, _ := body["cause"].(string)
		if body["reason"] != wantReason || body["state"] != wantState || body["request_id"] != id || gotCause != wantCause {
			t.Fatalf("id %s receipt: %+v", id, body)
		}
	}
	assertCause(hostID, "", "401 unauthorized", string(claudiawire.ReceiptCompleted), hostID)
	assertCause(steerID, "", "folded", string(claudiawire.ReceiptAccepted), "")
	assertCause(laterID, string(claudiawire.ReceiptCauseAuth), "provider stopped", string(claudiawire.ReceiptFailed), "")

	// Reconnect closes the plane and binds it again. The same three receipts
	// are still the production read. Publishing A again upserts that key and
	// does not execute it.
	wire(restarted, restartedSeat)
	lookup := func(id string) *claudiawire.RequestReceipt {
		t.Helper()
		got, err := restarted.LookupHeldReceipt(ctx, claudiawire.ReceiptQuery{
			Seat: name, SessionID: session, RequestID: id,
		})
		if err != nil {
			t.Fatalf("lookup %s: %v", id, err)
		}
		return got
	}
	want := func(id string, state claudia.ReceiptState, cause claudia.ReceiptCause, reason, attempt string, turns int) {
		t.Helper()
		got := lookup(id)
		if got.RequestID != id || got.SessionID != session || got.Seat != name ||
			got.State != state || got.Cause != cause || got.Reason != reason ||
			got.AttemptID != attempt || len(got.TurnIDs) != turns {
			t.Fatalf("lookup %s = %+v", id, got)
		}
		again := lookup(id)
		if again.RequestID != got.RequestID || again.State != got.State || again.AttemptID != got.AttemptID || again.UpdatedAt != got.UpdatedAt {
			t.Fatalf("second lookup changed %s: %+v then %+v", id, got, again)
		}
	}
	want(hostID, claudia.ReceiptCompleted, "", "401 unauthorized", "attempt-"+hostID, 1)
	want(steerID, claudia.ReceiptAccepted, "", "folded", "attempt-"+steerID, 0)
	want(laterID, claudia.ReceiptFailed, claudia.ReceiptCauseAuth, "provider stopped", "attempt-"+laterID, 1)

	lane.PublishReceipt(claudia.RequestReceipt{
		RequestID: hostID, SessionID: session, Seat: name,
		State: claudia.ReceiptCompleted, AttemptID: "attempt-" + hostID,
		TurnIDs: []string{"turn-A"}, Reason: "401 unauthorized",
		AcceptedAt: "2026-10-19T00:00:00.000000000Z",
		UpdatedAt:  "2026-10-19T00:00:01.000000000Z",
	})
	updated := lookup(hostID)
	if updated.UpdatedAt != "2026-10-19T00:00:01.000000000Z" || updated.State != claudia.ReceiptCompleted || updated.AttemptID != "attempt-"+hostID {
		t.Fatalf("republish of %s changed the logical receipt: %+v", hostID, updated)
	}
	if got := restartedSeat.sentIDs(); len(got) != 2 {
		t.Fatalf("republish executed again: %v", got)
	}
	if got := firstSeat.sentIDs(); len(got) != 1 || got[0] != hostID {
		t.Fatalf("host id executions = %v", got)
	}
}
