// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/escalate"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

type frozenMission struct {
	seat, id, mission string
}

// overseerIDProbe is the production request-id escalator. Untagged
// SendEscalating drops the host id, so a call to it fails the test.
type overseerIDProbe struct {
	calls    []overseerIDCall
	untagged int
	before   func(id string)
}

type overseerIDCall struct {
	text, id string
}

func (p *overseerIDProbe) SendEscalating(string, escalate.Ladder) (claudia.DeliveryOutcome, error) {
	p.untagged++
	return claudia.DeliveryOutcome{}, fmt.Errorf("untagged SendEscalating")
}

func (p *overseerIDProbe) SendEscalatingWithRequestID(text string, _ escalate.Ladder, requestID string) (claudia.DeliveryOutcome, error) {
	if p.before != nil {
		p.before(requestID)
	}
	p.calls = append(p.calls, overseerIDCall{text: text, id: requestID})
	return claudia.DeliveryOutcome{Mechanism: "steer"}, nil
}

func (s *Server) setOverseerInFlight(in bool) {
	a := s.seats.Load()
	a.Observe(seatstate.Observation{
		Name:       s.overseerAgentName(),
		Alive:      seatstate.Yes,
		InFlight:   seatstate.TriOf(in),
		QueueDepth: seatstate.QueueUnknown,
		Source:     "test.t1074",
	})
}

func ownerIDsFor(s *Server, text string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.notifyOwnerIDs[text]...)
}

func TestT1074OverseerOwnerIDReachesHandoff(t *testing.T) {
	ctx := context.Background()
	mission := "T-overseer-mission"
	usage, err := cost.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = usage.Close() })
	var frozen []frozenMission
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", SessionID: "gen-overseer", TargetID: mission,
		Provider: claudia.ProviderGrok, WorkDir: dir,
	}); err != nil {
		t.Fatal(err)
	}

	probe := &overseerIDProbe{before: func(id string) {
		if !missionFrozen(frozen, "jevons", id, mission) {
			t.Errorf("mission not frozen before steer id=%q frozen=%v", id, frozen)
		}
		got, ok, qerr := usage.RequestMission(ctx, "jevons", id)
		if qerr != nil || !ok || got != mission {
			t.Errorf("stored mission before steer id=%q got=%q ok=%v err=%v", id, got, ok, qerr)
		}
	}}
	var sends []overseerIDCall
	s := &Server{}
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	s.SetRequestMissionRecorder(func(ctx context.Context, seat, requestID, got string) error {
		frozen = append(frozen, frozenMission{seat: seat, id: requestID, mission: got})
		return usage.RecordRequestMission(ctx, seat, requestID, got)
	})
	s.SetOwnerEscalation(func() (escalate.Ladder, bool) { return ownerLadder, true })
	s.SetProcess(claudia.NewStubAgent(func(string) error {
		t.Errorf("untagged Send")
		return fmt.Errorf("untagged Send")
	}))
	s.overseerEscalatorSeam = probe
	s.overseerSendSeam = func(text, id string) error {
		if !missionFrozen(frozen, s.overseerAgentName(), id, mission) {
			t.Errorf("mission not frozen before handoff id=%q frozen=%v", id, frozen)
		}
		got, ok, qerr := usage.RequestMission(ctx, s.overseerAgentName(), id)
		if qerr != nil || !ok || got != mission {
			t.Errorf("stored mission before handoff id=%q got=%q ok=%v err=%v", id, got, ok, qerr)
		}
		sends = append(sends, overseerIDCall{text: text, id: id})
		return nil
	}

	if _, err := s.sendToNamedAgentMode("jevons", "preface", sendOriginOwner, delivery.ModeSubmit); err != nil {
		t.Fatal(err)
	}
	if len(sends) != 1 || sends[0].id == "" || !strings.Contains(sends[0].text, "preface") {
		t.Fatalf("idle drain = %+v", sends)
	}
	prefaceID := sends[0].id

	// The preface took the session. Hold it so the next owner turn is
	// accepted and stays pending instead of being steered or sent.
	s.mu.Lock()
	s.waiting = true
	s.overseerOwnerTurn = false
	s.mu.Unlock()
	if _, err := s.sendToNamedAgentMode("jevons", "alpha", sendOriginOwner, delivery.ModeSubmit); err != nil {
		t.Fatal(err)
	}
	alphaWire := userTurnPrefix + "alpha"
	pending := ownerIDsFor(s, alphaWire)
	if len(pending) != 1 || pending[0] == "" || pending[0] == prefaceID {
		t.Fatalf("pending alpha ids = %v", pending)
	}
	alphaID := pending[0]
	if len(sends) != 1 {
		t.Fatalf("alpha handed off while the overseer was busy: %+v", sends)
	}

	s.setOverseerInFlight(true)
	s.mu.Lock()
	s.waiting = true
	s.overseerOwnerTurn = true
	s.mu.Unlock()
	out, err := s.sendToNamedAgentMode("jevons", "beta", sendOriginOwner, delivery.ModeSubmit)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "steered" {
		t.Fatalf("steer outcome = %+v", out)
	}
	if probe.untagged != 0 {
		t.Fatalf("untagged escalations = %d", probe.untagged)
	}
	if len(probe.calls) != 1 || probe.calls[0].id == "" || probe.calls[0].id == alphaID || probe.calls[0].id == prefaceID {
		t.Fatalf("steer = %+v", probe.calls)
	}
	betaID := probe.calls[0].id
	if !missionFrozen(frozen, s.overseerAgentName(), betaID, mission) {
		t.Fatalf("steer mission missing from %v", frozen)
	}
	if !strings.HasPrefix(probe.calls[0].text, userTurnPrefix) || !strings.HasSuffix(probe.calls[0].text, "beta") {
		t.Fatalf("steer text = %q", probe.calls[0].text)
	}
	if gotIDs := ownerIDsFor(s, alphaWire); len(gotIDs) != 1 || gotIDs[0] != alphaID {
		t.Fatalf("steer consumed pending alpha: %v", gotIDs)
	}

	// A later identical text is a second request. It must not take alpha's id.
	s.setOverseerInFlight(false)
	s.mu.Lock()
	s.waiting = true
	s.overseerOwnerTurn = false
	s.mu.Unlock()
	if _, err := s.sendToNamedAgentMode("jevons", "alpha", sendOriginOwner, delivery.ModeSubmit); err != nil {
		t.Fatal(err)
	}
	fifo := ownerIDsFor(s, alphaWire)
	if len(fifo) != 2 || fifo[0] != alphaID || fifo[1] == "" || fifo[1] == alphaID || fifo[1] == betaID {
		t.Fatalf("fifo = %v; later text inherited an earlier id", fifo)
	}
	laterID := fifo[1]
	if len(sends) != 1 {
		t.Fatalf("later alpha handed off early: %+v", sends)
	}

	s.mu.Lock()
	s.waiting = false
	s.overseerOwnerTurn = false
	s.mu.Unlock()
	if _, err := s.sendToNamedAgentMode("jevons", "gamma", sendOriginOwner, delivery.ModeSubmit); err != nil {
		t.Fatal(err)
	}
	if len(sends) != 2 || sends[1].id != alphaID || !strings.Contains(sends[1].text, "alpha") || strings.Contains(sends[1].text, "gamma") {
		t.Fatalf("drain delivered %+v, want pending alpha %s", sends, alphaID)
	}
	if gotIDs := ownerIDsFor(s, alphaWire); len(gotIDs) != 1 || gotIDs[0] != laterID {
		t.Fatalf("after drain fifo = %v, want later id %s still pending", gotIDs, laterID)
	}
}

