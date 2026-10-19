// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/upgrade"
)

// TestT1074NilSeamFleetBatchReachesAgent leaves both notify seams nil and
// drives drainOverseerNotes through the attached *claudia.Agent. The id is
// observed inside Agent.SendWithRequestID, at the stub hook that method calls.
func TestT1074NilSeamFleetBatchReachesAgent(t *testing.T) {
	if upgrade.DefaultReadopter != nil {
		t.Fatal("DefaultReadopter is set; WithReadopt would not stay on this process")
	}
	ctx := context.Background()
	const mission = "T-nil-seam"
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
	var untagged int
	failNext := true
	s := &Server{}
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	s.SetRequestMissionRecorder(func(ctx context.Context, seat, requestID, got string) error {
		frozen = append(frozen, frozenMission{seat: seat, id: requestID, mission: got})
		return usage.RecordRequestMission(ctx, seat, requestID, got)
	})
	s.SetProcess(claudia.NewStubAgentOps(&claudia.StubAgentOps{
		Send: func(string) error {
			untagged++
			return fmt.Errorf("untagged Send")
		},
		SendWithRequestID: func(text, id string) error {
			if !strings.HasPrefix(id, "fn-") {
				t.Errorf("fleet id = %q", id)
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
		},
	}))
	if s.overseerSendSeam != nil || s.notifySender != nil {
		t.Fatal("nil-seam test installed a notify seam")
	}
	if st := s.seatState(s.overseerSeatName()); st.Alive != seatstate.Yes {
		t.Fatalf("stub not observed alive: %+v", st)
	}

	noteA := "[event: worker-idle]\nWorker: jv-a\n"
	noteB := "[event: worker-idle]\nWorker: jv-b\n"
	s.mu.Lock()
	s.waiting = true
	s.mu.Unlock()
	if err := s.SendToOverseer(noteA); err != nil {
		t.Fatal(err)
	}
	if err := s.SendToOverseer(noteB); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("batch handed off while the overseer was busy: %+v", calls)
	}
	s.mu.Lock()
	s.waiting = false
	s.mu.Unlock()
	s.drainOverseerNotes()
	if untagged != 0 || len(calls) != 1 || calls[0].id == "" || !strings.Contains(calls[0].text, "jv-a") || !strings.Contains(calls[0].text, "jv-b") {
		t.Fatalf("failed agent handoff untagged=%d calls=%+v", untagged, calls)
	}
	firstID := calls[0].id
	firstText := calls[0].text
	s.mu.RLock()
	queued := append([]string(nil), s.notifyQueue...)
	s.mu.RUnlock()
	if len(queued) != 2 {
		t.Fatalf("failed handoff dropped the batch: %v", queued)
	}

	s.drainOverseerNotes()
	if untagged != 0 || len(calls) != 2 || calls[1].id != firstID || calls[1].text != firstText {
		t.Fatalf("retry untagged=%d calls=%+v, want id %s", untagged, calls, firstID)
	}

	s.mu.Lock()
	s.waiting = true
	s.mu.Unlock()
	if err := s.SendToOverseer(noteA); err != nil {
		t.Fatal(err)
	}
	if err := s.SendToOverseer(noteB); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.waiting = false
	s.mu.Unlock()
	s.drainOverseerNotes()
	if untagged != 0 || len(calls) != 3 || calls[2].id == "" || calls[2].id == firstID || calls[2].text != firstText {
		t.Fatalf("later identical batch untagged=%d calls=%+v, want a new id", untagged, calls)
	}
}

func TestT1074NilSeamFreezeFailureDoesNotSend(t *testing.T) {
	if upgrade.DefaultReadopter != nil {
		t.Fatal("DefaultReadopter is set; WithReadopt would not stay on this process")
	}
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", SessionID: "gen-overseer", TargetID: "T-nil-seam",
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
	s.SetProcess(claudia.NewStubAgentOps(&claudia.StubAgentOps{
		Send: func(string) error {
			sends++
			return fmt.Errorf("untagged Send")
		},
		SendWithRequestID: func(string, string) error {
			sends++
			return nil
		},
	}))
	if s.overseerSendSeam != nil || s.notifySender != nil {
		t.Fatal("nil-seam test installed a notify seam")
	}
	if st := s.seatState(s.overseerSeatName()); st.Alive != seatstate.Yes {
		t.Fatalf("stub not observed alive: %+v", st)
	}
	s.mu.Lock()
	s.waiting = true
	s.mu.Unlock()
	if err := s.SendToOverseer("[event: worker-idle]\nWorker: jv-a\n"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.waiting = false
	s.mu.Unlock()
	s.drainOverseerNotes()
	if sends != 0 {
		t.Fatalf("freeze failure still handed off: sends=%d", sends)
	}
	s.mu.RLock()
	queued := append([]string(nil), s.notifyQueue...)
	held := s.notifyFleetBatchID
	s.mu.RUnlock()
	if len(queued) != 1 || held == "" || !strings.HasPrefix(held, "fn-") {
		t.Fatalf("queue=%v held=%q", queued, held)
	}
}
