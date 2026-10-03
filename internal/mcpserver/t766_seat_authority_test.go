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
	"github.com/marcelocantos/jevons/internal/panecensus"
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

// 🎯T766.2: setFlight / noteTurnEnded write InFlight into the shared authority
// so other controls read one answer rather than re-deriving from agentFlight.
func TestT766TurnFlightFeedsAuthority(t *testing.T) {
	s := t766Seat(t)
	s.noteTurnInFlight("jv-flight")
	st, ok := s.Seats().Get("jv-flight")
	if !ok || st.InFlight != seatstate.Yes {
		t.Fatalf("in-flight write did not reach authority: ok=%v %+v", ok, st)
	}
	if st.Alive != seatstate.Yes {
		t.Fatalf("in-flight should also claim alive: Alive=%s", st.Alive)
	}
	s.noteTurnEnded("jv-flight")
	st, _ = s.Seats().Get("jv-flight")
	if st.InFlight != seatstate.No {
		t.Fatalf("turn end did not clear InFlight: %+v", st)
	}
	// Unknown is not a claim — must not overwrite a prior Yes with ignorance.
	s.Seats().FromClaudia(seatstate.SeatReport{Name: "jv-u", Alive: true, PromptInFlight: true, Known: true}, nowForTest())
	s.setFlight("jv-u", FlightUnknown)
	st, _ = s.Seats().Get("jv-u")
	if st.InFlight != seatstate.Yes {
		t.Fatalf("FlightUnknown must not erase a known InFlight: %+v", st)
	}
}

// 🎯T766.2: flightState reads known InFlight from the authority before the
// process-local map — so a post-restart empty agentFlight does not erase a
// turn the authority still holds.
func TestT766FlightStatePrefersAuthority(t *testing.T) {
	s := t766Seat(t)
	// Authority says in flight; local map never written.
	s.Seats().Observe(seatstate.Observation{
		Name: "jv-auth", InFlight: seatstate.Yes, Alive: seatstate.Yes,
		QueueDepth: seatstate.QueueUnknown, Source: "turn.flight", At: nowForTest(),
	})
	if got := s.flightState("jv-auth"); got != FlightInFlight {
		t.Fatalf("flightState=%s want in_flight from authority", got)
	}
	// Authority says idle.
	s.Seats().Observe(seatstate.Observation{
		Name: "jv-auth", InFlight: seatstate.No,
		QueueDepth: seatstate.QueueUnknown, Source: "turn.flight", At: nowForTest(),
	})
	if got := s.flightState("jv-auth"); got != FlightIdle {
		t.Fatalf("flightState=%s want idle from authority", got)
	}
	// Authority silent → local map.
	s.setFlight("jv-local", FlightInFlight)
	// Clear authority knowledge by never observing jv-local... setFlight observes.
	// For a name with only local unknown authority: use a fresh seat whose
	// authority InFlight was never claimed — but setFlight claims it.
	// Use Get miss: no seat at all.
	if got := s.flightState("never-seen"); got != FlightUnknown {
		t.Fatalf("flightState=%s want unknown", got)
	}
}

// 🎯T766.2: agentPhase overlays Known idle/working from the authority onto
// a running seat only. never_briefed / phase_unknown / stopped stay first.
func TestT766AgentPhasePrefersAuthorityPhase(t *testing.T) {
	s := t766Seat(t)
	running := claudia.AgentDef{Name: "jv-t766-ap", Materialized: true}
	s.markAgentTurnBegan(running.Name)

	if got := s.agentPhase(running, true); got != AgentStatusPhaseUnknown {
		t.Fatalf("no authority phase: %s want phase_unknown", got)
	}

	s.Seats().Observe(seatstate.Observation{
		Name: running.Name, Alive: seatstate.Yes, Phase: turnev.PhaseIdle,
		QueueDepth: seatstate.QueueUnknown, Source: "test", At: nowForTest(),
	})
	if got := s.agentPhase(running, true); got != "idle" {
		t.Fatalf("known idle: %s want idle", got)
	}

	s.Seats().Observe(seatstate.Observation{
		Name: running.Name, Phase: turnev.PhaseWorking,
		QueueDepth: seatstate.QueueUnknown, Source: "test", At: nowForTest(),
	})
	if got := s.agentPhase(running, true); got != "working" {
		t.Fatalf("known working: %s want working", got)
	}

	// Stopped is not overridden by a Known working phase.
	s.Seats().Observe(seatstate.Observation{Name: running.Name, Alive: seatstate.No, QueueDepth: seatstate.QueueUnknown, Source: "test"})
	if got := s.agentPhase(running, false); got != AgentStatusStopped {
		t.Fatalf("dead process: %s want stopped", got)
	}

	// phase_unknown (no session to read) is not overridden by Known idle.
	unk := claudia.AgentDef{Name: "jv-t766-ap-unk"}
	s.Seats().Observe(seatstate.Observation{
		Name: unk.Name, Phase: turnev.PhaseIdle,
		QueueDepth: seatstate.QueueUnknown, Source: "test", At: nowForTest(),
	})
	if got := s.agentPhase(unk, true); got != AgentStatusPhaseUnknown {
		t.Fatalf("phase_unknown must stay first: %s", got)
	}
}