func TestT1074OverseerMissionFreezeFailureDoesNotSend(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", SessionID: "gen-overseer", TargetID: "T-overseer-mission",
		Provider: claudia.ProviderGrok, WorkDir: dir,
	}); err != nil {
		t.Fatal(err)
	}

	probe := &overseerIDProbe{}
	var sends int
	s := &Server{}
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	s.SetRequestMissionRecorder(func(context.Context, string, string, string) error {
		return fmt.Errorf("usage mission disk full")
	})
	s.SetOwnerEscalation(func() (escalate.Ladder, bool) { return ownerLadder, true })
	s.notifyRetryDelay = time.Hour
	t.Cleanup(func() {
		s.mu.Lock()
		if s.notifyRetryTimer != nil {
			s.notifyRetryTimer.Stop()
		}
		s.mu.Unlock()
	})
	s.SetProcess(claudia.NewStubAgent(func(string) error {
		t.Errorf("untagged Send")
		return fmt.Errorf("untagged Send")
	}))
	s.overseerEscalatorSeam = probe
	s.overseerSendSeam = func(string, string) error {
		sends++
		return nil
	}

	if _, err := s.sendToNamedAgentMode("jevons", "alpha", sendOriginOwner, delivery.ModeSubmit); err != nil {
		t.Fatal(err)
	}
	if sends != 0 || probe.untagged != 0 || len(probe.calls) != 0 {
		t.Fatalf("freeze failure still handed off: sends=%d untagged=%d steer=%d", sends, probe.untagged, len(probe.calls))
	}
	alphaWire := userTurnPrefix + "alpha"
	pending := ownerIDsFor(s, alphaWire)
	if len(pending) != 1 || pending[0] == "" {
		t.Fatalf("queued id after freeze failure = %v", pending)
	}
	s.mu.RLock()
	queued := append([]string(nil), s.notifyQueue...)
	s.mu.RUnlock()
	if len(queued) != 1 || queued[0] != alphaWire {
		t.Fatalf("queue = %v", queued)
	}

	s.setOverseerInFlight(true)
	s.mu.Lock()
	s.waiting = true
	s.overseerOwnerTurn = true
	s.mu.Unlock()
	if _, err := s.sendToNamedAgentMode("jevons", "beta", sendOriginOwner, delivery.ModeSubmit); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("steer freeze err = %v", err)
	}
	if sends != 0 || probe.untagged != 0 || len(probe.calls) != 0 {
		t.Fatalf("failed steer still handed off: sends=%d untagged=%d steer=%d", sends, probe.untagged, len(probe.calls))
	}
	if got := ownerIDsFor(s, alphaWire); len(got) != 1 || got[0] != pending[0] {
		t.Fatalf("failed steer popped pending alpha: %v", got)
	}
}

func missionFrozen(frozen []frozenMission, seat, id, mission string) bool {
	for _, row := range frozen {
		if row.seat == seat && row.id == id && row.mission == mission {
			return true
		}
	}
	return false
}
