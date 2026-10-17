// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/escalate"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// bindProbe is a seat that can carry a host request id. An untagged send
// fails the test: the production handoff must use the bound transport id.
type bindProbe struct {
	t     *testing.T
	check func(text, id string)
	fail  error
	mu    sync.Mutex
	sent  []string
	ids   []string
}

func (p *bindProbe) Alive() bool      { return true }
func (p *bindProbe) Interrupt() error { return nil }
func (p *bindProbe) Send(string) error {
	p.t.Errorf("untagged Send")
	return fmt.Errorf("untagged Send")
}
func (p *bindProbe) SendWithRequestID(text, id string) error {
	if p.check != nil {
		p.check(text, id)
	}
	p.mu.Lock()
	p.sent = append(p.sent, text)
	p.ids = append(p.ids, id)
	fail := p.fail
	p.mu.Unlock()
	return fail
}
func (p *bindProbe) SendModeString(string, string) (string, string, error) {
	p.t.Errorf("untagged steer")
	return "", "", fmt.Errorf("untagged steer")
}
func (p *bindProbe) SendModeWithRequestIDString(text, mode, id string) (string, string, error) {
	if p.check != nil {
		p.check(text, id)
	}
	p.mu.Lock()
	p.sent = append(p.sent, text)
	p.ids = append(p.ids, id)
	p.mu.Unlock()
	return "steer", delivery.PhaseInTurn, nil
}
func (p *bindProbe) SendEscalating(string, escalate.Ladder) (claudia.DeliveryOutcome, error) {
	p.t.Errorf("untagged escalation")
	return claudia.DeliveryOutcome{}, fmt.Errorf("untagged escalation")
}
func (p *bindProbe) SendEscalatingWithRequestID(text string, _ escalate.Ladder, id string) (claudia.DeliveryOutcome, error) {
	if p.check != nil {
		p.check(text, id)
	}
	p.mu.Lock()
	p.sent = append(p.sent, text)
	p.ids = append(p.ids, id)
	p.mu.Unlock()
	return claudia.DeliveryOutcome{Mechanism: "steer"}, nil
}

func (p *bindProbe) sentIDs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.ids...)
}

type bindFixture struct {
	s       *Server
	probe   *bindProbe
	usage   *cost.Store
	name    string
	session string
}

func newBindFixture(t *testing.T, mission string) *bindFixture {
	t.Helper()
	const name = "jv-t1074-bind"
	dir := t.TempDir()
	s, _, _ := t418Daemon(t, dir)
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const session = "gen-1"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: session, TargetID: mission,
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s.registry = reg
	usage, err := cost.OpenStore(filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = usage.Close() })
	s.SetRequestMissionRecorder(usage.RecordRequestMission)
	probe := &bindProbe{t: t}
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return probe, false, nil })
	return &bindFixture{s: s, probe: probe, usage: usage, name: name, session: session}
}

func (f *bindFixture) assertBoundBeforeSend(t *testing.T, text, id, mission string) {
	t.Helper()
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("queue at handoff = %+v", entries)
	}
	e := entries[0]
	if e.State != sendq.Attempting || e.SessionID != f.session || e.TransportRequestID != id || id == "" || !strings.HasPrefix(id, "sendq:") {
		t.Fatalf("receipt not bound before send: id=%q entry=%+v", id, e)
	}
	if e.RequestID != "" {
		t.Fatalf("ordinary admission gained an owner request id: %+v", e)
	}
	if e.WirePayload() != text {
		t.Fatalf("wire payload = %q, want %q", e.WirePayload(), text)
	}
	got, ok, err := f.usage.RequestMission(context.Background(), f.name, id)
	if err != nil || !ok || got != mission {
		t.Fatalf("mission at handoff = %q ok=%v err=%v, want %q", got, ok, err, mission)
	}
}

