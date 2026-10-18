// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/sendq"
)

type t7664Cause struct {
	cause string
	text  string
}

func (e *t7664Cause) Error() string         { return e.text }
func (e *t7664Cause) ProviderCause() string { return e.cause }

type queueAttemptSender struct{ send func(string) error }

func (p queueAttemptSender) Send(text string) error { return p.send(text) }
func (queueAttemptSender) Alive() bool              { return true }
func (queueAttemptSender) Interrupt() error         { return nil }

func TestT623DrainDiesAfterSubmit(t *testing.T) {
	const helper = "JEVONS_T623_CRASH_QUEUE"
	if dir := os.Getenv(helper); dir != "" {
		s, _, _ := t418Daemon(t, dir)
		setObservedSenderResolver(s, func(string) (agentSender, bool, error) {
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
		{"known busy refusal", fmt.Errorf("grok acp: %w", claudia.ErrTurnInFlight), true},
		{"process gone before write", &t7664Cause{cause: agenterr.CauseProcessNotRunning, text: "claude process not running"}, true},
		{"process gone rendered text", errors.New("claude process not running"), false},
		{"EOF after write", io.EOF, false},
		{"closed awaiting response", errors.New("codex app-server: closed waiting for turn/start"), false},
		{"unrecognized provider error", errors.New("provider failed"), false},
		{"quoted busy text", errors.New("provider response quoted: grok acp: prompt already in flight"), false},
		{"rendered broker refusal", errors.New("broker protocol: agent_failed: cursor acp: prompt already in flight"), false},
		{"unauthorized prose", errors.New("401 unauthorized"), false},
		// A broker refusal and the same cause wrapped again classify the same.
		// The rendered sentence is not the cause.
		{"broker refusal cause", &t7664Cause{cause: agenterr.CauseTurnInFlight, text: "broker protocol: agent_failed: cursor acp: prompt already in flight"}, true},
		{"wrapped broker refusal cause", fmt.Errorf("send: %w", &t7664Cause{cause: agenterr.CauseTurnInFlight, text: "broker protocol: agent_failed: grok acp: prompt already in flight"}), true},
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
			setObservedSenderResolver(s, func(string) (agentSender, bool, error) {
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
			// After a restart exactly one message is submitted. For a
			// definitely-not-sent refusal it is the first request, retried.
			// For an uncertain outcome it is the LATER request: 🎯T766.5
			// lets the queue flow past an entry whose delivery nobody can
			// establish, instead of freezing every message behind it until a
			// human reconciles — which held jevons-po's 104 messages for
			// seventeen hours. The uncertain entry itself is never resent,
			// and it stays in the queue, held, for reconciliation.
			after, receiver, _ := t418Daemon(t, dir)
			after.drainAgentSendQueue("a")
			got := receiver.delivered()
			wantText := "later request"
			if tc.retry {
				wantText = "first accepted request"
			}
			if len(got) != 1 || !strings.Contains(got[0], wantText) {
				t.Fatalf("restart submissions=%q, want exactly one carrying %q (retry=%v)", got, wantText, tc.retry)
			}
			if !tc.retry {
				held, err := after.sendQueue().Snapshot("a")
				if err != nil || len(held) == 0 || held[0].ID != first.ID || held[0].State != sendq.Uncertain {
					t.Fatalf("uncertain entry was not kept held after the queue flowed past it: %+v %v", held, err)
				}
				for _, text := range got {
					if strings.Contains(text, "first accepted request") {
						t.Fatal("uncertain entry was resent")
					}
				}
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
			setObservedSenderResolver(s, func(string) (agentSender, bool, error) {
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
				// 🎯T766.5: the head's attempt was orphaned by the dead
				// daemon, so it is held and never resent; the tail behind it
				// was never sent, so delivering it is not a duplicate.
				after, receiver, _ := t418Daemon(t, dir)
				after.drainAgentSendQueue("a")
				got := receiver.delivered()
				if len(got) != 1 || !strings.Contains(got[0], "tail") {
					t.Fatalf("after restart delivered %q, want only the never-sent tail", got)
				}
				left, _ := after.sendQueue().Snapshot("a")
				if len(left) != 2 || left[0].Text != "head" || left[0].State != sendq.Attempting || left[1].Text != "tail" || left[1].State != sendq.Uncertain || !strings.Contains(left[1].Detail, "no receipt") {
					t.Fatalf("orphaned attempt not kept held: %+v", left)
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
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) {
		return queueAttemptSender{send: func(text string) error {
			got = append(got, text)
			s.noteTurnEnded("a")
			s.drainAgentSendQueue("a") // terminal's drain races the retained claim
			return nil
		}}, false, nil
	})
	s.drainAgentSendQueue("a")
	// The nested drain races the outer claim, so only the first handoff
	// happens inside that call. It stays held: the sender has no receipt.
	if len(got) != 1 || got[0] != "first" || s.flightState("a") != FlightIdle {
		t.Fatalf("in-flight claim blocked the racing drain: sent=%v flight=%v", got, s.flightState("a"))
	}
	mid, err := s.sendQueue().Snapshot("a")
	if err != nil || len(mid) != 2 || mid[0].Text != "first" || mid[0].State != sendq.Uncertain || mid[1].Text != "second" || mid[1].State != sendq.Pending {
		t.Fatalf("after the racing drain: %+v %v", mid, err)
	}
	s.drainAgentSendQueue("a")
	if len(got) != 2 || got[1] != "second" || observedQueueDepth(s, "a") != 2 || observedPendingSends(s, "a") != 0 {
		t.Fatalf("pending behind a held handoff was not offered: sent=%v pending=%d depth=%d", got, observedPendingSends(s, "a"), observedQueueDepth(s, "a"))
	}
	left, _ := s.sendQueue().Snapshot("a")
	if len(left) != 2 || left[0].State != sendq.Uncertain || left[1].State != sendq.Uncertain {
		t.Fatalf("both handoffs = %+v", left)
	}
	s.drainAgentSendQueue("a")
	if len(got) != 2 {
		t.Fatalf("held handoffs resent: %v", got)
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
	res, err := deliverObservedToSender(s, "a", "must be durably held", false, &fakeSender{alive: true, inFlight: true}, false)
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
	if len(receiver.delivered()) != 1 {
		t.Fatal("durability change froze the existing healthy stream path")
	}
	left, err := s.sendQueue().Snapshot("a")
	if err != nil || len(left) != 1 || left[0].State != sendq.Uncertain || !strings.Contains(left[0].Detail, "no receipt") {
		t.Fatalf("healthy stream without a receipt must stay held: %+v %v", left, err)
	}
	s.drainAgentSendQueue("a")
	if len(receiver.delivered()) != 1 {
		t.Fatal("held handoff was offered again")
	}
}

func TestT623ConcurrentDrainAndAppendPreserveFIFO(t *testing.T) {
	s, _, up := t418Daemon(t, t.TempDir())
	if _, err := s.enqueueAgentSend("a", "first"); err != nil {
		t.Fatal(err)
	}
	entered, proceed, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	calls := 0
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) {
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
	if err != nil || calls != 1 || len(entries) != 2 || entries[0].Text != "first" || entries[0].State != sendq.Uncertain || entries[1].Text != "second" || entries[1].State != sendq.Pending {
		t.Fatalf("concurrent drain duplicated or lost an arrival: calls=%d entries=%+v err=%v", calls, entries, err)
	}
}

func TestT623BusyRefusalsRemainOrdinaryWaiting(t *testing.T) {
	s, _, _ := t418Daemon(t, t.TempDir())
	if _, err := s.enqueueAgentSend("a", "waiting for turn end"); err != nil {
		t.Fatal(err)
	}
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) {
		return queueAttemptSender{send: func(string) error { return fmt.Errorf("grok acp: %w", claudia.ErrTurnInFlight) }}, false, nil
	})
	for range SendqPinFailureThreshold + 1 {
		s.drainAgentSendQueue("a")
	}
	if pin, ok := s.sendqPinFor("a"); ok {
		t.Fatalf("ordinary busy seat was pinned: %+v", pin)
	}
}

func TestT623BrokerWrappedBusyStaysPendingForRetry(t *testing.T) {
	s, _, _ := t418Daemon(t, t.TempDir())
	if _, err := s.enqueueAgentSend("a", "monitor objective"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) {
		return queueAttemptSender{send: func(string) error {
			calls++
			return &t7664Cause{cause: agenterr.CauseTurnInFlight, text: "broker protocol: agent_failed: grok acp: prompt already in flight"}
		}}, false, nil
	})
	// PayloadQueued used to confirm this refusal. A transcript is not a receipt.
	s.SetTurnWitness(witnessYielding(TurnEvidence{
		Observed:      true,
		PayloadQueued: true,
		Detail:        "transcript shows payload enqueued behind live turn",
	}))
	s.drainAgentSendQueue("a")
	entries, err := s.sendQueue().Snapshot("a")
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Pending || entries[0].AttemptID != "" || !strings.Contains(entries[0].Detail, "grok acp: prompt already in flight") {
		t.Fatalf("busy refusal must stay pending for retry: %+v %v", entries, err)
	}
	if pin, ok := s.sendqPinFor("a"); ok {
		t.Fatalf("busy refusal pinned the seat: %+v", pin)
	}
	s.drainAgentSendQueue("a")
	if calls != 2 {
		t.Fatalf("calls=%d want 2; a definite non-send must be offered again", calls)
	}
}

func TestT623ReapedRoutingKeepsConcurrentArrivals(t *testing.T) {
	const agent, parent = "reaped-worker", "jevons-po"
	f := t582Server(t, agent, parent, fleetlog.ReasonReapDone)
	if _, err := f.s.enqueueAgentSend(agent, "old held message"); err != nil {
		t.Fatal(err)
	}
	setObservedSenderResolver(f.s, func(name string) (agentSender, bool, error) {
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
	if err != nil || len(entries) != 2 || entries[0].Text != "old held message" || entries[0].State != sendq.Uncertain || !strings.Contains(entries[0].Detail, "without a receipt") || entries[1].Text != "arrived during routing" || entries[1].State != sendq.Pending {
		t.Fatalf("routing lost a message: %+v %v", entries, err)
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
			if err != nil || len(entries) != 2 || entries[0].Text != "first" || entries[0].State != sendq.Uncertain || entries[1].Text != "new arrival" || entries[1].State != sendq.Pending {
				t.Fatalf("stale cleanup touched the retained handoff or the new cohort: %+v %v", entries, err)
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
				setObservedSenderResolver(f.s, func(string) (agentSender, bool, error) { return nil, false, nil })
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

// 🎯T766.5: after a restart, the backlog sweep is the only thing that can
// move an idle seat's queue — the turn boundary it was waiting for was
// consumed by the bounce. A held entry at the head must not stop the sweep
// from re-offering the pending messages behind it.
func TestT766SweepReoffersPendingBehindHeldEntryAfterRestart(t *testing.T) {
	dir := t.TempDir()
	s, _, _ := t418Daemon(t, dir)
	first, _, err := s.sendQueue().Append("a", "ambiguous delivery", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.sendQueue().Append("a", "waiting behind it", time.Now()); err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := s.sendQueue().ClaimFront("a")
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := s.sendQueue().Resolve("a", attempt, sendq.Unverified, "no session event within 45s"); err != nil {
		t.Fatal(err)
	}

	after, receiver, _ := t418Daemon(t, dir)
	after.ReportRecoveredBacklog()
	after.SweepSendBacklogs()

	got := receiver.delivered()
	if len(got) != 1 || !strings.Contains(got[0], "waiting behind it") {
		t.Fatalf("sweep delivered %q, want exactly the pending message behind the held one", got)
	}
	left, err := after.sendQueue().Snapshot("a")
	if err != nil || len(left) != 2 || left[0].ID != first.ID || left[0].State != sendq.Uncertain || !strings.Contains(left[0].Detail, "no session event") || left[1].Text != "waiting behind it" || left[1].State != sendq.Uncertain || !strings.Contains(left[1].Detail, "no receipt") {
		t.Fatalf("held entry not kept after the sweep flowed past it: %+v %v", left, err)
	}
	after.SweepSendBacklogs()
	if again := receiver.delivered(); len(again) != 1 {
		t.Fatalf("sweep resent %q", again)
	}
}
