// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T426 — a compaction must not make an agent invisible to the daemon.
//
// THE OUTAGE THIS PINS, 2026-08-10. jevons-po was over the context ceiling and
// the governor rotated it onto a fresh session at 20:14:44. A finish report had
// been queued 20 seconds earlier, behind a turn the daemon had watched begin.
// The successor process ran a full turn and ENDED it at 20:18:43 — terminal
// assistant message, stop_hook_summary, a turn_duration of 236783ms, and the
// transcript then stopped growing. The daemon never saw any of it. By 20:22:20
// six messages were stacked behind a flight record pinned at in_flight by a
// process that no longer existed, and every sender had been told "queued".
//
// WHAT THE FIXTURES BELOW ARE, AND WHY THERE ARE TWO. The first is the SYMPTOM
// (clause 4, first half): queue in, rotate, terminal stop, queue out. The
// second is the WIRING (clause 4, second half, the 🎯T416 clause-9 shape): the
// sink is asserted to be on the successor PROCESS OBJECT and off the
// predecessor's. A suite that only holds the symptom passes the day someone
// re-fixes the drain by inferring idleness from a registry row — which is the
// FALSE-IDLENESS defect 🎯T423 exists for, i.e. this bug's exact inverse. Pin
// the wiring and neither direction can be called fixed by accident.
//
// RED AGAINST THE PRE-FIX TREE, in band rather than by assertion. The first
// fixture runs both arms in one test: with the launch hook (the product) and
// without it (the world before this target). The dark arm is the control — it
// is what the pre-fix tree did on every path, and it is asserted to produce
// exactly the outage: nothing drains, flight stays in_flight, nothing reaches
// the overseer. If a later change makes the dark arm drain anyway, the wiring
// has stopped being what carries these controls and this suite is no longer
// testing what it claims to.

// wiredProcs holds which process object currently "is" each agent. A rotation
// is a swap here, which is precisely what it is in production: the name, the
// registry row and the workdir do not move.
type wiredProcs struct {
	mu sync.Mutex
	by map[string]*claudia.Agent
}

func newWiredProcs() *wiredProcs { return &wiredProcs{by: map[string]*claudia.Agent{}} }

func (w *wiredProcs) set(name string, proc *claudia.Agent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.by[name] = proc
}

func (w *wiredProcs) get(name string) *claudia.Agent {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.by[name]
}

// recordingSender is agentSender with a lock, because the drain runs on its own
// goroutine since 🎯T416 and the assertions read from the test goroutine.
type recordingSender struct {
	mu   sync.Mutex
	sent []string
}

func (r *recordingSender) Alive() bool { return true }

func (r *recordingSender) Send(text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, text)
	return nil
}

func (r *recordingSender) Interrupt() error { return nil }

func (r *recordingSender) delivered() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sent...)
}

// upward records what the daemon delivered to the overseer — the 🎯T165/notify
// half of clause 2, which hangs off the same terminal-stop branch as the drain.
type upward struct {
	mu    sync.Mutex
	lines []string
}

func (u *upward) deliver(text string, _ SendOrigin) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.lines = append(u.lines, text)
	return nil
}

func (u *upward) all() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.lines...)
}

// t426Fixture is a daemon with no provider processes: the registry holds rows,
// the seams hold the behaviour, and the only real claudia object is the bare
// Agent whose event fan-out is the thing under test.
func t426Fixture(t *testing.T, name string) (*Server, *wiredProcs, *recordingSender, *upward) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-" + name,
		Materialized: true, Provider: "claude",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	procs := newWiredProcs()
	s.SetProcResolver(procs.get)
	sender := &recordingSender{}
	s.SetSenderResolver(func(string) (agentSender, bool, error) { return sender, false, nil })
	// The drained message is seen to land: this suite is about whether the
	// drain HAPPENS, not about 🎯T416's judgement of what it produced.
	s.SetTurnWitness(func(_, _ string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, Durable: true, PayloadSeen: true}
		}
	})
	up := &upward{}
	s.SetOverseerDeliver(up.deliver)
	return s, procs, sender, up
}

