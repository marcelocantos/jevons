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
	"github.com/marcelocantos/jevons/internal/seatstate"
)

const t1074FallbackSession = "019fd13d-e500-7913-b96c-981e50aa2e99"

// launchFallbackWorker registers a live stub worker that is not the
// overseer. The send callback runs inside SendWithRequestID, after the
// host has frozen the seat mission and queued the request id.
func launchFallbackWorker(t *testing.T, s *Server, name, mission string, send func(string) error) *claudia.Agent {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: filepath.Join(dir, "work"), SessionID: t1074FallbackSession,
		TargetID: mission, Provider: claudia.ProviderGrok, Materialized: true, AutoStart: false,
	}); err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	var ag *claudia.Agent
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(context.Context, claudia.Config) (*claudia.Agent, error) {
			ag = claudia.NewStubAgent(send)
			return ag, nil
		},
	})
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	if _, err := reg.Launch(name); err != nil {
		t.Fatalf("stub launch: %v", err)
	}
	t.Cleanup(func() { reg.Stop(name) })
	if ag == nil {
		t.Fatal("launch returned no process")
	}
	s.seats.Load().FromClaudia(seatstate.SeatReport{
		Name: name, SessionID: t1074FallbackSession, Alive: true, Known: true,
	}, time.Now())
	return ag
}

func recordFallbackMission(t *testing.T, s *Server, usage *cost.Store, frozen *[]frozenMission) {
	t.Helper()
	s.SetRequestMissionRecorder(func(ctx context.Context, seat, requestID, got string) error {
		if err := usage.RecordRequestMission(ctx, seat, requestID, got); err != nil {
			return err
		}
		*frozen = append(*frozen, frozenMission{seat: seat, id: requestID, mission: got})
		return nil
	})
}

func openFallbackUsage(t *testing.T) *cost.Store {
	t.Helper()
	usage, err := cost.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = usage.Close() })
	return usage
}

func TestT1074BareRegistryCarriesRequestID(t *testing.T) {
	ctx := context.Background()
	const name = "jv-t1074-bare"
	const mission = "T-bare-mission"
	usage := openFallbackUsage(t)
	var frozen []frozenMission
	s := &Server{}
	var ag *claudia.Agent
	ag = launchFallbackWorker(t, s, name, mission, func(string) error {
		if len(frozen) == 0 || frozen[len(frozen)-1].id == "" || frozen[len(frozen)-1].seat != name {
			t.Errorf("mission not frozen before bare send: %+v", frozen)
			return fmt.Errorf("mission not frozen")
		}
		last := frozen[len(frozen)-1]
		got, ok, qerr := usage.RequestMission(ctx, name, last.id)
		if qerr != nil || !ok || got != mission {
			t.Errorf("stored mission before bare send id=%q got=%q ok=%v err=%v", last.id, got, ok, qerr)
			return fmt.Errorf("mission missing")
		}
		ag.PublishEvent(claudia.Event{
			Type: "assistant", Text: "pong", StopReason: "end_turn",
			TurnID: fmt.Sprintf("bare-%d", len(frozen)),
		})
		return nil
	})
	var stamped []string
	ag.SubscribeEvents(func(ev claudia.Event) {
		if ev.Type == "assistant" && ev.IsTerminalStop() {
			stamped = append(stamped, ev.RequestID)
		}
	})
	recordFallbackMission(t, s, usage, &frozen)

	out, err := s.sendToNamedAgentMode(name, "alpha", sendOriginOwner, delivery.ModeSubmit)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "sent" {
		t.Fatalf("bare status = %+v", out)
	}
	if len(frozen) != 1 || len(stamped) != 1 || stamped[0] == "" || stamped[0] != frozen[0].id || !strings.HasPrefix(stamped[0], "rq-") || frozen[0].mission != mission {
		t.Fatalf("bare frozen=%+v stamped=%v", frozen, stamped)
	}
	first := stamped[0]

	out, err = s.sendToNamedAgentMode(name, "alpha", sendOriginOwner, delivery.ModeSubmit)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "sent" {
		t.Fatalf("second status = %+v", out)
	}
	if len(frozen) != 2 || len(stamped) != 2 || stamped[1] == "" || stamped[1] == first || stamped[1] != frozen[1].id || !strings.HasPrefix(stamped[1], "rq-") {
		t.Fatalf("identical bare text inherited an id: frozen=%+v stamped=%v", frozen, stamped)
	}

	var hooked int
	s.SetAgentSendOriginHook(func(string, string, string, delivery.Mode) (AgentSendOutcome, error) {
		hooked++
		return AgentSendOutcome{Status: "queued", Mechanism: delivery.MechanismSubmit}, nil
	})
	out, err = s.sendToNamedAgentMode(name, "alpha", sendOriginOwner, delivery.ModeSubmit)
	if err != nil {
		t.Fatal(err)
	}
	if hooked != 1 || out.Status != "queued" || len(frozen) != 2 || len(stamped) != 2 {
		t.Fatalf("origin hook fell through: hooked=%d out=%+v frozen=%d stamped=%d", hooked, out, len(frozen), len(stamped))
	}
}

