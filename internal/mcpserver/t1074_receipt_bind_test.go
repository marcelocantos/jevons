// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
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
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// bindProbe is a seat that can carry a host request id. An untagged send
// fails the test: the production handoff must use the bound transport id.
type bindProbe struct {
	t     *testing.T
	check func(text, id string)
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
	p.mu.Unlock()
	return nil
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
	f.s.drainAgentSendQueue(f.name)
	if transportID == "" || len(f.probe.sentIDs()) != 1 {
		t.Fatalf("drain did not hand off one bound id: %q %v", transportID, f.probe.sentIDs())
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 0 {
		t.Fatalf("confirmed handoff left the queue: %+v %v", entries, err)
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
	if len(f.probe.sentIDs()) != 2 {
		t.Fatalf("ids=%v", f.probe.sentIDs())
	}
}

func TestT1074EscalationBindsBeforeHandoff(t *testing.T) {
	f := newBindFixture(t, "T1074")
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
	if len(f.probe.sentIDs()) != 1 || !strings.HasPrefix(f.probe.sentIDs()[0], "sendq:") {
		t.Fatalf("ids=%v", f.probe.sentIDs())
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