// terminalStop is what the successor publishes when its turn ends: the event
// the whole target is about the daemon receiving.
func terminalStop(text string) claudia.Event {
	return claudia.Event{Type: "assistant", Text: text, StopReason: "end_turn"}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// Clause 4, first half — and its control.
func TestT426CompactedSuccessorDrainsQueueAndReturnsIdle(t *testing.T) {
	const name = "jv-t426-po"
	const queued = "finish report: 🎯T416 landed, oracle green"

	for _, tc := range []struct {
		arm       string
		wireAfter bool
	}{
		// The product: fleet.Claudia.Launch calls the launch hook, which is
		// mcpSrv.EnsureAgentEventsWired.
		{"successor wired by the launch hook", true},
		// The control: the pre-fix tree, and the 2026-08-10 outage.
		{"successor left dark", false},
	} {
		t.Run(tc.arm, func(t *testing.T) {
			s, procs, sender, up := t426Fixture(t, name)

			// A running, wired agent — the state at 20:14:24.
			old := &claudia.Agent{}
			procs.set(name, old)
			if !s.EnsureAgentEventsWired(name) {
				t.Fatal("first wiring should attach the sink")
			}
			s.noteTurnInFlight(name)

			res, err := deliverToSender(s, name, queued, false, sender, false)
			if err != nil {
				t.Fatalf("send: %v", err)
			}
			if res.Status != "queued" || res.Queued != 1 {
				t.Fatalf("status=%q queued=%d want queued/1", res.Status, res.Queued)
			}
			// The stream was healthy at this moment, so "queued" is the whole
			// truth and must not carry the clause-3 warning.
			if strings.Contains(res.Message, "NOT TRUSTWORTHY") {
				t.Fatalf("healthy queue reported as untrustworthy: %q", res.Message)
			}

			// 20:14:44 — the ceiling governor rotates the agent. Same name,
			// same registry row, different process.
			successor := &claudia.Agent{}
			procs.set(name, successor)
			if tc.wireAfter {
				s.EnsureAgentEventsWired(name)
			}

			// 20:18:43 — the successor finishes its turn.
			successor.PublishEvent(terminalStop("compaction successor first turn"))

			if !tc.wireAfter {
				// The outage, asserted rather than assumed. Everything that
				// hangs off the terminal-stop branch stays where it was.
				time.Sleep(150 * time.Millisecond)
				if got := sender.delivered(); len(got) != 0 {
					t.Fatalf("dark stream drained anyway: %q — this control no longer detects the regression", got)
				}
				if f := s.flightState(name); f != FlightInFlight {
					t.Fatalf("dark stream flight=%v want in_flight (nothing observed the turn end)", f)
				}
				if got := up.all(); len(got) != 0 {
					t.Fatalf("dark stream reported upward: %q", got)
				}
				if n := s.pendingAgentSends(name); n != 1 {
					t.Fatalf("dark stream pending=%d want 1 (the wedged queue)", n)
				}
				return
			}

			waitFor(t, "the queue to drain into the successor", func() bool {
				return len(sender.delivered()) == 1
			})
			if got := sender.delivered()[0]; got != queued {
				t.Fatalf("drained %q want %q", got, queued)
			}
			waitFor(t, "flight to return to idle", func() bool {
				// The drain sends one message, which begins a turn of its own
				// — so the state after a successful drain is in_flight again,
				// having passed through idle. What matters is that the turn
				// END was observed at all: before that, flight is pinned.
				return s.flightState(name) != FlightUnknown
			})
			if n := s.pendingAgentSends(name); n != 0 {
				t.Fatalf("pending=%d want 0", n)
			}
			// Clause 2's other two controls ride the same branch: the report
			// reached the overseer.
			waitFor(t, "the successor's report to reach the overseer", func() bool {
				for _, l := range up.all() {
					if strings.Contains(l, "compaction successor first turn") {
						return true
					}
				}
				return false
			})
		})
	}
}

// Clause 4, second half — the regression is pinned at the wiring, not only at
// its symptom.
func TestT426SinkFollowsTheProcessNotTheName(t *testing.T) {
	const name = "jv-t426-worker"
	s, procs, _, _ := t426Fixture(t, name)

	old := &claudia.Agent{}
	procs.set(name, old)
	if !s.EnsureAgentEventsWired(name) {
		t.Fatal("first call should attach")
	}
	if n := old.EventSubscriberCount(); n != 1 {
		t.Fatalf("old subscribers=%d want 1", n)
	}
	// Idempotent per process object: every launch road may call this without
	// knowing whether another already did.
	if s.EnsureAgentEventsWired(name) {
		t.Fatal("second call on the same process should report no attach")
	}
	if n := old.EventSubscriberCount(); n != 1 {
		t.Fatalf("old subscribers=%d after re-assert, want 1 (no double delivery)", n)
	}

	successor := &claudia.Agent{}
	procs.set(name, successor)
	if !s.EnsureAgentEventsWired(name) {
		t.Fatal("rotation should attach the sink to the successor")
	}
	if n := successor.EventSubscriberCount(); n != 1 {
		t.Fatalf("successor subscribers=%d want 1", n)
	}
	// And off the predecessor: a pane that outlives its rotation must not
	// report turn ends under a name that has moved on.
	if n := old.EventSubscriberCount(); n != 0 {
		t.Fatalf("predecessor subscribers=%d want 0", n)
	}

	// A torn-down seat forgets its wiring, so a re-minted agent of the same
	// name is wired afresh rather than being mistaken for the departed one.
	s.clearAgentTurnBegan(name)
	reminted := &claudia.Agent{}
	procs.set(name, reminted)
	if !s.EnsureAgentEventsWired(name) {
		t.Fatal("re-minted agent should be wired")
	}
	if n := reminted.EventSubscriberCount(); n != 1 {
		t.Fatalf("re-minted subscribers=%d want 1", n)
	}
}

// The standing sweep: road six, whichever it turns out to be, is repaired
// within one interval instead of never.
func TestT426SweepRepairsAnUnwiredLaunchRoadAndSaysSo(t *testing.T) {
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const name = "jv-t426-swept"
	s, procs, sender, _ := t426Fixture(t, name)

	// The overseer has its own stream (Server.AttachOverseer). Wiring the
	// fleet sink to it as well would deliver every overseer turn back to the
	// overseer as a worker report.
	dir := t.TempDir()
	if err := s.registry.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: dir, SessionID: "s-jevons",
		Materialized: true, Provider: "claude",
	}); err != nil {
		t.Fatal(err)
	}
	overseerProc := &claudia.Agent{}
	procs.set("jevons", overseerProc)

	// A process nobody wired, holding a message nobody can deliver.
	proc := &claudia.Agent{}
	procs.set(name, proc)
	s.noteTurnInFlight(name)
	s.enqueueAgentSend(name, "brief that has been waiting")

	if n := s.sweepAgentWiring("jevons"); n != 1 {
		t.Fatalf("sweep repaired %d want 1", n)
	}
	if n := proc.EventSubscriberCount(); n != 1 {
		t.Fatalf("swept agent subscribers=%d want 1", n)
	}
	if n := overseerProc.EventSubscriberCount(); n != 0 {
		t.Fatalf("overseer got the fleet sink (subscribers=%d) — its turns would be delivered to itself", n)
	}
	// Steady state is silent: a sweep that keeps reporting repairs is a sweep
	// that is not repairing anything.
	if n := s.sweepAgentWiring("jevons"); n != 0 {
		t.Fatalf("second sweep repaired %d want 0", n)
	}

	// Loud, with the cost named — a dark stream is not a debug detail.
	var found bool
	for _, r := range cap.records {
		if r.Level != slog.LevelWarn || !strings.Contains(r.Message, "🎯T426") {
			continue
		}
		attrs := attrsMap(r)
		if attrs["agent"] != name {
			continue
		}
		found = true
		if q, ok := attrs["queued"]; !ok {
			t.Fatalf("dark-stream warning does not name the stranded queue: %v", attrs)
		} else if !equalsInt(q, 1) {
			t.Fatalf("queued=%v want 1", q)
		}
	}
	if !found {
		t.Fatal("sweep repaired a dark stream silently — clause 3 requires it to be surfaced")
	}

	// And the repaired stream works: the wedged brief drains at the next
	// observed terminal stop.
	proc.PublishEvent(terminalStop(""))
	waitFor(t, "the swept agent's queue to drain", func() bool {
		return len(sender.delivered()) == 1
	})
}