func TestT1074BareRegistryFreezeFailureDoesNotSend(t *testing.T) {
	const name = "jv-t1074-bare-freeze"
	var sends int
	s := &Server{}
	launchFallbackWorker(t, s, name, "T-bare-mission", func(string) error {
		sends++
		return nil
	})
	s.SetRequestMissionRecorder(func(context.Context, string, string, string) error {
		return fmt.Errorf("usage mission disk full")
	})

	_, err := s.sendToNamedAgentMode(name, "alpha", sendOriginOwner, delivery.ModeSubmit)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("bare freeze err = %v", err)
	}
	if sends != 0 {
		t.Fatalf("freeze failure still handed off %d times", sends)
	}
}

func TestT1074VoiceDelegateCarriesRequestID(t *testing.T) {
	ctx := context.Background()
	const name = "jv-t1074-voice"
	const mission = "T-voice-mission"
	usage := openFallbackUsage(t)
	var frozen []frozenMission
	s := &Server{}
	var ag *claudia.Agent
	ag = launchFallbackWorker(t, s, name, mission, func(string) error {
		if len(frozen) == 0 || frozen[len(frozen)-1].id == "" || frozen[len(frozen)-1].seat != name {
			t.Errorf("mission not frozen before voice send: %+v", frozen)
			return fmt.Errorf("mission not frozen")
		}
		last := frozen[len(frozen)-1]
		got, ok, qerr := usage.RequestMission(ctx, name, last.id)
		if qerr != nil || !ok || got != mission {
			t.Errorf("stored mission before voice send id=%q got=%q ok=%v err=%v", last.id, got, ok, qerr)
			return fmt.Errorf("mission missing")
		}
		ag.PublishEvent(claudia.Event{
			Type: "assistant", Text: "pong", StopReason: "end_turn",
			TurnID: fmt.Sprintf("voice-%d", len(frozen)),
		})
		return nil
	})
	var stamped []string
	ag.SubscribeEvents(func(ev claudia.Event) {
		if ev.Type == "assistant" && ev.IsTerminalStop() {
			stamped = append(stamped, ev.RequestID)
		}
	})
	recordFallbackMission(t, s, usage, &frozen)
	log, err := NewGrokLog(filepath.Join(t.TempDir(), "voice", "grok.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	vb := &VoiceBridge{srv: s, tasks: map[string]*pendingTask{}, log: log}

	vb.runDelegatedTask(&pendingTask{id: "task-1", agent: name, task: "alpha"}, ag)
	if len(frozen) != 1 || len(stamped) != 1 || stamped[0] == "" || stamped[0] != frozen[0].id || !strings.HasPrefix(stamped[0], "vc-") || frozen[0].mission != mission {
		t.Fatalf("voice frozen=%+v stamped=%v", frozen, stamped)
	}
	first := stamped[0]
	entries, err := log.Tail(4)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Meta["kind"] != "task_complete" || !strings.Contains(entries[0].Content, "pong") {
		t.Fatalf("voice completion = %+v", entries)
	}

	vb.runDelegatedTask(&pendingTask{id: "task-2", agent: name, task: "alpha"}, ag)
	if len(frozen) != 2 || len(stamped) != 2 || stamped[1] == "" || stamped[1] == first || stamped[1] != frozen[1].id || !strings.HasPrefix(stamped[1], "vc-") {
		t.Fatalf("identical voice text inherited an id: frozen=%+v stamped=%v", frozen, stamped)
	}

	var sends int
	s.SetRequestMissionRecorder(func(context.Context, string, string, string) error {
		return fmt.Errorf("usage mission disk full")
	})
	failed := launchFallbackWorker(t, s, "jv-t1074-voice-freeze", mission, func(string) error {
		sends++
		return nil
	})
	vb.runDelegatedTask(&pendingTask{id: "task-3", agent: "jv-t1074-voice-freeze", task: "beta"}, failed)
	if sends != 0 {
		t.Fatalf("voice freeze failure still handed off %d times", sends)
	}
	entries, err = log.Tail(4)
	if err != nil {
		t.Fatal(err)
	}
	last := entries[len(entries)-1]
	if last.Meta["kind"] != "task_failed" || !strings.Contains(last.Content, "disk full") {
		t.Fatalf("voice freeze completion = %+v", last)
	}
}
