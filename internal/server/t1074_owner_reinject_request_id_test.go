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
	"github.com/marcelocantos/jevons/internal/converge"
	"github.com/marcelocantos/jevons/internal/cost"
)

func TestT1074OwnerReinjectionCarriesRequestID(t *testing.T) {
	ctx := context.Background()
	const mission = "T-owner-reinject"
	const words = "where is the fleet?"
	usage, err := cost.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = usage.Close() })
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

	var frozen []frozenMission
	var calls []overseerIDCall
	failNext := true
	s := &Server{}
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	s.SetRequestMissionRecorder(func(ctx context.Context, seat, requestID, got string) error {
		frozen = append(frozen, frozenMission{seat: seat, id: requestID, mission: got})
		return usage.RecordRequestMission(ctx, seat, requestID, got)
	})
	s.SetProcess(claudia.NewStubAgent(func(string) error {
		t.Errorf("untagged Send")
		return fmt.Errorf("untagged Send")
	}))
	s.overseerSendSeam = func(text, id string) error {
		if !strings.HasPrefix(id, "om-") {
			t.Errorf("reinjection id = %q", id)
		}
		if !strings.Contains(text, words) || !isOwnerNotifyText(text) {
			t.Errorf("reinjection text = %q", text)
		}
		if !missionFrozen(frozen, "jevons", id, mission) {
			t.Errorf("mission not frozen before handoff id=%q frozen=%v", id, frozen)
		}
		got, ok, qerr := usage.RequestMission(ctx, "jevons", id)
		if qerr != nil || !ok || got != mission {
			t.Errorf("stored mission before handoff id=%q got=%q ok=%v err=%v", id, got, ok, qerr)
		}
		calls = append(calls, overseerIDCall{text: text, id: id})
		if failNext {
			failNext = false
			return fmt.Errorf("overseer not running")
		}
		return nil
	}
	s.ownerMu.Lock()
	h := s.ownerHealthLocked()
	h.sendID = "send-1"
	h.sendText = words
	s.ownerMu.Unlock()

	now := time.Now()
	gap := converge.OwnerGap{Kind: converge.OwnerGapSendNotDelivered, Reason: "delivery lost"}
	if err := (ownerActuator{s}).requeueOwnerSend(gap, now); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].id == "" {
		t.Fatalf("reinjection handoff = %+v", calls)
	}
	firstID := calls[0].id
	firstText := calls[0].text
	s.mu.RLock()
	queued := append([]string(nil), s.notifyQueue...)
	s.mu.RUnlock()
	if len(queued) != 1 || queued[0] != firstText {
		t.Fatalf("failed reinjection dropped the note: %v", queued)
	}

	s.drainOverseerNotes()
	if len(calls) != 2 || calls[1].id != firstID || calls[1].text != firstText {
		t.Fatalf("retry = %+v, want id %s", calls, firstID)
	}

	// The successful handoff owns the session. A later re-injection of the
	// same words is a second request.
	s.mu.Lock()
	s.waiting = false
	s.mu.Unlock()
	later := now.Add(converge.DefaultOwnerBounds().Reply + time.Second)
	if err := (ownerActuator{s}).requeueOwnerSend(gap, later); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || calls[2].id == "" || calls[2].id == firstID || calls[2].text != firstText {
		t.Fatalf("later reinjection = %+v, want a new id", calls)
	}
}

func TestT1074OwnerReinjectionKeepsCancelHold(t *testing.T) {
	s := &Server{}
	s.SetOverseerName("jevons")
	var calls int
	s.overseerSendSeam = func(string, string) error {
		calls++
		return nil
	}
	s.SetProcess(claudia.NewStubAgent(func(string) error {
		t.Errorf("untagged Send")
		return fmt.Errorf("untagged Send")
	}))
	s.ownerMu.Lock()
	h := s.ownerHealthLocked()
	h.sendID = "send-1"
	h.sendText = "status please"
	s.ownerMu.Unlock()
	s.mu.Lock()
	s.ownerCancelHold = true
	s.mu.Unlock()

	if err := (ownerActuator{s}).requeueOwnerSend(converge.OwnerGap{Kind: converge.OwnerGapSendNotDelivered}, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	held := s.ownerCancelHold
	queued := len(s.notifyQueue)
	s.mu.RUnlock()
	if calls != 0 || !held || queued != 1 {
		t.Fatalf("reinjection released the hold: calls=%d held=%v queued=%d", calls, held, queued)
	}

	s.mu.Lock()
	s.ownerCancelHold = false
	s.waiting = false
	s.mu.Unlock()
	s.drainOverseerNotes()
	if calls != 1 {
		t.Fatalf("drain after the hold = %d calls", calls)
	}
}

func TestT1074OwnerReinjectionFreezeFailureDoesNotSend(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", SessionID: "gen-overseer", TargetID: "T-owner-reinject",
		Provider: claudia.ProviderGrok, WorkDir: dir,
	}); err != nil {
		t.Fatal(err)
	}
	var sends int
	s := &Server{}
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	s.SetRequestMissionRecorder(func(context.Context, string, string, string) error {
		return fmt.Errorf("usage mission disk full")
	})
	s.SetProcess(claudia.NewStubAgent(func(string) error {
		t.Errorf("untagged Send")
		return fmt.Errorf("untagged Send")
	}))
	s.overseerSendSeam = func(string, string) error {
		sends++
		return nil
	}
	s.ownerMu.Lock()
	h := s.ownerHealthLocked()
	h.sendID = "send-1"
	h.sendText = "status please"
	s.ownerMu.Unlock()

	if err := (ownerActuator{s}).requeueOwnerSend(converge.OwnerGap{Kind: converge.OwnerGapSendNotDelivered}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if sends != 0 {
		t.Fatalf("freeze failure still handed off: sends=%d", sends)
	}
	wire := userTurnPrefix + "status please"
	s.mu.RLock()
	queued := append([]string(nil), s.notifyQueue...)
	held := s.notifyUntrackedOwnerIDs[wire]
	s.mu.RUnlock()
	if len(queued) != 1 || queued[0] != wire || held == "" || !strings.HasPrefix(held, "om-") {
		t.Fatalf("queue=%v held=%q", queued, held)
	}
}