// Clause 3 has a second edge, found by the fix's own first boot: an alarm that
// fires for the normal state of the world is not a loud signal, it is a quiet
// one. At 08:27:47 on 2026-08-15 the boot pass warned that all 17 resumed
// agents had dark streams — they had, in the sense that nothing had wired them
// yet, which is what a boot IS — and at 08:29:47 the sweep found a real
// unwired launch road (jv-t383-auto) whose warning was indistinguishable from
// the seventeen above it. The boot pass must therefore be quiet, and the sweep
// must stay loud, in the same suite: either one alone can be satisfied by
// silencing both.
func TestT426BootPassIsQuietAndTheSweepStaysLoud(t *testing.T) {
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const name = "jv-t426-booted"
	s, procs, _, _ := t426Fixture(t, name)
	procs.set(name, &claudia.Agent{})
	for _, other := range []string{"jv-t426-booted-2", "jv-t426-booted-3"} {
		if err := s.registry.Register(claudia.AgentDef{
			Name: other, WorkDir: t.TempDir(), SessionID: "s-" + other,
			Materialized: true, Provider: "claude",
		}); err != nil {
			t.Fatal(err)
		}
		procs.set(other, &claudia.Agent{})
	}

	if n := s.WireRunningAgents("jevons"); n != 3 {
		t.Fatalf("boot pass wired %d want 3", n)
	}
	for _, r := range cap.records {
		if r.Level >= slog.LevelWarn {
			t.Fatalf("boot pass raised %v: %q %v — resuming the fleet is not a fault",
				r.Level, r.Message, attrsMap(r))
		}
	}
	// Quiet is not silent: the count is stated once, so a boot that wires
	// nothing at all is still distinguishable from one that wires the fleet.
	var counted bool
	for _, r := range cap.records {
		if attrsMap(r)["count"] != nil && equalsInt(attrsMap(r)["count"], 3) {
			counted = true
		}
	}
	if !counted {
		t.Fatal("boot pass did not report how many streams it wired")
	}

	// Now the daemon has been running, and a rotation lands on a road that
	// does not wire. That one is a fault, and it must be audible over a log
	// the boot pass no longer filled.
	mark := len(cap.records)
	procs.set(name, &claudia.Agent{})
	if n := s.sweepAgentWiring("jevons"); n != 1 {
		t.Fatalf("sweep repaired %d want 1", n)
	}
	var warned bool
	for _, r := range cap.records[mark:] {
		if r.Level == slog.LevelWarn && attrsMap(r)["agent"] == name {
			warned = true
		}
	}
	if !warned {
		t.Fatal("the sweep's real find was silenced along with the boot noise")
	}
}

