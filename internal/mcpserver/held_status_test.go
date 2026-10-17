// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// The October 1181ef220253 incident repeated a deliberately parked backlog
// starting at age 77h6m41s. Periodic sweeps after restart must leave that
// condition as readable durable status, without a model turn. The dormant
// recovery-summary helper is checked too; it is not a daemon startup caller.
func TestT952IntentionalHoldIsStatusAcrossRestart(t *testing.T) {
	for _, intent := range []fleetintent.State{fleetintent.Parked, fleetintent.BlockedOwner, fleetintent.BlockedProvider} {
		for _, uncertain := range []bool{false, true} {
			label := string(intent) + "/pending"
			if uncertain {
				label = string(intent) + "/uncertain"
			}
			t.Run(label, func(t *testing.T) {
				const agent = "jv-t1056-auto"
				dir := t.TempDir()
				t0 := time.Date(2026, 10, 14, 0, 33, 0, 0, time.UTC)
				s, receiver, up := t418Daemon(t, dir)
				store, err := fleetintent.Open(dir)
				if err != nil {
					t.Fatal(err)
				}
				s.SetFleetIntentStore(store)
				if err := store.SetAgent(agent, intent, "owner", "intentional hold", t0); err != nil {
					t.Fatal(err)
				}
				entry, _, err := s.sendQueue().Append(agent, "preserve this accepted obligation", t0.Add(-77*time.Hour-6*time.Minute-41*time.Second))
				if err != nil {
					t.Fatal(err)
				}
				wantState := sendq.Pending
				wantAttempt, wantDetail := "", ""
				if uncertain {
					attempt, claimed, err := s.sendQueue().ClaimFront(agent)
					if err != nil || !claimed {
						t.Fatalf("claim: %v %v", claimed, err)
					}
					if err := s.sendQueue().Resolve(agent, attempt, sendq.Unverified, "receipt not observed"); err != nil {
						t.Fatal(err)
					}
					wantState = sendq.Uncertain
					wantAttempt, wantDetail = attempt.AttemptID, "receipt not observed"
				}
				for minute := 0; minute <= 20; minute++ {
					if minute == 10 {
						// No in-memory coalescer, backoff or intent survives this restart.
						s, receiver, up = t418Daemon(t, dir)
						store, err = fleetintent.Open(dir)
						if err != nil {
							t.Fatal(err)
						}
						s.SetFleetIntentStore(store)
					}
					now := t0.Add(time.Duration(minute) * time.Minute)
					// A stale in-flight observation was part of the original
					// misdiagnosis; explicit intent still outranks it.
					s.noteTurnInFlight(agent)
					s.SetSweepClock(func() time.Time { return now })
					if minute == 0 || minute == 10 {
						s.ReportRecoveredBacklog()
					}
					s.SweepSendBacklogs()
					if got := up.all(); len(got) != 0 {
						t.Fatalf("minute %d: intentional hold prompted overseer: %v", minute, got)
					}
					if got := receiver.delivered(); len(got) != 0 {
						t.Fatalf("minute %d: held seat was prompted: %v", minute, got)
					}
					rows, err := s.sendQueue().Snapshot(agent)
					if err != nil || len(rows) != 1 || rows[0].ID != entry.ID || rows[0].State != wantState || rows[0].Text != entry.Text || rows[0].AttemptID != wantAttempt || rows[0].Detail != wantDetail {
						t.Fatalf("hold changed: %+v, %v", rows, err)
					}
					show := resultText(t, reconcile(t, s, map[string]any{"name": agent}))
					if !strings.Contains(show, entry.ID) || !strings.Contains(show, entry.Text) {
						t.Fatalf("owner read path lost held obligation: %s", show)
					}
				}
				if !uncertain {
					// A real transition out of the hold may need an alert. Silence
					// must not consume the first actionable notice's admission.
					if err := store.SetAgent(agent, fleetintent.Working, "owner", "hold lifted", t0.Add(21*time.Minute)); err != nil {
						t.Fatal(err)
					}
					s.noteTurnInFlight(agent)
					s.SweepSendBacklogs()
					if !containsLine(up.all(), "Stalled backlog") {
						t.Fatalf("first actionable stall lost: %v", up.all())
					}
				}
			})
		}
	}
}