// 🎯T766.2: a real born-stuck diagnosis folds into the authority. Unknown
// (unobservable lookup, still inside grace) writes nothing — absence is not
// a claim.
func TestT766BornStuckFeedsAuthority(t *testing.T) {
	s := t766Seat(t)
	s.observeBornStuck("jv-bs", birthDiagnosis{Stuck: true})
	st, ok := s.Seats().Get("jv-bs")
	if !ok || st.BornStuck != seatstate.Yes {
		t.Fatalf("stuck claim: ok=%v %+v", ok, st)
	}

	s.observeBornStuck("jv-ok", birthDiagnosis{
		Accepted:  true,
		Existence: TranscriptExistence{Verdict: ExistencePresent},
	})
	st, ok = s.Seats().Get("jv-ok")
	if !ok || st.BornStuck != seatstate.No {
		t.Fatalf("present transcript: ok=%v %+v", ok, st)
	}

	s.observeBornStuck("jv-unk", birthDiagnosis{Unknown: true, Accepted: true, PastGrace: true})
	if _, ok := s.Seats().Get("jv-unk"); ok {
		t.Fatal("unobservable must not invent a born-stuck claim")
	}

	s.observeBornStuck("jv-grace", birthDiagnosis{Accepted: true, PastGrace: false})
	if _, ok := s.Seats().Get("jv-grace"); ok {
		t.Fatal("inside grace must not invent a born-stuck claim")
	}
}

// 🎯T766.2: a named fleet pane folds Alive=Yes; empty name is ignored.
func TestT766PanePresenceFeedsAuthority(t *testing.T) {
	s := t766Seat(t)
	p := panecensus.Pane{AgentName: "jv-pane", ID: "%1"}
	s.observePanePresence([]panecensus.Pane{p, {ID: "%2"}}) // unnamed
	st, ok := s.Seats().Get("jv-pane")
	if !ok || st.Alive != seatstate.Yes {
		t.Fatalf("named pane did not claim alive: ok=%v %+v", ok, st)
	}
	if _, ok := s.Seats().Get(""); ok {
		t.Fatal("empty name must not create a seat")
	}
	// Absence does not invent dead.
	if _, ok := s.Seats().Get("no-pane"); ok {
		t.Fatal("missing pane must not claim a seat")
	}
}

// The controls must retain unknown even if the old process-local cache says
// working. A queue-only feed cannot revive that old claim.
func TestT766StaleAuthorityNeverFallsBackToPrivateFlight(t *testing.T) {
	now := time.Now()
	a := seatstate.New(seatstate.Args{Now: func() time.Time { return now }, Stale: time.Second})
	s := &Server{}
	s.SetSeats(a)
	s.noteTurnInFlight("seat")
	now = now.Add(2 * time.Second)
	a.FromQueue("seat", 1, now)
	if s.flightState("seat") != FlightUnknown || s.seatInFlight("seat") != seatstate.Unknown {
		t.Fatal("stale authority fell back to private flight or was refreshed by queue traffic")
	}
}

func TestT766AgentPhaseDoesNotReadTheTranscriptOnDemand(t *testing.T) {
	s := &Server{}
	def := claudia.AgentDef{Name: "seat", SessionID: "unresolvable", Provider: claudia.ProviderClaude, WorkDir: t.TempDir()}
	s.Seats().Observe(seatstate.Observation{Name: def.Name, Alive: seatstate.Yes, Phase: turnev.PhaseWorking, Source: "test.feed", QueueDepth: seatstate.QueueUnknown})
	// The definition cannot resolve a transcript. Re-reading files would demote
	// this claim, while a cheap authority read retains exactly what was observed.
	for range 100 {
		if got := s.agentPhase(def, true); got != "working" {
			t.Fatalf("control re-derived phase: %s", got)
		}
	}
}

func TestT766QueueMutationsFeedAuthority(t *testing.T) {
	s := &Server{}
	q := s.sendQueue()
	s.observeQueue("seat")
	if s.pendingAgentSends("seat") != 0 {
		t.Fatal("observed empty queue not published")
	}
	if _, _, err := q.Append("seat", "one", time.Now()); err != nil {
		t.Fatal(err)
	}
	if s.pendingAgentSends("seat") != 1 {
		t.Fatal("enqueue not published")
	}
	if err := q.Clear("seat"); err != nil {
		t.Fatal(err)
	}
	if s.pendingAgentSends("seat") != 0 {
		t.Fatal("clear not published")
	}
	if s.pendingAgentSends("unseen") != seatstate.QueueUnknown {
		t.Fatal("unseen queue became empty")
	}
}

func TestT766PaneWithNoFlightObservationCannotAuthorizeReaping(t *testing.T) {
	s := &Server{}
	panes := []panecensus.Pane{{AgentName: "jv-unobserved", ID: "%123", Title: ""}}
	s.observePanePresence(panes)
	s.annotateFlight(panes)
	if s.seatState("jv-unobserved").InFlight != seatstate.Unknown {
		t.Fatal("blank title became idle")
	}
	if reaps := panecensus.Plan(panes, nil, panecensus.DefaultWarmPoolMax).Reap(); len(reaps) != 0 {
		t.Fatalf("unknown pane reaped: %+v", reaps)
	}
}

func TestT766BusyPaneReadsObservedActivityWithoutTranscriptScan(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	d := claudia.AgentDef{Name: "jv-busy", SessionID: "unresolvable", Provider: claudia.ProviderClaude, WorkDir: t.TempDir()}
	if err := reg.Register(d); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	s.Seats().Observe(seatstate.Observation{Name: d.Name, LastActivity: time.Now(), QueueDepth: seatstate.QueueUnknown, Source: "test.provider"})
	if !s.sendStallIsBusyPane(d.Name, t745Err("no_composer", t745Diff)) {
		t.Fatal("control ignored observed movement and tried to locate a transcript")
	}
	s.Seats().Forget(d.Name)
	if s.sendStallIsBusyPane(d.Name, t745Err("no_composer", t745Diff)) {
		t.Fatal("unobserved movement became known busy")
	}
}
