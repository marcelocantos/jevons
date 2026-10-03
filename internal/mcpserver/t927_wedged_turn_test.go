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

	"github.com/marcelocantos/jevons/internal/spool"
)

// 🎯T927 — the oracle for a sidecar seat re-attached after its host handle was
// lost. The fixture replays 2026-09-29: jevons-po's turn was running on one
// handle, the handle was replaced, and from then on the sidecar either ran
// nothing (idle) or held the turn open forever (awaiting a tool result from
// the dead connection). Prompts sent to it were accepted and never answered.

// fakeSidecar stands in for a claudia omp seat. A prompt to a hung seat is
// accepted and queued behind the open turn, exactly as sidecar/seat.ts does;
// abort always ends the turn with a turn_end, which claudia publishes as a
// terminal stop. An idle seat answers each prompt with a turn of its own.
type fakeSidecar struct {
	mu         sync.Mutex
	procs      *wiredProcs
	name       string
	hung       bool
	sent       []string
	answered   []string
	queued     []string
	interrupts int
}

func (f *fakeSidecar) Alive() bool { return true }

func (f *fakeSidecar) Send(text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, text)
	if f.hung {
		f.queued = append(f.queued, text)
		return nil
	}
	f.answerLocked([]string{text})
	return nil
}

// answerLocked runs one turn over texts on the seat's current handle.
func (f *fakeSidecar) answerLocked(texts []string) {
	f.answered = append(f.answered, texts...)
	proc := f.procs.get(f.name)
	reply := "answered: " + strings.Join(texts, " | ")
	go func() {
		time.Sleep(20 * time.Millisecond)
		proc.PublishEvent(terminalStop(reply))
	}()
}

func (f *fakeSidecar) Interrupt() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interrupts++
	f.hung = false
	proc := f.procs.get(f.name)
	go proc.PublishEvent(terminalStop("aborted"))
	// sidecar/seat.ts abort: what was queued behind the aborted turn runs
	// as its own turn once the run has unwound.
	if len(f.queued) > 0 {
		q := f.queued
		f.queued = nil
		f.answerLocked(q)
	}
	return nil
}

func (f *fakeSidecar) snapshot() (sent, answered []string, interrupts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...), append([]string(nil), f.answered...), f.interrupts
}

func t927Fixture(t *testing.T, name string) (*Server, *wiredProcs, *fakeSidecar, *upward) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(spool.DirEnv, filepath.Join(dir, "spool"))
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-" + name,
		Materialized: true, Provider: "anthropic",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	procs := newWiredProcs()
	s.SetProcResolver(procs.get)
	side := &fakeSidecar{procs: procs, name: name}
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return side, false, nil })
	// A sidecar is a live-stream backend: its "accepted" is a session event,
	// and that is all the drain has to go on — the 2026-09-29 reading.
	s.SetTurnWitness(func(_, _ string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, SessionEvent: true}
		}
	})
	up := &upward{}
	s.SetOverseerDeliver(up.deliver)
	return s, procs, side, up
}

func countLines(lines []string, needle string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, needle) {
			n++
		}
	}
	return n
}

// reattachMidTurn brings the fixture to 21:48:26: a turn running on one
// handle, then a new handle wired while the daemon still believes that turn
// is in flight.
func reattachMidTurn(t *testing.T, s *Server, procs *wiredProcs, name string) *claudia.Agent {
	t.Helper()
	old := &claudia.Agent{}
	procs.set(name, old)
	if !s.EnsureAgentEventsWired(name) {
		t.Fatal("first wiring should attach the sink")
	}
	s.noteTurnInFlight(name)
	successor := &claudia.Agent{}
	procs.set(name, successor)
	if !s.EnsureAgentEventsWired(name) {
		t.Fatal("the replacement handle should be wired")
	}
	waitFor(t, "the re-attach to be recorded against the in-flight turn", func() bool {
		_, lost := s.wedges.quietSince(name)
		return lost
	})
	return successor
}

