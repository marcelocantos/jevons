// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cost"
)

func TestT1074HandleUserMessageCarriesRequestID(t *testing.T) {
	ctx := context.Background()
	const mission = "T-remote-mission"
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
		Name: "jevons", SessionID: "gen-remote", TargetID: mission,
		Provider: claudia.ProviderGrok, WorkDir: dir,
	}); err != nil {
		t.Fatal(err)
	}

	var frozen []frozenMission
	var ag *claudia.Agent
	ag = claudia.NewStubAgent(func(text string) error {
		if len(frozen) == 0 || frozen[len(frozen)-1].id == "" {
			t.Errorf("mission not frozen before send of %q", text)
			return fmt.Errorf("mission not frozen")
		}
		last := frozen[len(frozen)-1]
		got, ok, qerr := usage.RequestMission(ctx, "jevons", last.id)
		if qerr != nil || !ok || got != mission {
			t.Errorf("stored mission before send id=%q got=%q ok=%v err=%v", last.id, got, ok, qerr)
			return fmt.Errorf("mission missing")
		}
		ag.PublishEvent(claudia.Event{
			Type: "assistant", Text: "ok", StopReason: "end_turn",
			TurnID: fmt.Sprintf("remote-%d", len(frozen)),
		})
		return nil
	})
	var stamped []string
	ag.SubscribeEvents(func(ev claudia.Event) {
		if ev.Type == "assistant" && ev.IsTerminalStop() {
			stamped = append(stamped, ev.RequestID)
		}
	})

	s := &Server{}
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	s.SetProcess(ag)
	s.SetRequestMissionRecorder(func(ctx context.Context, seat, requestID, got string) error {
		if err := usage.RecordRequestMission(ctx, seat, requestID, got); err != nil {
			return err
		}
		frozen = append(frozen, frozenMission{seat: seat, id: requestID, mission: got})
		return nil
	})

	s.HandleUserMessage("alpha")
	if len(frozen) != 1 || len(stamped) != 1 || stamped[0] == "" || stamped[0] != frozen[0].id || frozen[0].mission != mission {
		t.Fatalf("remote send frozen=%v stamped=%v", frozen, stamped)
	}
	first := stamped[0]

	s.HandleAction("send_message", "alpha")
	if len(frozen) != 2 || len(stamped) != 2 || stamped[1] == "" || stamped[1] == first || stamped[1] != frozen[1].id {
		t.Fatalf("identical remote text inherited an id: frozen=%v stamped=%v", frozen, stamped)
	}
}

func TestT1074HandleUserMessageFreezeFailureDoesNotSend(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", SessionID: "gen-remote", TargetID: "T-remote-mission",
		Provider: claudia.ProviderGrok, WorkDir: dir,
	}); err != nil {
		t.Fatal(err)
	}

	var sends int
	s := &Server{}
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	s.SetProcess(claudia.NewStubAgent(func(string) error {
		sends++
		return nil
	}))
	s.SetRequestMissionRecorder(func(context.Context, string, string, string) error {
		return fmt.Errorf("usage mission disk full")
	})

	s.HandleUserMessage("alpha")
	s.mu.RLock()
	waiting := s.waiting
	s.mu.RUnlock()
	if sends != 0 || waiting {
		t.Fatalf("freeze failure sent=%d waiting=%v", sends, waiting)
	}

	s.SetProcess(nil)
	var minted int
	s.SetRequestMissionRecorder(func(context.Context, string, string, string) error {
		minted++
		return nil
	})
	s.HandleUserMessage("beta")
	if minted != 0 {
		t.Fatalf("nil process minted %d requests", minted)
	}
}

func remoteReceiptServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", SessionID: "gen-remote", TargetID: "T-remote-mission",
		Provider: claudia.ProviderGrok, WorkDir: dir,
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	s.SetProcess(claudia.NewStubAgent(func(string) error { return nil }))
	return s
}

func TestT1074HandleUserMessageNilQueryDoesNotConfirm(t *testing.T) {
	s := remoteReceiptServer(t)
	s.HandleUserMessage("alpha")
	if ownerSendDelivered(s) {
		t.Fatal("nil query confirmed owner delivery")
	}
}

func TestT1074HandleUserMessageUnknownReceiptDoesNotConfirm(t *testing.T) {
	s := remoteReceiptServer(t)
	s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		if q.Seat != "jevons" || q.SessionID != "gen-remote" || q.RequestID == "" {
			t.Errorf("query = %+v", q)
		}
		return durableOwnerReceipt(q, claudia.ReceiptUnknown, "not_found"), nil
	})
	s.HandleUserMessage("alpha")
	if ownerSendDelivered(s) {
		t.Fatal("unknown receipt confirmed owner delivery")
	}
}

func TestT1074HandleUserMessageSettlingReceiptConfirms(t *testing.T) {
	s := remoteReceiptServer(t)
	var seen *claudia.RequestReceipt
	s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		if q.Seat != "jevons" || q.SessionID != "gen-remote" || q.RequestID == "" {
			t.Errorf("query = %+v", q)
		}
		seen = durableOwnerReceipt(q, claudia.ReceiptCompleted, "401 unauthorized")
		return seen, nil
	})
	s.HandleUserMessage("alpha")
	if seen == nil || seen.Cause != "" || seen.Reason != "401 unauthorized" {
		t.Fatalf("receipt = %+v", seen)
	}
	if !ownerSendDelivered(s) {
		t.Fatal("settling receipt did not confirm owner delivery")
	}
}
