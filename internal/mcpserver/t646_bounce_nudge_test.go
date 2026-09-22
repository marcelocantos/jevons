// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// TestT646BrokerPresentSkipBounceNudge: when the claudia daemon holds
// the seats, StartIdleNudgeLoop settle must not NotifyDaemonRestarted
// or ResumeOpenMissionWorkers (🎯T646).
func TestT646BrokerPresentSkipBounceNudge(t *testing.T) {
	prev := brokerHoldsFleet
	brokerHoldsFleet = func() bool { return true }
	t.Cleanup(func() { brokerHoldsFleet = prev })

	s, inbox := t452Fixture(t, "jevons", "sid-overseer", t452Fleet()...)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go StartIdleNudgeLoop(ctx, IdleNudgeLoopArgs{
		Server:       s,
		PostDelay:    15 * time.Millisecond,
		OverseerName: "jevons",
		DefaultPO:    "jevons-po",
		Interval:     -1,
	})
	time.Sleep(80 * time.Millisecond)
	cancel()
	got := inbox.snapshot()
	if n := bounceNudgeCount(got); n != 0 {
		t.Fatalf("broker-held bounce sent %d T171 messages: %v", n, got)
	}
}

// TestT646NoBrokerStillRunsT171: CLAUDIA_NO_BROKER / no daemon keeps
// the dual-path wave — red against an over-broad skip.
func TestT646NoBrokerStillRunsT171(t *testing.T) {
	prev := brokerHoldsFleet
	brokerHoldsFleet = func() bool { return false }
	t.Cleanup(func() { brokerHoldsFleet = prev })

	s, inbox := t452Fixture(t, "jevons", "sid-overseer", t452Fleet()...)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go StartIdleNudgeLoop(ctx, IdleNudgeLoopArgs{
		Server:       s,
		PostDelay:    15 * time.Millisecond,
		OverseerName: "jevons",
		DefaultPO:    "jevons-po",
		Interval:     -1,
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bounceNudgeCount(inbox.snapshot()) > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no-broker settle sent no T171 wave: %v", inbox.snapshot())
}

// The restart wake used to run once, at the settle delay. A Cursor
// session/new finishes later. The brief has to be waiting for that
// process, and a broker-held reclaim must still not be interrupted.
func TestRestartBriefArrivesWhenTheProcessDoes(t *testing.T) {
	prevEvery, prevFor := restartBriefRetryEvery, restartBriefRetryFor
	restartBriefRetryEvery = 20 * time.Millisecond
	restartBriefRetryFor = 2 * time.Second
	t.Cleanup(func() {
		restartBriefRetryEvery = prevEvery
		restartBriefRetryFor = prevFor
	})

	const worker = "jv-t443-red-as-proof"
	s, inbox := t452Fixture(t, "jevons", "sid-overseer", t452Fleet()...)
	def := s.registry.Def(worker)
	if def == nil {
		t.Fatal("missing worker")
	}
	next := *def
	next.TermLogPath = "-"
	next.AutoStart = true
	next.Materialized = false
	next.Provider = claudia.ProviderCursor
	if err := s.registry.Register(next); err != nil {
		t.Fatal(err)
	}
	s.registry.SetDirect(true)
	s.registry.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			return claudia.StartStub(ctx, cfg, nil)
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.retryRestartBriefs(ctx, "jevons", "", nil, map[string]bool{worker: true})
	}()

	time.Sleep(60 * time.Millisecond)
	if n := len(inbox.snapshot()[worker]); n != 0 {
		t.Fatalf("brief delivered before the process existed: %d", n)
	}
	if _, err := s.registry.Launch(worker); err != nil {
		t.Fatal(err)
	}
	if proc := s.registry.Get(worker); proc == nil || !proc.Alive() {
		t.Fatalf("launch did not leave an alive process: %#v", proc)
	}
	t.Cleanup(func() { s.registry.Stop(worker) })

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range inbox.snapshot()[worker] {
			if strings.Contains(m, "Jevons fleet standing brief") {
				cancel()
				<-done
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no full brief after the process started: %v", inbox.snapshot())
}

func bounceNudgeCount(got map[string][]string) int {
	n := 0
	for _, msgs := range got {
		for _, m := range msgs {
			if strings.Contains(m, "[event: "+eventDaemonRestarted+"]") ||
				strings.Contains(m, "[event: "+eventOwnerIntentResume+"]") {
				n++
			}
		}
	}
	return n
}