// Clauses 1, 2 and 4: a re-attached seat whose recorded turn is in flight is
// reconciled, and two messages queued behind it are delivered and answered.
func TestT927ReattachedSidecarTurnIsClearedAndQueueAnswered(t *testing.T) {
	for _, tc := range []struct {
		arm  string
		hung bool
	}{
		{"sidecar idle: the turn did not survive the handle", false},
		{"sidecar hung: the turn awaits a tool result from the dead handle", true},
	} {
		t.Run(tc.arm, func(t *testing.T) {
			const name = "jevons-po"
			s, procs, side, up := t927Fixture(t, name)
			side.hung = tc.hung
			reattachMidTurn(t, s, procs, name)

			payloads := []string{
				"[Agent jv-t910-mcpserver-race responded] finish report, oracle green",
				"Supervisor tick 21:57: the frontier is open and you hold no children",
			}
			for _, p := range payloads {
				res, err := deliverObservedToSender(s, name, p, false, side, false)
				if err != nil {
					t.Fatalf("send: %v", err)
				}
				if res.Status != "queued" {
					t.Fatalf("status=%q; want queued behind the believed turn", res.Status)
				}
			}

			start := time.Now()
			// Inside the bound: a turn that has been quiet for ten minutes may
			// be a long tool call. Nothing is interrupted.
			s.SetSweepClock(func() time.Time { return start.Add(10 * time.Minute) })
			s.SweepSendBacklogs()
			if _, _, n := side.snapshot(); n != 0 {
				t.Fatalf("interrupted inside the bound (%d); a quiet turn is not yet a dead one", n)
			}

			s.SetSweepClock(func() time.Time { return start.Add(WedgedTurnAfter + time.Minute) })
			s.SweepSendBacklogs()

			waitFor(t, "both queued messages to be answered", func() bool {
				_, answered, _ := side.snapshot()
				joined := strings.Join(answered, "\n")
				return strings.Contains(joined, payloads[0]) && strings.Contains(joined, payloads[1])
			})
			waitFor(t, "the daemon's queue to empty", func() bool { return observedPendingSends(s, name) == 0 })
			if _, _, n := side.snapshot(); n != 1 {
				t.Fatalf("interrupts=%d; want exactly one clear of the lost turn", n)
			}
			waitFor(t, "the answer to reach the overseer", func() bool {
				return countLines(up.all(), "answered:") > 0
			})
			if got := countLines(up.all(), "WEDGED "+name); got != 1 {
				t.Fatalf("wedge notices=%d; want the overseer told once: %q", got, up.all())
			}
			if !containsLine(up.all(), "interrupted it so the queue drains") {
				t.Fatalf("the notice does not say the turn was cleared: %q", up.all())
			}
			if _, wedged := s.WedgedSeat(name); wedged {
				t.Fatal("seat still flagged wedged after its queue was answered")
			}
		})
	}
}