func TestT952RecoveredSummaryOmitsIntentionalHolds(t *testing.T) {
	s, _, up := t418Daemon(t, t.TempDir())
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	for _, name := range []string{"parked-seat", "working-seat"} {
		if _, _, err := s.sendQueue().Append(name, "held", time.Now().Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetAgent("parked-seat", fleetintent.Parked, "owner", "hold", time.Now()); err != nil {
		t.Fatal(err)
	}
	s.ReportRecoveredBacklog()
	lines := up.all()
	if len(lines) != 1 || !strings.Contains(lines[0], "Recovered 1 queued message") || !strings.Contains(lines[0], "working-seat") || strings.Contains(lines[0], "parked-seat") {
		t.Fatalf("recovery summary mixes status with actionable work: %v", lines)
	}
}

func TestT952FleetHoldDefersReapedRecoveryWithoutLosingIt(t *testing.T) {
	const agent = "recoverable-seat"
	f := t582Server(t, agent, "jevons-po", fleetlog.ReasonKill)
	if _, _, err := f.s.sendQueue().Append(agent, "held recovery obligation", f.now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := f.s.intentStore().SetFleet(fleetintent.Parked, "owner", "fleet paused", f.now); err != nil {
		t.Fatal(err)
	}
	f.s.ReportRecoveredBacklog()
	f.sweepFor(20 * time.Minute)
	if got := f.up.all(); len(got) != 0 {
		t.Fatalf("fleet hold spent model turns on reaped recovery: %v", got)
	}
	if err := f.s.intentStore().SetFleet(fleetintent.Working, "owner", "resume", f.now); err != nil {
		t.Fatal(err)
	}
	f.s.SweepSendBacklogs()
	if got := heldReapedNotices(f.up.all()); got != 1 {
		t.Fatalf("fleet resume lost first recovery notice: %v", f.up.all())
	}
}

// t952UnconfirmedDaemon is a restarted process whose turn witness never
// confirms a fleet-health note. The September stall was answered by the
// model while this witness still said the receipt was unknown.
func t952UnconfirmedDaemon(t *testing.T, dir string, up *upward) *Server {
	t.Helper()
	s := &Server{}
	s.SetSendQueueDir(dir)
	s.SetRecoverBin("", dir)
	s.SetTurnWitness(func(string, string) turnWatch {
		return func() TurnEvidence { return TurnEvidence{} }
	})
	s.SetOverseerDeliver(up.deliver)
	return s
}

// The 2026-09-30 claudia-po stall advanced 10m25s through 15m55s and each
// sweep started an overseer turn. Twenty simulated minutes, a restart, and
// an unconfirmed receipt stay on the original episode: one notice, then one
// more when the age crosses the 15-minute band, and no age-varied replacement.
func TestT952UnconfirmedWorkingStallDoesNotMintAgeVariedPrompts(t *testing.T) {
	const agent = "claudia-po"
	dir := t.TempDir()
	t0 := time.Date(2026, 9, 30, 2, 6, 0, 0, time.UTC)
	up := &upward{}
	s := t952UnconfirmedDaemon(t, dir, up)
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	if err := store.SetAgent(agent, fleetintent.Working, "owner", "long turn", t0); err != nil {
		t.Fatal(err)
	}
	entry, _, err := s.sendQueue().Append(agent, "integrate the three worker reports", t0.Add(-10*time.Minute-25*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	now := t0
	for minute := 0; minute <= 20; minute++ {
		if minute == 10 {
			s = t952UnconfirmedDaemon(t, dir, up)
			store, err = fleetintent.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			s.SetFleetIntentStore(store)
		}
		now = t0.Add(time.Duration(minute) * time.Minute)
		s.noteTurnInFlight(agent)
		s.SetSweepClock(func() time.Time { return now })
		s.SetNoticeCoalesceClock(func() time.Time { return now })
		s.SetNotifyReplayClock(func() time.Time { return now })
		s.SweepSendBacklogs()
	}

	var stalled []string
	for _, line := range up.all() {
		if strings.Contains(line, "Stalled backlog") {
			stalled = append(stalled, line)
		}
	}
	if len(stalled) != 2 {
		t.Fatalf("stall notices=%d want 2 (the open and the 15m band); overseer saw %v", len(stalled), up.all())
	}
	rows, err := s.sendQueue().Snapshot(agent)
	if err != nil || len(rows) != 1 || rows[0].ID != entry.ID || rows[0].State != sendq.Pending || rows[0].Text != entry.Text {
		t.Fatalf("obligation changed: %+v %v", rows, err)
	}
}