func TestT1074BlockedDrainDoesNotBind(t *testing.T) {
	f := newBindFixture(t, "T1074")
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.s.SetFleetIntentStore(store)
	const payload = "idle-nudge: continue the mission"
	if _, _, err := f.s.sendQueue().Append(f.name, payload, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetFleetIntent(fleetintent.BlockedOwner, "owner", "stabilization pause"); err != nil {
		t.Fatal(err)
	}
	f.s.drainAgentSendQueue(f.name)
	if ids := f.probe.sentIDs(); len(ids) != 0 {
		t.Fatalf("blocked drain handed off: %v", ids)
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Pending || entries[0].TransportRequestID != "" || entries[0].SessionID != "" {
		t.Fatalf("blocked entry was bound or dropped: %+v %v", entries, err)
	}
	if _, ok, err := f.usage.RequestMission(context.Background(), f.name, "sendq:"+entries[0].ID); err != nil || ok {
		t.Fatalf("blocked drain froze a mission: ok=%v err=%v", ok, err)
	}
}

func TestT1074DrainBindsReceiptAndMissionBeforeHandoff(t *testing.T) {
	f := newBindFixture(t, "T1074")
	const payload = "land the receipt before the model sees it"
	if _, _, err := f.s.sendQueue().Append(f.name, payload, time.Now()); err != nil {
		t.Fatal(err)
	}
	var transportID string
	f.probe.check = func(text, id string) {
		if text != payload {
			t.Fatalf("sent %q", text)
		}
		f.assertBoundBeforeSend(t, payload, id, "T1074")
		transportID = id
	}
	// t418Daemon's witness reports PayloadSeen for every drain. A bound
	// request must stay held until its own receipt settles it.
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		return heldReceipt(q, claudia.ReceiptUnknown), nil
	})
	f.s.drainAgentSendQueue(f.name)
	if transportID == "" || len(f.probe.sentIDs()) != 1 {
		t.Fatalf("drain did not hand off one bound id: %q %v", transportID, f.probe.sentIDs())
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Uncertain || entries[0].TransportRequestID != transportID {
		t.Fatalf("PayloadSeen confirmed a bound drain: %+v %v", entries, err)
	}
	receiptJournal(t, f.s, t.TempDir())
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		if q.RequestID != transportID {
			t.Fatalf("recovery queried %q, want %q", q.RequestID, transportID)
		}
		return heldReceipt(q, claudia.ReceiptAccepted), nil
	})
	f.s.drainAgentSendQueue(f.name)
	if len(f.probe.sentIDs()) != 1 {
		t.Fatalf("receipt recovery sent again: %v", f.probe.sentIDs())
	}
	entries, err = f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 0 {
		t.Fatalf("accepted receipt left the queue: %+v %v", entries, err)
	}

	input, output := int64(11), int64(3)
	now := time.Now().UTC()
	fact := claudia.UsageFact{
		Seat: f.name, SessionID: f.session, CallID: "call-1", Seq: 2,
		Kind: "model", State: "completed", Provider: "fixture", Model: "fixture-model",
		TurnID: "turn", Requests: []claudia.UsageRequestRef{{RequestID: transportID, AttemptID: "attempt-1"}},
		StartedAt: now.Format(time.RFC3339Nano), FinishedAt: now.Add(time.Second).Format(time.RFC3339Nano),
		Counters:      claudia.UsageCounters{InputTokens: &input, OutputTokens: &output},
		CounterSource: "adapter_normalized_positive", Coverage: "engine_invocation",
	}
	ctx := context.Background()
	if err := f.usage.IngestUsageFact(ctx, fact.Seat, fact.SessionID, &fact); err != nil {
		t.Fatal(err)
	}
	if err := f.usage.IngestUsageFact(ctx, fact.Seat, fact.SessionID, &fact); err != nil {
		t.Fatal(err)
	}
	mission, ok, err := f.usage.UsageMission(ctx, &fact)
	if err != nil || !ok || mission != "T1074" {
		t.Fatalf("mission=%q ok=%v err=%v", mission, ok, err)
	}
	usage, err := f.usage.StructuredUsage(ctx, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil || usage.Calls != 1 || usage.Known.Input != 11 || usage.Known.Output != 3 {
		t.Fatalf("replay changed consumption: %+v %v", usage, err)
	}

	d := *f.s.registry.Def(f.name)
	d.TargetID = "T9999"
	if err := f.s.registry.Register(d); err != nil {
		t.Fatal(err)
	}
	mission, ok, err = f.usage.UsageMission(ctx, &fact)
	if err != nil || !ok || mission != "T1074" {
		t.Fatalf("later target rewrote the frozen mission: %q %v %v", mission, ok, err)
	}
}