// Clause 3, and the limit of clause 1: a turn that went quiet on the handle it
// began on is flagged and reported once — never interrupted on silence alone,
// because silence does not tell a long tool call from a dead one.
func TestT927QuietTurnOnItsOwnHandleIsFlaggedOnceNotInterrupted(t *testing.T) {
	const name = "jevons-po"
	s, procs, side, up := t927Fixture(t, name)
	side.hung = true
	proc := &claudia.Agent{}
	procs.set(name, proc)
	s.EnsureAgentEventsWired(name)
	s.noteTurnInFlight(name)
	if _, err := deliverObservedToSender(s, name, "a finish report the PO has not read", false, side, false); err != nil {
		t.Fatalf("send: %v", err)
	}

	start := time.Now()
	s.SetSweepClock(func() time.Time { return start.Add(WedgedTurnAfter + time.Minute) })
	for range 3 {
		s.SweepSendBacklogs()
	}
	if got := countLines(up.all(), "WEDGED "+name); got != 1 {
		t.Fatalf("wedge notices=%d over three sweeps; want exactly one: %q", got, up.all())
	}
	if !containsLine(up.all(), "mode=interrupt clears it") {
		t.Fatalf("the notice does not name the remedy: %q", up.all())
	}
	line, wedged := s.WedgedSeat(name)
	if !wedged || !strings.HasPrefix(line, "WEDGED "+name) {
		t.Fatalf("WedgedSeat=%q,%v; want the seat flagged for /api/agents", line, wedged)
	}
	if _, _, n := side.snapshot(); n != 0 {
		t.Fatalf("interrupted %d time(s) on silence alone", n)
	}

	// The turn ends: the flag goes, and the held message drains.
	proc.PublishEvent(terminalStop("done with the long gate"))
	waitFor(t, "the held message to be handed over", func() bool {
		sent, _, _ := side.snapshot()
		return len(sent) == 1
	})
	if _, wedged := s.WedgedSeat(name); wedged && s.flightState(name) != FlightInFlight {
		t.Fatal("wedge flag outlived the turn it described")
	}
}

// Motion on the replacement handle means the turn is running there: the lost
// handle is forgotten and the turn is not interrupted, however long it runs.
func TestT927MotionOnTheNewHandleIsNotALostTurn(t *testing.T) {
	const name = "jevons-po"
	s, procs, side, _ := t927Fixture(t, name)
	successor := reattachMidTurn(t, s, procs, name)
	if _, err := deliverObservedToSender(s, name, "queued behind a turn that is still working", false, side, false); err != nil {
		t.Fatalf("send: %v", err)
	}
	// A bare acceptance is not motion…
	successor.PublishEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressPromptAccepted})
	if _, lost := s.wedges.quietSince(name); !lost {
		t.Fatal("an acceptance cleared the lost-handle record; a hung sidecar accepts prompts too")
	}
	// …streamed output is.
	successor.PublishEvent(claudia.Event{Type: "assistant", Text: "still working"})
	if _, lost := s.wedges.quietSince(name); lost {
		t.Fatal("output on the new handle did not clear the lost-handle record")
	}
	start := time.Now()
	s.SetSweepClock(func() time.Time { return start.Add(WedgedTurnAfter + time.Minute) })
	s.SweepSendBacklogs()
	if _, _, n := side.snapshot(); n != 0 {
		t.Fatalf("interrupted a turn that was moving on its new handle (%d)", n)
	}
}

// 🎯T937: the re-attach note runs on a goroutine after the attach. A turn
// that begins in between began on the new handle and is not lost — before the
// fix, the note read flight when it ran, called that turn lost, and the sweep
// interrupted it (TestT927QuietTurnOnItsOwnHandleIsFlaggedOnceNotInterrupted
// failed 15/20). Each arm drives the note in the order the scheduler may pick.
func TestT937ReattachNoteNamesTheTurnSeenAtAttach(t *testing.T) {
	for _, tc := range []struct {
		arm        string
		beforeSnap func(s *Server, name string)
		afterSnap  func(s *Server, name string)
		wantLost   bool
	}{
		{
			arm:       "turn begins after the attach: on the new handle, not lost",
			afterSnap: func(s *Server, name string) { s.noteTurnInFlight(name) },
		},
		{
			arm:        "turn in flight across the attach: lost",
			beforeSnap: func(s *Server, name string) { s.noteTurnInFlight(name) },
			wantLost:   true,
		},
		{
			arm:        "turn ends and another begins before the note runs: not lost",
			beforeSnap: func(s *Server, name string) { s.noteTurnInFlight(name) },
			afterSnap: func(s *Server, name string) {
				s.noteTurnEnded(name)
				s.noteTurnInFlight(name)
			},
		},
	} {
		t.Run(tc.arm, func(t *testing.T) {
			const name = "jevons-po"
			s, _, _, _ := t927Fixture(t, name)
			if tc.beforeSnap != nil {
				tc.beforeSnap(s, name)
			}
			turn, inFlight := s.wedges.turnAt(name)
			if tc.afterSnap != nil {
				tc.afterSnap(s, name)
			}
			s.noteReattachedMidTurn(name, turn, inFlight, false)
			if _, lost := s.wedges.quietSince(name); lost != tc.wantLost {
				t.Fatalf("lost handle recorded=%v; want %v", lost, tc.wantLost)
			}
		})
	}
}

