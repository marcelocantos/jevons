// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/sendq"
)

type queueAttemptSender struct{ send func(string) error }

func (p queueAttemptSender) Send(text string) error { return p.send(text) }
func (queueAttemptSender) Alive() bool              { return true }
func (queueAttemptSender) Interrupt() error         { return nil }

func TestT623DrainDiesAfterSubmit(t *testing.T) {
	const helper = "JEVONS_T623_CRASH_QUEUE"
	if dir := os.Getenv(helper); dir != "" {
		s, _, _ := t418Daemon(t, dir)
		s.SetSenderResolver(func(string) (agentSender, bool, error) {
			return queueAttemptSender{send: func(string) error { os.Exit(23); return nil }}, false, nil
		})
		s.drainAgentSendQueue("a")
		t.Fatal("helper never submitted")
	}
	dir := t.TempDir()
	s, _, _ := t418Daemon(t, dir)
	original, _, err := s.sendQueue().Append("a", "must survive the submission window", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestT623DrainDiesAfterSubmit$")
	cmd.Env = append(os.Environ(), helper+"="+dir)
	out, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 23 {
		t.Fatalf("crash helper: %v %s", err, out)
	}
	after, receiver, up := t418Daemon(t, dir)
	after.ReportRecoveredBacklog()
	after.SweepSendBacklogs()
	after.drainAgentSendQueue("a")
	entries, err := after.sendQueue().Snapshot("a")
	if err != nil || len(entries) != 1 || entries[0].ID != original.ID || entries[0].Text != original.Text || entries[0].State != sendq.Attempting {
		t.Fatalf("submission crash lost accepted payload: %+v %v", entries, err)
	}
	if len(receiver.delivered()) != 0 {
		t.Fatal("restarted daemon blindly repeated submission")
	}
	pin, ok := after.sendqPinFor("a")
	if !ok || pin.EntryID != original.ID || pin.AttemptID == "" || !strings.Contains(FormatSendqPinLine("a", pin), "will not retry") {
		t.Fatalf("recovered attempt not visible: %+v %v", pin, ok)
	}
	if !containsLine(up.all(), "uncertain") {
		t.Fatalf("recovery concealed uncertainty: %v", up.all())
	}
}

func TestT623DrainErrorNeverLosesOrBlindlyRetriesPayload(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		retry bool
	}{
		{"known busy refusal", errors.New("grok acp: prompt already in flight"), true},
		{"process gone before write", errors.New("claude process not running"), true},
		{"EOF after write", io.EOF, false},
		{"closed awaiting response", errors.New("codex app-server: closed waiting for turn/start"), false},
		{"unrecognized provider error", errors.New("provider failed"), false},
		{"quoted busy text", errors.New("provider response quoted: grok acp: prompt already in flight"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s, _, _ := t418Daemon(t, dir)
			first, _, err := s.sendQueue().Append("a", "first accepted request", time.Now().Add(-time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.sendQueue().Append("a", "later request", time.Now()); err != nil {
				t.Fatal(err)
			}
			s.SetSenderResolver(func(string) (agentSender, bool, error) {
				return queueAttemptSender{send: func(string) error { return tc.err }}, false, nil
			})
			// An unrelated live event is not a receipt for an errored send.
			s.SetTurnWitness(func(string, string) turnWatch {
				return func() TurnEvidence { return TurnEvidence{Observed: true, SessionEvent: true} }
			})
			s.drainAgentSendQueue("a")
			entries, err := s.sendQueue().Snapshot("a")
			want := sendq.Uncertain
			if tc.retry {
				want = sendq.Pending
			}
			if err != nil || len(entries) != 2 || entries[0].ID != first.ID || entries[0].Text != first.Text || !entries[0].EnqueuedAt.Equal(first.EnqueuedAt) || entries[0].State != want {
				t.Fatalf("drain changed/lost acceptance: %+v %v", entries, err)
			}
			after, receiver, _ := t418Daemon(t, dir)
			after.drainAgentSendQueue("a")
			if got := len(receiver.delivered()); (got == 1) != tc.retry {
				t.Fatalf("restart submissions=%d retry=%v", got, tc.retry)
			}
		})
	}
}

func TestT623DrainClaimAndResolutionMustReachDisk(t *testing.T) {
	for _, at := range []string{"claim", "resolution"} {
		t.Run(at, func(t *testing.T) {
			dir := t.TempDir()
			s, _, _ := t418Daemon(t, dir)
			for _, text := range []string{"head", "tail"} {
				if _, err := s.enqueueAgentSend("a", text); err != nil {
					t.Fatal(err)
				}
			}
			blocker := filepath.Join(dir, "sendq", "a.json.tmp")
			calls := 0
			block := func() {
				if err := os.Mkdir(blocker, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if at == "claim" {
				block()
			}
			s.SetSenderResolver(func(string) (agentSender, bool, error) {
				return queueAttemptSender{send: func(string) error { calls++; block(); return nil }}, false, nil
			})
			s.drainAgentSendQueue("a")
			if at == "claim" && calls != 0 {
				t.Fatal("sent without durable claim")
			}
			entries, err := s.sendQueue().Snapshot("a")
			if err != nil || len(entries) != 2 || entries[0].Text != "head" {
				t.Fatalf("failed write lost payload: %+v %v", entries, err)
			}
			if at == "resolution" && (calls != 1 || entries[0].State != sendq.Attempting) {
				t.Fatalf("failed completion lost claim: %+v calls=%d", entries, calls)
			}
			if at == "resolution" {
				if pin, blocked := s.sendqPinFor("a"); !blocked || pin.AttemptID != entries[0].AttemptID {
					t.Fatalf("failed resolution still appears actively owned: %+v blocked=%v", pin, blocked)
				}
			}
			if err := os.Remove(blocker); err != nil {
				t.Fatal(err)
			}
			if at == "resolution" {
				after, receiver, _ := t418Daemon(t, dir)
				after.drainAgentSendQueue("a")
				if len(receiver.delivered()) != 0 {
					t.Fatal("failed resolution caused a duplicate after restart")
				}
			}
		})
	}
}

func TestT623TerminalDuringWitnessPreservesNextDrain(t *testing.T) {
	s, _, _ := t418Daemon(t, t.TempDir())
	for _, text := range []string{"first", "second"} {
		if _, err := s.enqueueAgentSend("a", text); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	s.SetSenderResolver(func(string) (agentSender, bool, error) {
		return queueAttemptSender{send: func(text string) error {
			got = append(got, text)
			s.noteTurnEnded("a")
			s.drainAgentSendQueue("a") // terminal's drain races the retained claim
			return nil
		}}, false, nil
	})
	s.drainAgentSendQueue("a")
	if len(got) != 2 || got[0] != "first" || got[1] != "second" || s.pendingAgentSends("a") != 0 || s.flightState("a") != FlightIdle {
		t.Fatalf("terminal wakeup lost: sent=%v depth=%d flight=%v", got, s.pendingAgentSends("a"), s.flightState("a"))
	}
}

func TestT623AutomaticCleanupCannotEraseOrRouteAttempts(t *testing.T) {
	s, receiver, _ := t418Daemon(t, t.TempDir())
	if _, err := s.enqueueAgentSend("a", "held payload"); err != nil {
		t.Fatal(err)
	}
	attempt, _, err := s.sendQueue().ClaimFront("a")
	if err != nil {
		t.Fatal(err)
	}
	backlogs, err := s.sendQueue().Backlogs()
	if err != nil {
		t.Fatal(err)
	}
	s.reapBacklogForMissingAgent(backlogs[0], time.Now())
	s.routeHeldReapedBacklog(backlogs[0], fleetintent.Record{}, time.Now())
	entries, err := s.sendQueue().Snapshot("a")
	if err != nil || len(entries) != 1 || entries[0] != attempt || len(receiver.delivered()) != 0 {
		t.Fatalf("cleanup consumed an attempt: %+v %v", entries, err)
	}
}

func TestT623UnusableQueueDirectoryNeverFallsBackToMemory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state-is-a-file")
	if err := os.WriteFile(dir, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	s.SetSendQueueDir(dir)
	res, err := deliverToSender(s, "a", "must be durably held", false, &fakeSender{alive: true, inFlight: true}, false)
	if err == nil || res.Status == "queued" {
		t.Fatalf("unusable spool falsely accepted message: %+v %v", res, err)
	}
}

func TestT623SuccessfulLiveStreamVerdictStillDrains(t *testing.T) {
	s, receiver, _ := t418Daemon(t, t.TempDir())
	s.SetTurnWitness(witnessYielding(TurnEvidence{Observed: true, SessionEvent: true}))
	if _, err := s.enqueueAgentSend("a", "normal stream-backed send"); err != nil {
		t.Fatal(err)
	}
	s.drainAgentSendQueue("a")
	if len(receiver.delivered()) != 1 || s.pendingAgentSends("a") != 0 {
		t.Fatal("durability change froze the existing healthy stream path")
	}
}

func TestT623ConcurrentDrainAndAppendPreserveFIFO(t *testing.T) {
	s, _, up := t418Daemon(t, t.TempDir())
	if _, err := s.enqueueAgentSend("a", "first"); err != nil {
		t.Fatal(err)
	}
	entered, proceed, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	calls := 0
	s.SetSenderResolver(func(string) (agentSender, bool, error) {
		return queueAttemptSender{send: func(string) error { calls++; close(entered); <-proceed; return nil }}, false, nil
	})
	go func() { defer close(done); s.drainAgentSendQueue("a") }()
	<-entered
	if pin, ok := s.sendqPinFor("a"); ok {
		t.Errorf("healthy active submission was pinned: %+v", pin)
	}
	s.SweepSendBacklogs()
	if containsLine(up.all(), "reconcile") {
		t.Errorf("healthy submission raised an orphan alarm: %v", up.all())
	}
	if _, err := s.enqueueAgentSend("a", "second"); err != nil {
		t.Fatal(err)
	}
	s.drainAgentSendQueue("a")
	close(proceed)
	<-done
	entries, err := s.sendQueue().Snapshot("a")
	if err != nil || calls != 1 || len(entries) != 1 || entries[0].Text != "second" || entries[0].State != sendq.Pending {
		t.Fatalf("concurrent drain duplicated or lost an arrival: calls=%d entries=%+v err=%v", calls, entries, err)
	}
}

func TestT623BusyRefusalsRemainOrdinaryWaiting(t *testing.T) {
	s, _, _ := t418Daemon(t, t.TempDir())
	if _, err := s.enqueueAgentSend("a", "waiting for turn end"); err != nil {
		t.Fatal(err)
	}
	s.SetSenderResolver(func(string) (agentSender, bool, error) {
		return queueAttemptSender{send: func(string) error { return errors.New("grok acp: prompt already in flight") }}, false, nil
	})
	for range SendqPinFailureThreshold + 1 {
		s.drainAgentSendQueue("a")
	}
	if pin, ok := s.sendqPinFor("a"); ok {
		t.Fatalf("ordinary busy seat was pinned: %+v", pin)
	}
}

func TestT623ReapedRoutingKeepsConcurrentArrivals(t *testing.T) {
	const agent, parent = "reaped-worker", "jevons-po"
	f := t582Server(t, agent, parent, fleetlog.ReasonReapDone)
	if _, err := f.s.enqueueAgentSend(agent, "old held message"); err != nil {
		t.Fatal(err)
	}
	f.s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		if name != parent {
			return nil, false, nil
		}
		return queueAttemptSender{send: func(string) error { _, err := f.s.enqueueAgentSend(agent, "arrived during routing"); return err }}, false, nil
	})
	backlogs, err := f.s.sendQueue().Backlogs()
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := LookupReapedRecord(f.s.fleetIntent(), agent)
	f.s.routeHeldReapedBacklog(backlogs[0], rec, f.now)
	entries, err := f.s.sendQueue().Snapshot(agent)
	if err != nil || len(entries) != 1 || entries[0].Text != "arrived during routing" {
		t.Fatalf("routing erased concurrent arrival: %+v %v", entries, err)
	}
}

func TestT623WatcherCanBeReleasedWithoutAwaiting(t *testing.T) {
	obs := newFakeObserver("")
	_, cancel := observeTurnForCancelable(obs, "payload", time.Hour)
	if len(obs.subs) != 1 {
		t.Fatal("watch never subscribed")
	}
	cancel()
	cancel()
	if len(obs.subs) != 0 {
		t.Fatal("pre-submit refusal leaked its subscription")
	}
}

func TestT623CleanupUsesOriginalEntryIDs(t *testing.T) {
	for _, mode := range []string{"missing", "reaped"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _ := t418Daemon(t, t.TempDir())
			for _, text := range []string{"first", "second"} {
				if _, err := s.enqueueAgentSend("a", text); err != nil {
					t.Fatal(err)
				}
			}
			backlogs, err := s.sendQueue().Backlogs()
			if err != nil {
				t.Fatal(err)
			}
			// Another drain consumes one old entry and a new one arrives before
			// cleanup gets the stale snapshot. Depth is still two, IDs differ.
			s.drainAgentSendQueue("a")
			if _, err := s.enqueueAgentSend("a", "new arrival"); err != nil {
				t.Fatal(err)
			}
			if mode == "missing" {
				s.reapBacklogForMissingAgent(backlogs[0], time.Now())
			} else {
				s.routeHeldReapedBacklog(backlogs[0], fleetintent.Record{}, time.Now())
			}
			entries, err := s.sendQueue().Snapshot("a")
			if err != nil || len(entries) != 1 || entries[0].Text != "new arrival" {
				t.Fatalf("stale cleanup consumed new cohort: %+v %v", entries, err)
			}
		})
	}
}

func TestT623ReapedRouteRetainsUnconfirmedAndFailedSuccessor(t *testing.T) {
	for _, mode := range []string{"unconfirmed", "failed reaped enqueue"} {
		t.Run(mode, func(t *testing.T) {
			const agent, parent = "reaped-worker", "jevons-po"
			f := t582Server(t, agent, parent, fleetlog.ReasonReapDone)
			if _, err := f.s.enqueueAgentSend(agent, "hold until the destination accepts"); err != nil {
				t.Fatal(err)
			}
			if mode == "unconfirmed" {
				f.s.SetTurnWitness(witnessYielding(TurnEvidence{}))
			} else {
				f.s.MarkAgentReaped(parent, "product:"+fleetlog.ReasonReapDone, "parent finished too")
				if err := f.s.registry.Remove(parent); err != nil {
					t.Fatal(err)
				}
				f.s.SetSenderResolver(func(string) (agentSender, bool, error) { return nil, false, nil })
				if err := os.Mkdir(filepath.Join(f.s.sendQueue().Dir(), parent+".json"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			backlogs, err := f.s.sendQueue().Backlogs()
			if err != nil {
				t.Fatal(err)
			}
			rec, _ := LookupReapedRecord(f.s.fleetIntent(), agent)
			f.s.routeHeldReapedBacklog(backlogs[0], rec, f.now)
			entries, err := f.s.sendQueue().Snapshot(agent)
			if err != nil || len(entries) != 1 || entries[0].State != sendq.Uncertain {
				t.Fatalf("failed successor erased payload: %+v %v", entries, err)
			}
		})
	}
}