func TestT1074ImmediateAndSteerBindBeforeHandoff(t *testing.T) {
	f := newBindFixture(t, "T1074")
	journal := receiptJournal(t, f.s, t.TempDir())
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		return heldReceipt(q, claudia.ReceiptAccepted), nil
	})
	const payload = "owner: send this now"
	f.probe.check = func(text, id string) {
		if text != payload {
			t.Fatalf("sent %q", text)
		}
		f.assertBoundBeforeSend(t, payload, id, "T1074")
	}
	res, err := deliverObservedToSender(f.s, f.name, payload, false, f.probe, false)
	if err != nil || res.Status != "sent" {
		t.Fatalf("immediate send: status=%q err=%v", res.Status, err)
	}
	if entries, err := f.s.sendQueue().Snapshot(f.name); err != nil || len(entries) != 0 {
		t.Fatalf("immediate send left the queue: %+v %v", entries, err)
	}

	const steered = "owner: fold this into the turn"
	f.probe.check = func(text, id string) {
		if text != steered {
			t.Fatalf("steered %q", text)
		}
		f.assertBoundBeforeSend(t, steered, id, "T1074")
	}
	res, err = deliverObservedToSenderMode(f.s, f.name, steered, delivery.ModeSteer, f.probe, false, confirmHere)
	if err != nil || res.Status != "steered" {
		t.Fatalf("steer: status=%q err=%v message=%q", res.Status, err, res.Message)
	}
	ids := f.probe.sentIDs()
	if len(ids) != 2 {
		t.Fatalf("ids=%v", ids)
	}
	events, err := eventlog.Tail(journal.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
	if err != nil || len(events) != 2 {
		t.Fatalf("journal: %+v %v", events, err)
	}
	seen := map[string]bool{}
	for _, ev := range events {
		id, _ := ev.Fields["transport_request_id"].(string)
		if ev.Fields["receipt_cause"] != "" {
			t.Fatalf("accepted receipt invented a cause: %+v", ev.Fields)
		}
		seen[id] = true
	}
	if !seen[ids[0]] || !seen[ids[1]] {
		t.Fatalf("journal missed a handoff: ids=%v seen=%v", ids, seen)
	}
}

func TestT1074EscalationBindsBeforeHandoff(t *testing.T) {
	f := newBindFixture(t, "T1074")
	journal := receiptJournal(t, f.s, t.TempDir())
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		return heldReceipt(q, claudia.ReceiptAccepted), nil
	})
	observeSenderFixture(f.s, f.name, f.probe)
	f.s.noteTurnInFlight(f.name)
	const payload = "owner: steer the running turn"
	f.probe.check = func(text, id string) {
		if text != payload {
			t.Fatalf("escalated %q", text)
		}
		f.assertBoundBeforeSend(t, payload, id, "T1074")
	}
	res, handled, err := f.s.escalateIfBusyWithRequestID(f.name, payload, config.EscalationOwner, "owner", f.probe, "")
	if err != nil || !handled || res.Status != "steered" {
		t.Fatalf("handled=%v status=%q err=%v message=%q", handled, res.Status, err, res.Message)
	}
	if entries, err := f.s.sendQueue().Snapshot(f.name); err != nil || len(entries) != 0 {
		t.Fatalf("escalation left the queue: %+v %v", entries, err)
	}
	ids := f.probe.sentIDs()
	if len(ids) != 1 || !strings.HasPrefix(ids[0], "sendq:") {
		t.Fatalf("ids=%v", ids)
	}
	events, err := eventlog.Tail(journal.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
	if err != nil || len(events) != 1 || events[0].Fields["transport_request_id"] != ids[0] || events[0].Fields["receipt_cause"] != "" {
		t.Fatalf("journal: %+v %v", events, err)
	}
}

