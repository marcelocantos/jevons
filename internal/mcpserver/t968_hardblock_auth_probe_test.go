// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T968: plan login recovery while blocked on auth triggers one provider probe
// that can clear the hard-block without an owner/supervisor send.

func t968AuthBlock(t *testing.T) (*Server, *t406Notifier, agenterr.Class, string) {
	s, n := t406Server(t)
	raw := `provider refused the turn: 401 {"type":"error","error":{"type":"authentication_error","message":"OAuth access token has been revoked"}}`
	class := agenterr.Classify(agenterr.WithCause(agenterr.CauseAuth, errors.New(raw)))
	if !agenterr.HardBlock(class, raw) {
		t.Fatalf("fixture must hard-block; class=%s", class)
	}
	s.ObserveProviderFailure(class, raw)
	if s.fleetIntent().FleetState() != fleetintent.BlockedProvider {
		t.Fatal("setup: want blocked_provider")
	}
	return s, n, class, raw
}

func TestT968PlanLoginRecoveryProbesAndClearsAuthHardBlock(t *testing.T) {
	s, _, _, _ := t968AuthBlock(t)

	var probed sync.WaitGroup
	probed.Add(1)
	s.mu.Lock()
	s.authHardBlockProbeFn = func(name string) {
		if name != "claudia-po" {
			t.Errorf("probe seat=%q want claudia-po (first running recovered seat)", name)
		}
		s.ObserveProviderOK()
		probed.Done()
	}
	s.mu.Unlock()

	s.ClearPlanAuthFailures([]string{"jevons-po", "claudia-po"})
	done := make(chan struct{})
	go func() {
		probed.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("auth hard-block probe did not run")
	}
	if got := s.fleetIntent().FleetState(); got != fleetintent.Working {
		t.Fatalf("fleet=%q want working after successful probe", got)
	}
}

func TestT968PlanLoginRecoveryProbeStill401LeavesBlockWithoutExtraNotice(t *testing.T) {
	s, n, class, raw := t968AuthBlock(t)
	n.mu.Lock()
	notified := n.n
	n.mu.Unlock()

	var probed sync.WaitGroup
	probed.Add(1)
	s.mu.Lock()
	s.authHardBlockProbeFn = func(string) {
		s.ObserveProviderFailure(class, raw)
		probed.Done()
	}
	s.mu.Unlock()

	s.ClearPlanAuthFailures([]string{"jevons-po"})
	done := make(chan struct{})
	go func() {
		probed.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("auth hard-block probe did not run")
	}
	if got := s.fleetIntent().FleetState(); got != fleetintent.BlockedProvider {
		t.Fatalf("fleet=%q want still blocked_provider", got)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.n != notified {
		t.Fatalf("failed probe re-notified owner: n=%d want %d (🎯T906)", n.n, notified)
	}
}

func TestT968SpendWallBlockDoesNotProbeOnPlanRecovery(t *testing.T) {
	s, _ := t406Server(t)
	raw := "You've hit your monthly spend limit"
	s.ObserveProviderFailure(agenterr.Classify(agenterr.WithCause(agenterr.CauseSpendWall, errors.New(raw))), raw)
	probes := 0
	s.mu.Lock()
	s.authHardBlockProbeFn = func(string) { probes++ }
	s.mu.Unlock()
	s.ClearPlanAuthFailures([]string{"jevons-po"})
	if probes != 0 {
		t.Fatalf("spend-wall block probed on plan recovery: probes=%d", probes)
	}
}