// syncAbortSidecar is a fakeSidecar whose abort ends the turn before
// Interrupt returns: the terminal stop is published on the caller's goroutine.
type syncAbortSidecar struct{ *fakeSidecar }

func (f syncAbortSidecar) Interrupt() error {
	f.mu.Lock()
	f.interrupts++
	f.hung = false
	proc := f.procs.get(f.name)
	f.mu.Unlock()
	proc.PublishEvent(terminalStop("aborted"))
	return nil
}

// 🎯T937: when the abort's terminal stop lands before Interrupt returns, it
// ends the turn and forgets the wedge record. The clear used to re-read that
// record and announce a zero WedgedTurn — "believed in flight for
// 2562047h…", zero messages waiting, and the manual-interrupt remedy for a
// turn the daemon had just interrupted.
func TestT937AbortEndingTheTurnFirstStillAnnouncesTheClear(t *testing.T) {
	const name = "jevons-po"
	s, procs, side, up := t927Fixture(t, name)
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return syncAbortSidecar{side}, false, nil })
	reattachMidTurn(t, s, procs, name)
	if _, err := deliverObservedToSender(s, name, "queued behind the lost turn", false, side, false); err != nil {
		t.Fatalf("send: %v", err)
	}

	start := time.Now()
	s.SetSweepClock(func() time.Time { return start.Add(WedgedTurnAfter + time.Minute) })
	s.SweepSendBacklogs()

	var notice string
	for _, l := range up.all() {
		if strings.Contains(l, "WEDGED "+name) {
			notice = l
		}
	}
	if !strings.Contains(notice, "interrupted it so the queue drains") ||
		!strings.Contains(notice, "1 message(s) wait") ||
		!strings.Contains(notice, "for 16m0s") {
		t.Fatalf("the notice misreports the clear: %q", up.all())
	}
	waitFor(t, "the queued message to be answered", func() bool {
		_, answered, _ := side.snapshot()
		return len(answered) == 1
	})
	if f := s.flightState(name); f == FlightUnknown {
		t.Fatalf("flight=%v; the abort's terminal stop already settled it", f)
	}
}

// 🎯T963: a process attached while its seat's launch is in flight is that
// launch's successor. The turn believed in flight is not recorded as lost on
// a replaced handle, and nothing is warned. The check runs on a goroutine
// after the attach, so the launch state is the one captured at the attach:
// T426A's sweep otherwise tripped the warning whenever the goroutine ran
// before the test looked.
func TestT963AttachDuringALaunchIsNotALostHandle(t *testing.T) {
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })
	const name = "jevons-po"
	s, _, _, _ := t927Fixture(t, name)
	s.noteTurnInFlight(name)
	turn, inFlight := s.wedges.turnAt(name)
	s.noteReattachedMidTurn(name, turn, inFlight, true)
	if _, lost := s.wedges.quietSince(name); lost {
		t.Fatal("an attach during a launch recorded a lost handle")
	}
	for _, r := range cap.snapshot() {
		if r.Level >= slog.LevelWarn {
			t.Fatalf("warned %q for an attach during a launch", r.Message)
		}
	}
	// The control: the same attach with no launch in flight is a lost handle.
	s.noteReattachedMidTurn(name, turn, inFlight, false)
	if _, lost := s.wedges.quietSince(name); !lost {
		t.Fatal("control: an attach mid-turn with no launch did not record a lost handle")
	}
}
