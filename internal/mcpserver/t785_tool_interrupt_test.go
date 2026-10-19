// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// seedToolInFlight marks name with a fresh ACP tool_call_id the way a long
// bin/gate progress event would (🎯T785 specimens jv-t762 / jv-t763).
func seedToolInFlight(s *Server, name, toolID string, age time.Duration) {
	if s.idleActivity == nil {
		s.idleActivity = NewIdleActivityTracker()
	}
	tr := s.idleActivity
	t0 := time.Unix(1_700_000_000, 0)
	now := t0
	tr.now = func() time.Time { return now }
	raw := []byte(`{"type":"progress","progress_type":"tool_use","raw":{"update":{"sessionUpdate":"tool_call_update","status":"in_progress","toolCallId":"` + toolID + `"}}}`)
	ev := claudia.Event{Type: "progress", ProgressType: "tool_use", Raw: raw}
	tr.Observe(name, ev)
	if age > 0 {
		now = t0.Add(age)
		// heartbeat keeps Updated fresh but not ToolCallSince
		tr.Observe(name, ev)
	}
	s.Seats().Observe(seatstate.Observation{
		Name: name, Alive: seatstate.Yes, QueueDepth: seatstate.QueueUnknown, Source: "test.t785",
	})
}

// 🎯T785 acceptance 1 — fleet submit while tool mid-flight is queued; the
// process is never interrupted and never offered the text.
func TestT785FleetSubmitQueuesWhileToolInFlight(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	const name = "jv-t762-dropped-spawn-half"
	seedToolInFlight(s, name, "tool_gate_bin", 5*time.Second)

	fs := &fakeSender{alive: true, inFlight: false} // flight ledger idle; tool id is the signal
	res, err := deliverObservedToSenderMode(s, name, "fleet paste after gate", delivery.ModeSubmit, fs, false, confirmByCaller)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Status != "queued" {
		t.Fatalf("status=%q want queued (🎯T785)", res.Status)
	}
	if fs.interrupts != 0 {
		t.Fatalf("interrupts=%d — running tool must not be cancelled", fs.interrupts)
	}
	if len(fs.sent) != 0 {
		t.Fatalf("sent=%v — must not offer text to the provider mid-tool", fs.sent)
	}
	if !strings.Contains(res.Message, "T785") || !strings.Contains(res.Message, "tool_gate_bin") {
		t.Fatalf("message should name the tool and T785, got %q", res.Message)
	}
}

// 🎯T785 — fleet mode=interrupt against a fresh tool is demoted and queued
// (specimen: interrupt ~1s before fleet paste cancelled the gate).
func TestT785FleetInterruptDemotedWhileToolFresh(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	const name = "jv-t763-specimen"
	if err := s.registry.Register(claudia.AgentDef{
		Name: name, Parent: "jevons-po", Purpose: claudia.PurposeWork, AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	seedToolInFlight(s, name, "tool_bin_gate", 10*time.Second)

	fs := &fakeSender{alive: true, inFlight: true, afterInterruptClears: true}
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) {
		return fs, false, nil
	})

	res, err := s.deliverByNameAs("jevons-po", name, "RECOVER unstick paste", OriginAgent, true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Status != "queued" {
		t.Fatalf("status=%q want queued after demotion (🎯T785)", res.Status)
	}
	if fs.interrupts != 0 {
		t.Fatalf("interrupts=%d — fleet interrupt demoted, tool must keep running", fs.interrupts)
	}
	if len(fs.sent) != 0 {
		t.Fatalf("sent=%v", fs.sent)
	}
}

// 🎯T785 acceptance 2 — when a fleet interrupt does proceed (tool past the
// stuck bar), the payload carries the in-band notice.
func TestT785FleetInterruptNoticeWhenToolAged(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	const name = "jv-t762-aged"
	if err := s.registry.Register(claudia.AgentDef{
		Name: name, Parent: "jevons-po", Purpose: claudia.PurposeWork, AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	// Past DefaultFleetStuckTimeout — demotion must not apply.
	seedToolInFlight(s, name, "tool_stuck", DefaultFleetStuckTimeout+time.Minute)

	fs := &fakeSender{alive: true, inFlight: true, afterInterruptClears: true}
	s.SetTurnWitness(witnessYielding(TurnEvidence{Observed: true, PayloadSeen: true}))
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) {
		return fs, false, nil
	})

	res, err := s.deliverByNameAs("jevons-po", name, "RECOVER (unstick)", OriginAgent, true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if fs.interrupts != 1 {
		t.Fatalf("interrupts=%d want 1 for aged tool", fs.interrupts)
	}
	if len(fs.sent) != 1 {
		t.Fatalf("sent=%v", fs.sent)
	}
	if !strings.Contains(fs.sent[0], "[fleet interrupt 🎯T785]") {
		t.Fatalf("fleet interrupt must label the cut in-band, got:\n%s", fs.sent[0])
	}
	if !strings.Contains(fs.sent[0], "NOT by the owner") {
		t.Fatalf("notice must say not owner rejection:\n%s", fs.sent[0])
	}
	if res.Status != "interrupted_sent" && res.Status != "sent" {
		// interrupted_sent is preferred; either means the text landed after cut
		t.Fatalf("status=%q", res.Status)
	}
}

// 🎯T785 — owner mode=interrupt still cuts a fresh tool (🎯T711 is not demoted).
func TestT785OwnerInterruptStillCutsFreshTool(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	const name = "jevons-po"
	if err := s.registry.Register(claudia.AgentDef{
		Name: name, Purpose: claudia.PurposeWork, AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	seedToolInFlight(s, name, "tool_owner_cut", 5*time.Second)

	fs := &fakeSender{alive: true, inFlight: true, afterInterruptClears: true}
	s.SetTurnWitness(witnessYielding(TurnEvidence{Observed: true, PayloadSeen: true}))
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) {
		return fs, false, nil
	})

	res, err := s.deliverByNameAs(ActorOwnerSurface, name, "owner: stop that", OriginOwner, true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if fs.interrupts != 1 {
		t.Fatalf("owner interrupt must cut: interrupts=%d", fs.interrupts)
	}
	if len(fs.sent) != 1 {
		t.Fatalf("sent=%v", fs.sent)
	}
	if strings.Contains(fs.sent[0], "[fleet interrupt 🎯T785]") {
		t.Fatalf("owner path must not carry the fleet notice:\n%s", fs.sent[0])
	}
	if res.Status != "interrupted_sent" {
		t.Fatalf("status=%q want interrupted_sent", res.Status)
	}
}

func TestT785NoticeIdempotent(t *testing.T) {
	once := withFleetInterruptNotice("hello")
	twice := withFleetInterruptNotice(once)
	if strings.Count(twice, "[fleet interrupt 🎯T785]") != 1 {
		t.Fatalf("notice stacked:\n%s", twice)
	}
}

func TestT785ToolCallFresh(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	if s.toolCallFresh("nobody") {
		t.Fatal("unknown seat is not fresh")
	}
	seedToolInFlight(s, "w", "t1", 0)
	if !s.toolCallFresh("w") {
		t.Fatal("just-started tool must be fresh")
	}
	seedToolInFlight(s, "w2", "t2", DefaultFleetStuckTimeout+time.Second)
	if s.toolCallFresh("w2") {
		t.Fatal("aged tool must not be fresh")
	}
}
