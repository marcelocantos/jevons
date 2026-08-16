// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/converge"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/poproactive"
	"github.com/marcelocantos/jevons/internal/staffops"
)

// 🎯T414 acceptance 5: the hermetic oracle. Every control named in acceptance 2
// is driven through its real decision path, crossed with every intent state,
// and checked in both directions:
//
//	suppress — a parked, provider-blocked, owner-blocked or reaped intent
//	           stops the control acting, whichever axis carries it
//	revive   — a working (or unstamped) intent leaves the control acting on
//	           exactly the case it exists for, so 🎯T171 short-resume and
//	           🎯T208 outage re-pressure survive this target
//
// The second direction is not decoration. The obvious over-broad fix — treat a
// stopped process as parked — passes every suppression check and silently ends
// crash recovery, which is the same class of invisible failure as the
// resurrections of 2026-08-10. TestT414OracleDetectsBothFailureModes below
// feeds this checker a mutant of each shape and asserts it goes red on both.

func t414Now() time.Time { return time.Date(2026, 8, 10, 6, 0, 0, 0, time.UTC) }

const t414Agent = "jv-t414-auto"

// t414Snapshot is the fixture registry intent state: one fleet-wide answer and
// one agent row.
func t414Snapshot(fleet, agent fleetintent.State) fleetintent.Snapshot {
	return fleetintent.Snapshot{
		Fleet:  fleetintent.Record{State: fleet},
		Agents: map[string]fleetintent.Record{t414Agent: {State: agent}},
	}
}

// t414Control binds one acceptance-2 control to the real code that decides
// whether it acts.
type t414Control struct {
	// label is the acceptance-2 wording, so a failure names the control the
	// target names.
	label   string
	control fleetintent.Control
	// fleetOnly marks a control deciding whether to create an agent that does
	// not exist yet: there is no row to carry a per-agent intent, so only the
	// fleet-wide axis can decline it.
	fleetOnly bool
	// noReviveCase marks a control whose acting path cannot be driven
	// hermetically — it launches a real provider process. Declared rather
	// than quietly skipped: see ControlDeliverStart below.
	noReviveCase bool
	// acts drives the control over its canonical open case (the agent is
	// registered, its mission is open, and its process is not doing the work)
	// and reports whether it acted.
	acts func(t *testing.T, fleet, agent fleetintent.State) bool
}

// t414ReadyLeaf is one unengaged, ungated, unblocked Build leaf — the input
// that made the sentinel read 34 of these as neglect while the fleet was
// parked under a provider block.
func t414ReadyLeaf() []poproactive.LeafObs {
	return []poproactive.LeafObs{{ID: "T500", Name: "New unengaged Build leaf"}}
}

// t414SweepReg is a fleetSweepReg whose agent has a process handle that is no
// longer alive: dead, AutoStart, and therefore the exact shape the 🎯T85 sweep
// re-launched every pass before intent existed.
type t414SweepReg struct{ launched bool }

func (r *t414SweepReg) List() []claudia.AgentDef {
	return []claudia.AgentDef{{Name: t414Agent, Purpose: claudia.PurposeWork, TargetID: "T500", AutoStart: true}}
}
func (r *t414SweepReg) ProcState(string) (bool, bool) { return true, false }
func (r *t414SweepReg) Launch(string) error           { r.launched = true; return nil }
func (r *t414SweepReg) Stop(string)                   {}

