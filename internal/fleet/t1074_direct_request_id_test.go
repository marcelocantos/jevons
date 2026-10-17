// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

const t1074DirectSession = "019fd13d-e500-7913-b96c-981e50aa2e99"

type directFreeze struct {
	seat, id, mission string
}

// launchDirectSeat registers a live stub. The stub's Send callback is the
// provider handoff: it runs inside SendWithRequestID, after the host froze
// the mission and queued the request id.
func launchDirectSeat(t *testing.T, name, mission string, send func(string) error) (*claudia.Registry, *claudia.Agent) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: filepath.Join(dir, "work"), SessionID: t1074DirectSession,
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
	if _, err := reg.Launch(name); err != nil {
		t.Fatalf("stub launch: %v", err)
	}
	t.Cleanup(func() { reg.Stop(name) })
	if ag == nil {
		t.Fatal("launch returned no process")
	}
	return reg, ag
}

func newDirectFleet(t *testing.T, reg *claudia.Registry, name string) *Claudia {
	t.Helper()
	f := NewClaudia(reg)
	f.seats.FromClaudia(seatstate.SeatReport{
		Name: name, SessionID: t1074DirectSession, Alive: true, Known: true,
	}, time.Now())
	f.replyTimeout = 5 * time.Second
	return f
}

func TestT1074DirectTurnCarriesRequestID(t *testing.T) {
	ctx := context.Background()
	const name = "jv-t1074-direct"
	const mission = "T-direct-mission"
	usage, err := cost.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = usage.Close() })

	var frozen []directFreeze
	reg, ag := launchDirectSeat(t, name, mission, func(string) error {
		if len(frozen) == 0 || frozen[len(frozen)-1].id == "" {
			t.Errorf("mission not frozen before send")
			return fmt.Errorf("mission not frozen")
		}
		last := frozen[len(frozen)-1]
		got, ok, qerr := usage.RequestMission(ctx, name, last.id)
		if qerr != nil || !ok || got != mission {
			t.Errorf("stored mission before send id=%q got=%q ok=%v err=%v", last.id, got, ok, qerr)
			return fmt.Errorf("mission missing")
		}
		return nil
	})
	before := reg.Def(name).SessionID

	var stamped []string
	ag.SubscribeEvents(func(ev claudia.Event) {
		if ev.Type == "assistant" && ev.IsTerminalStop() {
			stamped = append(stamped, ev.RequestID)
		}
	})

	f := newDirectFleet(t, reg, name)
	var turn int
	f.replyStarted = func(a *claudia.Agent) {
		turn++
		a.PublishEvent(claudia.Event{
			Type: "assistant", Text: "pong", StopReason: "end_turn",
			TurnID: fmt.Sprintf("turn-%d", turn),
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
	first := stamped[0]

	reply, err = f.Send(name, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "pong" {
		t.Fatalf("second reply = %q", reply)
	}
	if len(frozen) != 2 || len(stamped) != 2 || stamped[1] == "" || stamped[1] == first || stamped[1] != frozen[1].id {
		t.Fatalf("identical text inherited an id: frozen=%v stamped=%v", frozen, stamped)
	}

	reply, err = f.Deliver(name, "beta")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "pong" {
		t.Fatalf("deliver reply = %q", reply)
	}
	if len(frozen) != 3 || len(stamped) != 3 || stamped[2] == "" || stamped[2] == first || stamped[2] == stamped[1] || stamped[2] != frozen[2].id {
		t.Fatalf("deliver frozen=%v stamped=%v", frozen, stamped)
	}
	if reg.Def(name).SessionID != before {
		t.Fatalf("direct turn reminted session: %s", reg.Def(name).SessionID)
	}
}

func TestT1074DirectTurnFreezeFailureDoesNotSend(t *testing.T) {
	const name = "jv-t1074-direct-freeze"
	var sends int
	reg, _ := launchDirectSeat(t, name, "T-direct-mission", func(string) error {
		sends++
		return nil
	})
	f := newDirectFleet(t, reg, name)
	f.replyStarted = func(*claudia.Agent) {
		t.Errorf("reply started after a failed freeze")
	}
	f.SetRequestMissionRecorder(func(context.Context, string, string, string) error {
		return fmt.Errorf("usage mission disk full")
	})

	_, err := f.Send(name, "alpha")
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("send freeze err = %v", err)
	}
	_, err = f.Deliver(name, "beta")
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("deliver freeze err = %v", err)
	}
	if sends != 0 {
		t.Fatalf("freeze failure still handed off %d times", sends)
	}
}

func TestT1074DirectTurnGateDoesNotMint(t *testing.T) {
	const name = "jv-t1074-direct-gate"
	var sends int
	reg, _ := launchDirectSeat(t, name, "T-direct-mission", func(string) error {
		sends++
		return nil
	})
	before := reg.Def(name).SessionID
	f := newDirectFleet(t, reg, name)
	var minted int
	f.SetRequestMissionRecorder(func(context.Context, string, string, string) error {
		minted++
		return nil
	})
	f.SetTurnGate(func(agent, sessionID string) error {
		return errors.New("turn delay for " + agent + ": high burn; session " + sessionID + " unchanged")
	})

	_, err := f.Send(name, "alpha")
	if err == nil || !strings.Contains(err.Error(), "high burn") {
		t.Fatalf("send err = %v", err)
	}
	_, err = f.Deliver(name, "beta")
	if err == nil || !strings.Contains(err.Error(), "high burn") {
		t.Fatalf("deliver err = %v", err)
	}
	if minted != 0 || sends != 0 {
		t.Fatalf("gate minted=%d sends=%d", minted, sends)
	}
	if reg.Def(name).SessionID != before {
		t.Fatalf("gate reminted session: %s", reg.Def(name).SessionID)
	}
}
