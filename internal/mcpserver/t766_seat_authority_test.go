// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// 🎯T766.2: the seat-load governor stopped deriving idleness for itself.
//
// It used to default to idle and then ask the registry, so a seat nothing
// was known about read as idle — and SeatIdle is what authorises
// LoadTerminate to kill a process group. An absence authorising a kill is
// the sharpest instance of the pattern docs/fleet-census.md catalogues.

// The Server's authority runs on the real clock, so observations must be
// stamped against it — a fixed date reads as stale and every condition
// decays to unknown, which is the authority behaving correctly.
func nowForTest() time.Time { return time.Now() }

func t766Seat(t *testing.T) *Server {
	t.Helper()
	return &Server{}
}

// The property: a seat the authority cannot speak for is never idle, so it
// can never be terminated on ignorance.
func TestT766UnknownSeatIsNotIdleForTheLoadGovernor(t *testing.T) {
	s := t766Seat(t)
	if st, ok := s.Seats().Get("never-seen"); ok {
		t.Fatalf("an unreported seat answered: %+v", st)
	}
	// The conversion under test: idle is false unless the authority says
	// InFlight is observed false.
	idle := false
	if st, ok := s.Seats().Get("never-seen"); ok && st.InFlight == seatstate.No {
		idle = true
	}
	if idle {
		t.Fatal("an unknown seat read as idle — that is what authorises a process-group kill")
	}
}

// A seat the provider reported as between turns is idle, and one mid-turn
// is not.
func TestT766AuthorityDecidesSeatIdleness(t *testing.T) {
	s := t766Seat(t)
	a := s.Seats()

	a.FromClaudia(seatstate.SeatReport{Name: "busy", Alive: true, PromptInFlight: true, Known: true}, nowForTest())
	a.FromClaudia(seatstate.SeatReport{Name: "quiet", Alive: true, PromptInFlight: false, Known: true}, nowForTest())

	for _, tc := range []struct {
		name string
		want bool
	}{{"busy", false}, {"quiet", true}} {
		st, ok := a.Get(tc.name)
		if !ok {
			t.Fatalf("%s: not known", tc.name)
		}
		got := st.InFlight == seatstate.No
		if got != tc.want {
			t.Fatalf("%s: idle=%v want %v (InFlight=%s)", tc.name, got, tc.want, st.InFlight)
		}
	}
}

// The sink feeds the authority, which is what makes the governor's reading
// current rather than a guess.
func TestT766TurnEventsReachTheAuthority(t *testing.T) {
	s := t766Seat(t)
	s.Seats().FromTurnEvent("jv-1", false, nowForTest())
	st, ok := s.Seats().Get("jv-1")
	if !ok || st.InFlight != seatstate.Yes {
		t.Fatalf("a mid-turn event did not reach the authority: %+v ok=%v", st, ok)
	}
	s.Seats().FromTurnEvent("jv-1", true, nowForTest())
	st, _ = s.Seats().Get("jv-1")
	if st.InFlight != seatstate.No {
		t.Fatalf("a terminal event did not end the turn: %+v", st)
	}
}

// Guard against the load source shape drifting away from what the governor
// consumes.
func TestT766LoadSourceCarriesSeatIdle(t *testing.T) {
	var src capacity.LoadSource
	src.SeatIdle = true
	if !src.SeatIdle {
		t.Fatal("capacity.LoadSource lost SeatIdle")
	}
}

// 🎯T766.2: main hands every package the same authority. The injected
// instance is the one Seats() returns, so the MCP server, the HTTP server
// and the fleet share one answer.
func TestT766SetSeatsSharesTheInjectedAuthority(t *testing.T) {
	s := &Server{}
	a := seatstate.New(seatstate.Args{})
	s.SetSeats(a)
	if s.Seats() != a {
		t.Fatal("Seats() is not the injected authority")
	}
}

// 🎯T766.2: fleet_recover no longer holds a private PromptInFlight read.
// Production wires PromptInFlight through seatInFlight (records + answers);
// a nil hook is unobserved false, not a second derivation against the handle.
func TestT766FleetRecoverDoesNotDeriveInFlight(t *testing.T) {
	// Needle scan mirrors the docratchet: production fleet_recover.go must
	// not contain a direct .PromptInFlight() call outside comments.
	body, err := os.ReadFile("fleet_recover.go")
	if err != nil {
		// test runs with the package dir as cwd under go test.
		body, err = os.ReadFile("internal/mcpserver/fleet_recover.go")
	}
	if err != nil {
		t.Fatal(err)
	}
	needle := ".PromptInFlight()"
	for i, line := range strings.Split(string(body), "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		if idx := strings.Index(line, "//"); idx >= 0 && idx < strings.Index(line, needle) {
			continue
		}
		t.Fatalf("fleet_recover.go:%d still derives in-flight directly: %s", i+1, strings.TrimSpace(line))
	}
}

// 🎯T766.2: idle-nudge folds the decoder phase it already paid for into the
// shared authority (phase-only). Alive/InFlight stay unclaimed — a transcript
// decode is not a process or event-stream claim.
func TestT766IdleNudgeObservesSessionPhase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t766-phase", WorkDir: dir, SessionID: "s1",
		Purpose: claudia.PurposeWork, Materialized: true, AutoStart: true, TargetID: "T766.2",
	}); err != nil {
		t.Fatal(err)
	}
	s := t766Seat(t)
	var observed []string
	reps := SweepIdleNudges(IdleNudgeSweepArgs{
		Reg:          reg,
		Now:          time.Unix(5000, 0),
		OverseerName: "jevons",
		SessionPhase: func(claudia.AgentDef) turnev.Phase { return turnev.PhaseIdle },
		ObservePhase: func(name string, phase turnev.Phase) {
			observed = append(observed, name+":"+phase.String())
			s.observeSessionPhase(name, phase)
		},
		ProcessRunning: func(string) bool { return true },
		// No push — we only care that the fold fired.
	})
	_ = reps
	if len(observed) != 1 || observed[0] != "jv-t766-phase:idle" {
		t.Fatalf("ObservePhase got %v", observed)
	}
	st, ok := s.Seats().Get("jv-t766-phase")
	if !ok {
		t.Fatal("authority never saw the seat")
	}
	if st.Phase != turnev.PhaseIdle {
		t.Fatalf("phase=%s want idle", st.Phase)
	}
	// Phase-only: Alive/InFlight must stay unknown (anti-pattern T766.2 ends).
	if st.Alive.Known() || st.InFlight.Known() {
		t.Fatalf("transcript fold invented live fields: Alive=%s InFlight=%s", st.Alive, st.InFlight)
	}
}
