// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/seatprovider"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// TestT1074ModelSwitchPreservesRequestIdentity drives the butler send path
// and the real registry switch. A turn in flight refuses the switch and
// does not mint. A landed switch keeps the session, the target, and the
// mission already frozen for the earlier request, and it does not send.
// The next identical text is a new id.
func TestT1074ModelSwitchPreservesRequestIdentity(t *testing.T) {
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	ctx := context.Background()
	const name = "jv-t1074-switch"
	const mission = "T-switch-mission"
	const fromModel = "grok-3"
	const toModel = "grok-4"

	usage, err := cost.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = usage.Close() })

	var mu sync.Mutex
	var sends int
	var switches []string
	var frozen []directFreeze
	inFlight := false

	dir := t.TempDir()
	lane := openSwitchReceiptLane(t, dir)
	expectReceipt := func(id string, turns []string) {
		t.Helper()
		if !lane.live {
			return
		}
		got, err := lane.read(ctx, name, t1074DirectSession, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.RequestID != id || got.AttemptID != "attempt-"+id || got.State != claudiawire.ReceiptCompleted || got.Cause != "" {
			t.Fatalf("receipt id=%s got=%+v", id, got)
		}
		if len(got.TurnIDs) != len(turns) {
			t.Fatalf("receipt turns=%v want %v", got.TurnIDs, turns)
		}
		for i := range turns {
			if got.TurnIDs[i] != turns[i] {
				t.Fatalf("receipt turns=%v want %v", got.TurnIDs, turns)
			}
		}
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: t1074DirectSession,
		TargetID: mission, Provider: claudia.ProviderGrok, Model: fromModel,
		Materialized: true, AutoStart: false,
	}); err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	var ag *claudia.Agent
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			started, err := claudia.StartStub(ctx, cfg, &claudia.StubAgentOps{
				Send: func(string) error {
					mu.Lock()
					sends++
					mu.Unlock()
					if len(frozen) == 0 || frozen[len(frozen)-1].id == "" {
						return fmt.Errorf("mission not frozen before send")
					}
					last := frozen[len(frozen)-1]
					got, ok, qerr := usage.RequestMission(context.Background(), name, last.id)
					if qerr != nil || !ok || got != mission {
						return fmt.Errorf("stored mission before send id=%q got=%q ok=%v err=%v", last.id, got, ok, qerr)
					}
					return nil
				},
				PromptInFlight: func() bool {
					mu.Lock()
					defer mu.Unlock()
					return inFlight
				},
				SwitchModel: func(p claudia.Provider, m string) error {
					mu.Lock()
					switches = append(switches, string(p)+"/"+m)
					mu.Unlock()
					return nil
				},
			})
			if err != nil {
				return nil, err
			}
			ag = started
			return started, nil
		},
	})
	if _, err := reg.Launch(name); err != nil {
		t.Fatalf("stub launch: %v", err)
	}
	t.Cleanup(func() { reg.Stop(name) })
	if ag == nil {
		t.Fatal("launch returned no process")
	}
	if err := ag.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reg.Def(name).SessionID != t1074DirectSession || ag.SessionID() != t1074DirectSession {
		t.Fatalf("launch reminted session def=%s proc=%s", reg.Def(name).SessionID, ag.SessionID())
	}

	var stamped []string
	var switchStamps []string
	var switchEvents []claudia.Event
	ag.SubscribeEvents(func(ev claudia.Event) {
		if ev.Type == "assistant" && ev.IsTerminalStop() {
			stamped = append(stamped, ev.RequestID)
		}
		if ev.ProgressType == claudia.ProgressModelSwitch {
			switchStamps = append(switchStamps, ev.RequestID)
			switchEvents = append(switchEvents, ev)
		}
	})

	f := NewClaudia(reg)
	f.seats.FromClaudia(seatstate.SeatReport{
		Name: name, SessionID: t1074DirectSession, Alive: true, Known: true,
	}, time.Now())
	f.replyTimeout = 5 * time.Second
	var turn int
	f.replyStarted = func(a *claudia.Agent) {
		turn++
		turnID := fmt.Sprintf("turn-%d", turn)
		// The host id binds when the provider accepts the turn. An
		// assistant line is not acceptance, so it carries the id only
		// after this event has taken it.
		a.PublishEvent(claudia.Event{
			Type: "progress", ProgressType: claudia.ProgressPromptAccepted, TurnID: turnID,
		})
		a.PublishEvent(claudia.Event{
			Type: "assistant", Text: "pong", StopReason: "end_turn", TurnID: turnID,
		})
	}
	f.SetRequestMissionRecorder(func(ctx context.Context, seat, requestID, got string) error {
		if err := usage.RecordRequestMission(ctx, seat, requestID, got); err != nil {
			return err
		}
		frozen = append(frozen, directFreeze{seat: seat, id: requestID, mission: got})
		return nil
	})

	reply, err := f.Send(name, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "pong" {
		t.Fatalf("send reply = %q", reply)
	}
	if len(frozen) != 1 || len(stamped) != 1 || stamped[0] == "" || stamped[0] != frozen[0].id || frozen[0].mission != mission {
		t.Fatalf("send frozen=%v stamped=%v", frozen, stamped)
	}
	first := frozen[0].id
	got, ok, qerr := usage.RequestMission(ctx, name, first)
	if qerr != nil || !ok || got != mission {
		t.Fatalf("stored mission before switch id=%q got=%q ok=%v err=%v", first, got, ok, qerr)
	}
	if lane.live {
		lane.publish(name, t1074DirectSession, first, claudiawire.ReceiptCompleted, "", "", []string{"turn-1"})
	}
	expectReceipt(first, []string{"turn-1"})
	mu.Lock()
	if sends != 1 || len(switches) != 0 {
		t.Fatalf("before switch sends=%d switches=%v", sends, switches)
	}
	mu.Unlock()

	mu.Lock()
	inFlight = true
	mu.Unlock()
	if _, err := f.SwitchModel(name, "", toModel, false); err == nil || !strings.Contains(err.Error(), "in flight") {
		t.Fatalf("in-flight switch err = %v", err)
	}
	mu.Lock()
	inFlight = false
	refusedSends, refusedSwitches := sends, append([]string(nil), switches...)
	mu.Unlock()
	if refusedSends != 1 || len(refusedSwitches) != 0 || len(frozen) != 1 || len(switchStamps) != 0 {
		t.Fatalf("in-flight switch minted or sent: sends=%d switches=%v frozen=%v stamps=%v", refusedSends, refusedSwitches, frozen, switchStamps)
	}
	def := reg.Def(name)
	if def.Provider != claudia.ProviderGrok || def.Model != fromModel || def.SessionID != t1074DirectSession || def.TargetID != mission {
		t.Fatalf("refused switch rewrote the seat: %+v", def)
	}
	got, ok, qerr = usage.RequestMission(ctx, name, first)
	if qerr != nil || !ok || got != mission {
		t.Fatalf("refused switch changed mission id=%q got=%q ok=%v err=%v", first, got, ok, qerr)
	}
	expectReceipt(first, []string{"turn-1"})

	sw, err := f.SwitchModel(name, "", toModel, false)
	if err != nil {
		t.Fatal(err)
	}
	if !sw.Live || !sw.Changed || sw.Model != toModel || sw.FromModel != fromModel {
		t.Fatalf("switch = %+v", sw)
	}
	if reg.Get(name) != ag {
		t.Fatal("switch replaced the process")
	}
	mu.Lock()
	landedSwitches := append([]string(nil), switches...)
	landedSends := sends
	mu.Unlock()
	if landedSends != 1 || len(landedSwitches) != 1 || landedSwitches[0] != string(seatprovider.Resolve(claudia.ProviderGrok).Launch)+"/"+toModel {
		t.Fatalf("switch handoff sends=%d switches=%v", landedSends, landedSwitches)
	}
	def = reg.Def(name)
	if def.Model != toModel || def.SessionID != t1074DirectSession || def.TargetID != mission {
		t.Fatalf("landed switch lost identity: %+v", def)
	}
	if def.Provider != seatprovider.Resolve(claudia.ProviderGrok).Launch {
		t.Fatalf("provider = %s, want the sidecar seat id", def.Provider)
	}
	if seatprovider.Resolve(def.Provider).Plan != seatprovider.Grok {
		t.Fatalf("plan provider = %s", def.Provider)
	}
	if len(frozen) != 1 || len(switchStamps) != 1 || switchStamps[0] != "" || len(switchEvents) != 1 || switchEvents[0].Receipt != nil {
		t.Fatalf("switch took a request or a receipt cause: frozen=%v switchStamps=%v events=%+v", frozen, switchStamps, switchEvents)
	}
	got, ok, qerr = usage.RequestMission(ctx, name, first)
	if qerr != nil || !ok || got != mission {
		t.Fatalf("landed switch changed mission id=%q got=%q ok=%v err=%v", first, got, ok, qerr)
	}
	expectReceipt(first, []string{"turn-1"})

	reply, err = f.Send(name, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "pong" {
		t.Fatalf("second reply = %q", reply)
	}
	if len(frozen) != 2 || frozen[1].id == "" || frozen[1].id == first || frozen[1].mission != mission {
		t.Fatalf("post-switch text inherited an id: frozen=%v", frozen)
	}
	if len(stamped) != 2 || stamped[1] != frozen[1].id {
		t.Fatalf("post-switch stamp frozen=%v stamped=%v", frozen, stamped)
	}
	got, ok, qerr = usage.RequestMission(ctx, name, frozen[1].id)
	if qerr != nil || !ok || got != mission {
		t.Fatalf("second mission id=%q got=%q ok=%v err=%v", frozen[1].id, got, ok, qerr)
	}
	got, ok, qerr = usage.RequestMission(ctx, name, first)
	if qerr != nil || !ok || got != mission {
		t.Fatalf("first mission moved id=%q got=%q ok=%v err=%v", first, got, ok, qerr)
	}
	if lane.live {
		lane.publish(name, t1074DirectSession, frozen[1].id, claudiawire.ReceiptCompleted, "", "", []string{"turn-2"})
	}
	expectReceipt(frozen[1].id, []string{"turn-2"})
	expectReceipt(first, []string{"turn-1"})
	def = reg.Def(name)
	if def.SessionID != t1074DirectSession || def.TargetID != mission || def.Model != toModel {
		t.Fatalf("later send reminted the seat: %+v", def)
	}
}