func TestT1074ImmediateHoldDoesNotBind(t *testing.T) {
	f := newBindFixture(t, "T1074")
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.s.SetFleetIntentStore(store)
	if err := f.s.SetFleetIntent(fleetintent.BlockedOwner, "owner", "stabilization pause"); err != nil {
		t.Fatal(err)
	}
	const payload = "idle-nudge: continue the mission"
	res, err := deliverObservedToSender(f.s, f.name, payload, false, f.probe, false)
	if err != nil || res.Status != "queued" {
		t.Fatalf("status=%q err=%v", res.Status, err)
	}
	if len(f.probe.sentIDs()) != 0 {
		t.Fatalf("held send handed off: %v", f.probe.sentIDs())
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Pending || entries[0].TransportRequestID != "" || entries[0].Text != payload {
		t.Fatalf("held entry = %+v %v", entries, err)
	}
}

// capableGapServer is a request-id-capable seat with no receiving generation.
// bindProbe.Send fails the test, so a historical handoff cannot pass quietly.
func capableGapServer(t *testing.T, mode string) (*Server, *bindProbe, string) {
	t.Helper()
	const name = "jv-t1074-gap"
	dir := t.TempDir()
	s, _, _ := t418Daemon(t, dir)
	probe := &bindProbe{t: t}
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return probe, false, nil })
	switch mode {
	case "no-registry":
	case "no-def":
		reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
		if err != nil {
			t.Fatal(err)
		}
		s.registry = reg
	case "empty-session":
		path := filepath.Join(dir, "agents.json")
		body, err := json.Marshal([]claudia.AgentDef{{
			Name: name, WorkDir: dir, Provider: "grok",
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
		reg, err := claudia.NewRegistry(path)
		if err != nil {
			t.Fatal(err)
		}
		d := reg.Def(name)
		if d == nil || strings.TrimSpace(d.SessionID) != "" {
			t.Fatalf("empty-session fixture = %+v", d)
		}
		s.registry = reg
	default:
		t.Fatalf("unknown gap %q", mode)
	}
	return s, probe, name
}

func TestT1074CapableSendWithoutGenerationDoesNotSend(t *testing.T) {
	const payload = "owner: this must not reach the model untagged"
	for _, mode := range []string{"no-registry", "no-def", "empty-session"} {
		t.Run(mode, func(t *testing.T) {
			t.Run("drain", func(t *testing.T) {
				s, probe, name := capableGapServer(t, mode)
				if _, _, err := s.sendQueue().Append(name, payload, time.Now()); err != nil {
					t.Fatal(err)
				}
				s.drainAgentSendQueue(name)
				if len(probe.sentIDs()) != 0 {
					t.Fatalf("drain handed off: %v", probe.sentIDs())
				}
				entries, err := s.sendQueue().Snapshot(name)
				if err != nil || len(entries) != 1 || entries[0].State != sendq.Pending || entries[0].TransportRequestID != "" || entries[0].SessionID != "" {
					t.Fatalf("unbound drain entry = %+v %v", entries, err)
				}
			})
			t.Run("immediate", func(t *testing.T) {
				s, probe, name := capableGapServer(t, mode)
				_, err := deliverObservedToSender(s, name, payload, false, probe, false)
				if err == nil || !strings.Contains(err.Error(), "no receiving generation") {
					t.Fatalf("immediate err = %v", err)
				}
				if len(probe.sentIDs()) != 0 {
					t.Fatalf("immediate handed off: %v", probe.sentIDs())
				}
				entries, serr := s.sendQueue().Snapshot(name)
				if serr != nil || len(entries) != 0 {
					t.Fatalf("immediate left a queue entry: %+v %v", entries, serr)
				}
			})
			t.Run("steer", func(t *testing.T) {
				s, probe, name := capableGapServer(t, mode)
				_, err := deliverObservedToSenderMode(s, name, payload, delivery.ModeSteer, probe, false, confirmHere)
				if err == nil || !strings.Contains(err.Error(), "no receiving generation") {
					t.Fatalf("steer err = %v", err)
				}
				if len(probe.sentIDs()) != 0 {
					t.Fatalf("steer handed off: %v", probe.sentIDs())
				}
			})
			t.Run("escalation", func(t *testing.T) {
				s, probe, name := capableGapServer(t, mode)
				observeSenderFixture(s, name, probe)
				s.noteTurnInFlight(name)
				_, handled, err := s.escalateIfBusyWithRequestID(name, payload, config.EscalationOwner, "owner", probe, "")
				if err == nil || !handled || !strings.Contains(err.Error(), "no receiving generation") {
					t.Fatalf("handled=%v err=%v", handled, err)
				}
				if len(probe.sentIDs()) != 0 {
					t.Fatalf("escalation handed off: %v", probe.sentIDs())
				}
			})
			t.Run("cursor-brief", func(t *testing.T) {
				s, probe, name := capableGapServer(t, mode)
				err := s.submitCursorStartBrief(name, payload)
				if err == nil {
					t.Fatal("cursor brief sent without a generation")
				}
				if mode == "no-registry" {
					if !strings.Contains(err.Error(), "no agent registry") {
						t.Fatalf("cursor err = %v", err)
					}
				} else if !strings.Contains(err.Error(), "no receiving generation") {
					t.Fatalf("cursor err = %v", err)
				}
				if len(probe.sentIDs()) != 0 {
					t.Fatalf("cursor brief handed off: %v", probe.sentIDs())
				}
			})
		})
	}
}

func TestT1074CursorStartBindsBeforeHandoff(t *testing.T) {
	f := newBindFixture(t, "T1074")
	journal := receiptJournal(t, f.s, t.TempDir())
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		return heldReceipt(q, claudia.ReceiptAccepted), nil
	})
	const prompt = "Execute the cursor brief"
	f.probe.check = func(text, id string) {
		if text == prompt || !strings.Contains(text, prompt) || !strings.Contains(text, "Jevons fleet standing brief") {
			t.Fatalf("cursor start sent an uncomposed brief: %q", text)
		}
		f.assertBoundBeforeSend(t, text, id, "T1074")
	}
	if err := f.s.submitCursorStartBrief(f.name, prompt); err != nil {
		t.Fatal(err)
	}
	ids := f.probe.sentIDs()
	if len(ids) != 1 {
		t.Fatalf("ids=%v", ids)
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 0 {
		t.Fatalf("confirmed brief left the queue: %+v %v", entries, err)
	}
	events, err := eventlog.Tail(journal.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
	if err != nil || len(events) != 1 || events[0].Fields["transport_request_id"] != ids[0] || events[0].Fields["receipt_cause"] != "" {
		t.Fatalf("journal: %+v %v", events, err)
	}
}

// TestT1074EscalationNilErrorDoesNotConfirm is a capable escalation whose
// ladder returns nil. The receipt is unknown, so the caller hears
// delivered_unconfirmed, the queue stays uncertain, and nothing is journaled.
func TestT1074EscalationNilErrorDoesNotConfirm(t *testing.T) {
	f := newBindFixture(t, "T1074")
	journal := receiptJournal(t, f.s, t.TempDir())
	observeSenderFixture(f.s, f.name, f.probe)
	f.s.noteTurnInFlight(f.name)
	const payload = "owner: nil ladder write is not acceptance"
	var transportID string
	f.probe.check = func(text, id string) {
		if text != payload {
			t.Fatalf("escalated %q", text)
		}
		transportID = id
	}
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		if transportID == "" || q.RequestID != transportID {
			t.Fatalf("queried %q, handoff %q", q.RequestID, transportID)
		}
		r := heldReceipt(q, claudia.ReceiptUnknown)
		r.Reason = "not_found"
		r.Cause = ""
		return r, nil
	})
	res, handled, err := f.s.escalateIfBusyWithRequestID(f.name, payload, config.EscalationOwner, "owner", f.probe, "")
	if err != nil || !handled || res.Status != "delivered_unconfirmed" {
		t.Fatalf("handled=%v status=%q err=%v", handled, res.Status, err)
	}
	if _, held := f.s.pendingUnconfirmedSend(f.name, time.Now()); !held {
		t.Fatal("unsettled escalation receipt was not remembered as undecided")
	}
	if len(f.probe.sentIDs()) != 1 || transportID == "" {
		t.Fatalf("sends=%v", f.probe.sentIDs())
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Uncertain || entries[0].TransportRequestID != transportID {
		t.Fatalf("nil escalation confirmed: %+v %v", entries, err)
	}
	events, err := eventlog.Tail(journal.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
	if err != nil || len(events) != 0 {
		t.Fatalf("unknown escalation receipt was journaled: %+v %v", events, err)
	}
}

// TestT1074EscalationProseReasonDoesNotInventCause settles a completed
// escalation receipt whose Reason names an auth failure and whose typed
// cause is empty.
func TestT1074EscalationProseReasonDoesNotInventCause(t *testing.T) {
	f := newBindFixture(t, "T1074")
	journal := receiptJournal(t, f.s, t.TempDir())
	observeSenderFixture(f.s, f.name, f.probe)
	f.s.noteTurnInFlight(f.name)
	const payload = "owner: escalation prose is not a cause"
	var transportID string
	f.probe.check = func(text, id string) {
		transportID = id
	}
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		if transportID == "" || q.RequestID != transportID {
			t.Fatalf("queried %q, handoff %q", q.RequestID, transportID)
		}
		r := heldReceipt(q, claudia.ReceiptCompleted)
		r.Reason = "401 unauthorized"
		r.Cause = ""
		r.TurnIDs = []string{"turn-escalation"}
		return r, nil
	})
	res, handled, err := f.s.escalateIfBusyWithRequestID(f.name, payload, config.EscalationOwner, "owner", f.probe, "")
	if err != nil || !handled || res.Status != "steered" {
		t.Fatalf("handled=%v status=%q err=%v", handled, res.Status, err)
	}
	if entries, err := f.s.sendQueue().Snapshot(f.name); err != nil || len(entries) != 0 {
		t.Fatalf("settling escalation left the queue: %+v %v", entries, err)
	}
	if _, held := f.s.pendingUnconfirmedSend(f.name, time.Now()); held {
		t.Fatal("a settling escalation receipt stayed undecided")
	}
	events, err := eventlog.Tail(journal.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
	if err != nil || len(events) != 1 {
		t.Fatalf("journal: %+v %v", events, err)
	}
	if events[0].Fields["transport_request_id"] != transportID || events[0].Fields["receipt_cause"] != "" {
		t.Fatalf("prose became a cause: %+v", events[0].Fields)
	}
	receipt, _ := events[0].Fields["receipt"].(map[string]any)
	if receipt["reason"] != "401 unauthorized" || receipt["cause"] != nil || receipt["state"] != string(claudia.ReceiptCompleted) || receipt["request_id"] != transportID {
		t.Fatalf("escalation receipt: %+v", receipt)
	}
}

// TestT1074CursorBriefNilErrorDoesNotConfirm is a capable opening brief
// whose socket write returns nil. The call still succeeds. The receipt is
// unknown, so the queue stays uncertain and nothing is journaled.
func TestT1074CursorBriefNilErrorDoesNotConfirm(t *testing.T) {
	f := newBindFixture(t, "T1074")
	journal := receiptJournal(t, f.s, t.TempDir())
	const prompt = "Execute without a receipt"
	var transportID string
	f.probe.check = func(_, id string) {
		transportID = id
	}
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		if transportID == "" || q.RequestID != transportID {
			t.Fatalf("queried %q, handoff %q", q.RequestID, transportID)
		}
		r := heldReceipt(q, claudia.ReceiptUnknown)
		r.Reason = "not_found"
		r.Cause = ""
		return r, nil
	})
	if err := f.s.submitCursorStartBrief(f.name, prompt); err != nil {
		t.Fatal(err)
	}
	if len(f.probe.sentIDs()) != 1 || transportID == "" {
		t.Fatalf("sends=%v", f.probe.sentIDs())
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Uncertain || entries[0].TransportRequestID != transportID {
		t.Fatalf("nil cursor brief confirmed: %+v %v", entries, err)
	}
	events, err := eventlog.Tail(journal.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
	if err != nil || len(events) != 0 {
		t.Fatalf("unknown cursor receipt was journaled: %+v %v", events, err)
	}
}

// TestT1074CursorBriefProseReasonDoesNotInventCause settles a completed
// opening-brief receipt whose Reason names an auth failure and whose typed
// cause is empty.
func TestT1074CursorBriefProseReasonDoesNotInventCause(t *testing.T) {
	f := newBindFixture(t, "T1074")
	journal := receiptJournal(t, f.s, t.TempDir())
	const prompt = "Execute and keep the prose out of the cause"
	var transportID string
	f.probe.check = func(_, id string) {
		transportID = id
	}
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		if transportID == "" || q.RequestID != transportID {
			t.Fatalf("queried %q, handoff %q", q.RequestID, transportID)
		}
		r := heldReceipt(q, claudia.ReceiptCompleted)
		r.Reason = "401 unauthorized"
		r.Cause = ""
		r.TurnIDs = []string{"turn-cursor"}
		return r, nil
	})
	if err := f.s.submitCursorStartBrief(f.name, prompt); err != nil {
		t.Fatal(err)
	}
	if entries, err := f.s.sendQueue().Snapshot(f.name); err != nil || len(entries) != 0 {
		t.Fatalf("settling brief left the queue: %+v %v", entries, err)
	}
	events, err := eventlog.Tail(journal.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
	if err != nil || len(events) != 1 {
		t.Fatalf("journal: %+v %v", events, err)
	}
	if events[0].Fields["transport_request_id"] != transportID || events[0].Fields["receipt_cause"] != "" {
		t.Fatalf("prose became a cause: %+v", events[0].Fields)
	}
	receipt, _ := events[0].Fields["receipt"].(map[string]any)
	if receipt["reason"] != "401 unauthorized" || receipt["cause"] != nil || receipt["state"] != string(claudia.ReceiptCompleted) || receipt["request_id"] != transportID {
		t.Fatalf("cursor receipt: %+v", receipt)
	}
}

func TestT1074EmptyIDOnCapableSeatDoesNotSend(t *testing.T) {
	probe := &bindProbe{t: t}
	err := sendWithRequestID(probe, "untagged", "")
	if !errors.Is(err, errRequestIDUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if len(probe.sentIDs()) != 0 {
		t.Fatalf("capable empty id handed off: %v", probe.sentIDs())
	}
	plain := &recordingSender{}
	if err := sendWithRequestID(plain, "historical", ""); err != nil {
		t.Fatal(err)
	}
	if got := plain.delivered(); len(got) != 1 || got[0] != "historical" {
		t.Fatalf("incapable send = %v", got)
	}
}
