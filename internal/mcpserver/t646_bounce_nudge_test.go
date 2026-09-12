// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"
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