func t414Controls() []t414Control {
	return []t414Control{
		{
			label:     "frontier-consume (spawn a worker for a ready leaf)",
			control:   fleetintent.ControlSpawn,
			fleetOnly: true,
			acts: func(_ *testing.T, fleet, _ fleetintent.State) bool {
				spawned := false
				SweepFrontierConsume(FrontierConsumeArgs{
					Leaves:       t414ReadyLeaf(),
					Now:          t414Now(),
					PORegistered: true,
					FleetIntent:  fleet,
					Spawn: func(poproactive.LeafObs, string) error {
						spawned = true
						return nil
					},
				})
				return spawned
			},
		},
		{
			label:     "PO proactive pass (keep kicking while leaves are ready)",
			control:   fleetintent.ControlSpawn,
			fleetOnly: true,
			acts: func(_ *testing.T, fleet, _ fleetintent.State) bool {
				return poproactive.ShouldKeepKickingUnderIntent(t414ReadyLeaf(), fleet)
			},
		},
		{
			label:   "worker-idle nudge sweep",
			control: fleetintent.ControlNudge,
			acts: func(t *testing.T, fleet, agent fleetintent.State) bool {
				act, _ := ClassifyIdleNudge(IdleNudgeObs{
					Name:           t414Agent,
					FleetIntent:    fleet,
					Intent:         agent,
					ProcessRunning: true,
					Phase:          "idle",
					IdleFor:        30 * time.Minute,
					HasOpenMission: true,
					BriefPresent:   true,
				})
				return act == IdleNudgeNudge
			},
		},
		{
			label:   "dead-handle recovery sweep (🎯T85 silent death)",
			control: fleetintent.ControlRevive,
			acts: func(t *testing.T, fleet, agent fleetintent.State) bool {
				reg := &t414SweepReg{}
				reps := sweepDeadAgents(reg, "jevons", t414Snapshot(fleet, agent))
				// The sweep must still *report* the dead handle under every
				// intent — the owner should see that a process is gone. Only
				// the re-launch is gated.
				if len(reps) != 1 || reps[0].Name != t414Agent {
					t.Fatalf("dead handle went unreported: %+v", reps)
				}
				return reg.launched
			},
		},
		{
			label:   "restart reattach short-resume (🎯T171 path 2)",
			control: fleetintent.ControlRevive,
			acts: func(t *testing.T, fleet, agent fleetintent.State) bool {
				def := claudia.AgentDef{Name: t414Agent, Purpose: claudia.PurposeWork, TargetID: "T500"}
				return EligibleOpenMissionResume(def, true, false, false, false, t414Snapshot(fleet, agent))
			},
		},
		{
			label:   "impatience ladder (re-pressure a stalled mission)",
			control: fleetintent.ControlRepressure,
			acts: func(t *testing.T, fleet, agent fleetintent.State) bool {
				now := t414Now()
				l := converge.NewLadder()
				actions, _ := l.Reconcile(now, []converge.Gap{{
					Agent:       t414Agent,
					Mission:     "T500",
					Since:       now.Add(-converge.RepressureAfter - time.Minute),
					FleetIntent: fleet,
					Intent:      agent,
				}})
				return len(actions) > 0
			},
		},
		{
			label:   "sentinel / staff-ops repair mission",
			control: fleetintent.ControlRepair,
			acts: func(t *testing.T, fleet, agent fleetintent.State) bool {
				d := staffops.Classify(staffops.Signal{
					Kind:         "dead_agent",
					Symptom:      "dead:" + t414Agent,
					Mechanical:   true,
					GraceElapsed: true,
					FleetIntent:  fleet,
					Intent:       agent,
				})
				return d.Action == staffops.ActionRepair
			},
		},
		{
			label:   "worker-idle notification to the parent (🎯T413)",
			control: fleetintent.ControlNotifyIdle,
			acts: func(t *testing.T, fleet, agent fleetintent.State) bool {
				return ShouldEmitWorkerIdle("working", "idle", claudia.PurposeWork, true, fleet, agent)
			},
		},
		{
			// 🎯T408's control. Its acting path calls Launch, which starts a
			// real provider process, so only the declining direction is driven
			// here — the revive direction is left to T408's own coverage
			// rather than faked. Declared, not fudged.
			label:        "delivery start (a message to a stopped agent, 🎯T408)",
			control:      fleetintent.ControlDeliverStart,
			noReviveCase: true,
			acts: func(t *testing.T, fleet, agent fleetintent.State) bool {
				s := t414ServerWithIntent(t, fleet, agent)
				_, _, err := s.ensureAgentProcess(t414Agent)
				// Acting means getting past the intent gate. Any error that
				// does not name the intent is a later failure on the start
				// path, which is the gate having allowed it through.
				return err == nil || !strings.Contains(err.Error(), "intent says it should not be")
			},
		},
	}
}