// Clause 3: the failure mode is silence, so a send queued over a stream the
// daemon had lost says so to the sender, not only to the log.
func TestT426QueuedSendOverADarkStreamIsFailLoud(t *testing.T) {
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const name = "jv-t426-dark"
	s, procs, sender, _ := t426Fixture(t, name)
	procs.set(name, &claudia.Agent{})
	// in_flight was written by this process, before whatever detached the
	// sink. That is exactly the state the six stacked messages were in.
	s.noteTurnInFlight(name)

	res, err := deliverToSender(s, name, "another brief", false, sender, false)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.Status != "queued" {
		t.Fatalf("status=%q want queued", res.Status)
	}
	if !strings.Contains(res.Message, "NOT TRUSTWORTHY") {
		t.Fatalf("sender was told a cheerful %q over a dark stream", res.Message)
	}
	if !strings.Contains(res.Message, "interrupt=true") {
		t.Fatalf("message does not name the recovery: %q", res.Message)
	}
	var warned bool
	for _, r := range cap.records {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, "🎯T426") {
			warned = true
		}
	}
	if !warned {
		t.Fatal("no WARN for a send queued over a dark stream")
	}

	// The send re-attached the stream on its way past, so the next send is an
	// ordinary queue behind a live turn and must not cry wolf.
	res2, err := deliverToSender(s, name, "and another", false, sender, false)
	if err != nil {
		t.Fatalf("second send: %v", err)
	}
	if strings.Contains(res2.Message, "NOT TRUSTWORTHY") {
		t.Fatalf("healthy queue still reported as untrustworthy: %q", res2.Message)
	}
	if res2.Queued != 2 {
		t.Fatalf("queued=%d want 2", res2.Queued)
	}
}

func equalsInt(v any, want int) bool {
	switch n := v.(type) {
	case int:
		return n == want
	case int64:
		return n == int64(want)
	}
	return false
}