// t414ServerWithIntent builds a Server whose registry holds a registered but
// unstarted agent, with the durable intent store carrying the fixture state.
func t414ServerWithIntent(t *testing.T, fleet, agent fleetintent.State) *Server {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: t414Agent, WorkDir: dir, SessionID: "s-t414",
		Purpose: claudia.PurposeWork, TargetID: "T500",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fleetintent.Valid(fleet) {
		if err := store.SetFleet(fleet, "jevons", "oracle fixture", t414Now()); err != nil {
			t.Fatal(err)
		}
	}
	if fleetintent.Valid(agent) {
		if err := store.SetAgent(t414Agent, agent, "jevons", "oracle fixture", t414Now()); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{registry: reg}
	s.SetFleetIntentStore(store)
	return s
}

// t414Violations is the whole acceptance-5 cross for one control. It returns
// every way the control broke the rule, tagged by direction so the mutation
// control below can tell the two failure modes apart.
func t414Violations(t *testing.T, c t414Control) []string {
	t.Helper()
	var out []string
	suppress := func(axis string, s fleetintent.State) {
		out = append(out, fmt.Sprintf(
			"suppress: %s acted with %s intent %q — not-running is a fact, never a licence",
			c.label, axis, s))
	}

	for _, s := range fleetintent.AllStates() {
		if s == fleetintent.Working {
			continue
		}
		if c.acts(t, s, fleetintent.Working) {
			suppress("fleet", s)
		}
		if !c.fleetOnly && c.acts(t, fleetintent.Working, s) {
			suppress("agent", s)
		}
		// Both axes set at once: a per-agent park must not become a licence
		// just because the fleet is also stood down, or vice versa.
		if !c.fleetOnly && c.acts(t, s, s) {
			suppress("fleet+agent", s)
		}
	}

	if c.noReviveCase {
		return out
	}
	// Working, and the unstamped state every pre-T414 agent carries. Both must
	// leave the control acting: this is 🎯T414 acceptance 4.
	for _, s := range []fleetintent.State{fleetintent.Working, fleetintent.Unknown} {
		if !c.acts(t, s, s) {
			out = append(out, fmt.Sprintf(
				"revive: %s declined under intent %q — 🎯T171 short-resume and 🎯T208 re-pressure must survive this target",
				c.label, fleetintent.Describe(s)))
		}
	}
	return out
}

// TestT414EveryFleetControlDecidesFromIntent is the target's oracle.
func TestT414EveryFleetControlDecidesFromIntent(t *testing.T) {
	controls := t414Controls()
	// Every control the policy declares must appear here, so adding one to
	// fleetintent without wiring it cannot pass silently.
	covered := map[fleetintent.Control]bool{}
	for _, c := range controls {
		covered[c.control] = true
	}
	for _, want := range fleetintent.AllControls() {
		if !covered[want] {
			t.Errorf("control %q is declared in fleetintent but no oracle drives it", want)
		}
	}

	for _, c := range controls {
		t.Run(c.label, func(t *testing.T) {
			for _, v := range t414Violations(t, c) {
				t.Error(v)
			}
		})
	}
}

// TestT414ReapedAgentIsNamedByNoControl is acceptance 5's third clause, called
// out separately because it is 🎯T413's incident: the product had already
// deregistered jv-t370-auto, and a notifier holding a stale fleet view told the
// PO to continue, re-brief, or restart it.
func TestT414ReapedAgentIsNamedByNoControl(t *testing.T) {
	for _, c := range t414Controls() {
		if c.fleetOnly {
			// No agent row to reap: the fleet-wide axis is covered above.
			continue
		}
		if c.acts(t, fleetintent.Working, fleetintent.Reaped) {
			t.Errorf("%s acted on a reaped agent", c.label)
		}
	}
}

// TestT414OracleDetectsBothFailureModes is the control on the oracle: fed a
// mutant of each shape, the checker must go red, and red in the right
// direction. Without this, "all controls pass" would be equally true of a
// checker that asserts nothing.
func TestT414OracleDetectsBothFailureModes(t *testing.T) {
	// The pre-fix tree: every control acted on the strength of the process
	// being absent, whatever anyone intended.
	preFix := t414Control{
		label:   "mutant: pre-T414 (process state authorises the start)",
		control: fleetintent.ControlRevive,
		acts:    func(*testing.T, fleetintent.State, fleetintent.State) bool { return true },
	}
	v := t414Violations(t, preFix)
	if len(v) == 0 {
		t.Fatal("the oracle passes a control with no intent gate at all")
	}
	states := map[string]bool{}
	for _, s := range v {
		if !strings.HasPrefix(s, "suppress: ") {
			t.Errorf("pre-fix mutant produced a non-suppression violation: %s", s)
		}
		for _, st := range fleetintent.AllStates() {
			if st != fleetintent.Working && strings.Contains(s, string(st)) {
				states[string(st)] = true
			}
		}
	}
	for _, st := range fleetintent.AllStates() {
		if st == fleetintent.Working {
			continue
		}
		if !states[string(st)] {
			t.Errorf("pre-fix mutant went unreported for intent %q", st)
		}
	}

	// The over-broad fix: a stopped process is treated as parked, so nothing
	// is ever revived. Every suppression check above still passes, and crash
	// recovery is silently dead.
	overBroad := t414Control{
		label:   "mutant: over-broad (a dead process is read as a park)",
		control: fleetintent.ControlRevive,
		acts:    func(*testing.T, fleetintent.State, fleetintent.State) bool { return false },
	}
	v = t414Violations(t, overBroad)
	if len(v) == 0 {
		t.Fatal("the oracle passes a control that never revives anything — 🎯T171 and 🎯T208 would be dead with a green suite")
	}
	for _, s := range v {
		if !strings.HasPrefix(s, "revive: ") {
			t.Errorf("over-broad mutant produced a non-revival violation: %s", s)
		}
	}
}

// TestT414DeliveryStartNamesTheIntent: a control that declines must say which
// intent declined it, or the operator is left with a control that appears
// broken rather than obedient.
func TestT414DeliveryStartNamesTheIntent(t *testing.T) {
	s := t414ServerWithIntent(t, fleetintent.Working, fleetintent.Parked)
	_, _, err := s.ensureAgentProcess(t414Agent)
	if err == nil {
		t.Fatal("a message to a parked agent started it — 🎯T408")
	}
	for _, want := range []string{"parked", "agent_intent_parked", "lift the intent"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("decline %q does not contain %q", err.Error(), want)
		}
	}
}

// TestT414StopParksAndParkOutlivesTheProcess wires the doctrine end to end at
// the daemon seam: a deliberate stop records the intent, and the recorded
// intent is what every control then reads.
func TestT414StopParksAndParkOutlivesTheProcess(t *testing.T) {
	s := t414ServerWithIntent(t, fleetintent.Working, fleetintent.Working)
	if !s.AllowFleetControl(t414Agent, fleetintent.ControlDeliverStart).Allow {
		t.Fatal("a working agent was declined before anything stopped it")
	}
	s.MarkAgentParked(t414Agent, "jevons", "anthropic spend block")

	for _, c := range fleetintent.AllControls() {
		if d := s.AllowFleetControl(t414Agent, c); d.Allow {
			t.Errorf("%s still allowed after a deliberate stop", c)
		}
	}
	if sum := s.FleetIntentSummary(); !strings.Contains(sum, t414Agent) {
		t.Errorf("summary %q does not name the stood-down agent", sum)
	}

	s.MarkAgentWorking(t414Agent, "owner", "spend restored")
	for _, c := range fleetintent.AllControls() {
		if d := s.AllowFleetControl(t414Agent, c); !d.Allow {
			t.Errorf("%s still declined after the park was lifted: %s", c, d)
		}
	}
}
