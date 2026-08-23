// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/poproactive"
)

// 🎯T406 hermetic oracle.
//
// Acceptance 6: fixture provider responses — a spend-limit refusal enters
// blocked_provider and suppresses spawn/nudge/revive; an ordinary transient
// error does not; a successful call clears it. Red against the pre-fix tree
// (no ObserveProviderFailure → never enters) and against an over-broad mutant
// that treats any failure as a hard block.

type t406Notifier struct {
	mu   sync.Mutex
	kind string
	text string
	n    int
}

func (n *t406Notifier) NotifyOwner(subject, kind, text string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.kind = kind
	n.text = text
	n.n++
	return true
}

func t406Server(t *testing.T) (*Server, *t406Notifier) {
	t.Helper()
	dir := t.TempDir()
	store, err := fleetintent.Open(filepath.Join(dir, "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	s.SetFleetIntentStore(store)
	n := &t406Notifier{}
	s.SetOwnerNotifier(n)
	return s, n
}

func t406Spawned(fleet fleetintent.State) bool {
	spawned := false
	SweepFrontierConsume(FrontierConsumeArgs{
		Leaves:       []poproactive.LeafObs{{ID: "T406", Name: "ready leaf"}},
		Now:          time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		PORegistered: true,
		FleetIntent:  fleet,
		Spawn: func(poproactive.LeafObs, string) error {
			spawned = true
			return nil
		},
	})
	return spawned
}

func TestT406SpendLimitEntersHardBlockAndSuppressesSpawn(t *testing.T) {
	s, n := t406Server(t)
	raw := "You've hit your monthly spend limit. Run /usage-credits to manage your limit"
	class := agenterr.ClassifyText(raw)
	if !agenterr.HardBlock(class, raw) {
		t.Fatalf("fixture must hard-block; class=%s", class)
	}
	s.ObserveProviderFailure(class, raw)

	if got := s.fleetIntent().FleetState(); got != fleetintent.BlockedProvider {
		t.Fatalf("fleet state=%q want blocked_provider", got)
	}
	if t406Spawned(s.fleetIntent().FleetState()) {
		t.Fatal("frontier-consume must not spawn under blocked_provider")
	}
	if poproactive.ShouldKeepKickingUnderIntent(
		[]poproactive.LeafObs{{ID: "T406", Name: "ready"}},
		s.fleetIntent().FleetState(),
	) {
		t.Fatal("PO proactive must not kick under blocked_provider")
	}
	dec := fleetintent.AllowsFleet(s.fleetIntent().FleetState(), fleetintent.ControlRevive)
	if dec.Allow {
		t.Fatal("revive must decline under blocked_provider (restart must not resurrect into the wall)")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.n < 1 || n.kind != hardBlockKind || !strings.Contains(n.text, "hard-block") {
		t.Fatalf("owner notice missing: n=%d kind=%q text=%q", n.n, n.kind, n.text)
	}
}

func TestT406TransientErrorDoesNotEnterHardBlock(t *testing.T) {
	s, n := t406Server(t)
	for _, raw := range []string{
		"Internal error",
		"rate limit exceeded",
		"HTTP 429 Too Many Requests",
		"connection refused",
	} {
		class := agenterr.ClassifyText(raw)
		s.ObserveProviderFailure(class, raw)
		if got := s.fleetIntent().FleetState(); got != fleetintent.Working {
			t.Fatalf("transient %q entered %q", raw, got)
		}
	}
	if t406Spawned(fleetintent.Working) != true {
		t.Fatal("working fleet must still spawn")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.n != 0 {
		t.Fatalf("transient must not notify owner; n=%d", n.n)
	}
}

func TestT406SuccessfulCallClearsHardBlock(t *testing.T) {
	s, _ := t406Server(t)
	raw := "You've hit your monthly spend limit"
	s.ObserveProviderFailure(agenterr.ClassifyText(raw), raw)
	if s.fleetIntent().FleetState() != fleetintent.BlockedProvider {
		t.Fatal("setup: want blocked_provider")
	}
	s.ObserveProviderOK()
	if got := s.fleetIntent().FleetState(); got != fleetintent.Working {
		t.Fatalf("after OK fleet=%q want working", got)
	}
	if !t406Spawned(s.fleetIntent().FleetState()) {
		t.Fatal("after clear, spawn must resume")
	}
}

func TestT406HardBlockSurvivesStoreReopen(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "fleet")
	store, err := fleetintent.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	s.SetFleetIntentStore(store)
	raw := "You've hit your monthly spend limit"
	s.ObserveProviderFailure(agenterr.ClassifyText(raw), raw)

	reopened, err := fleetintent.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Snapshot().FleetState(); got != fleetintent.BlockedProvider {
		t.Fatalf("after reopen fleet=%q want blocked_provider", got)
	}
}

func TestT406FleetIntentHTTP(t *testing.T) {
	s, _ := t406Server(t)
	raw := "You've hit your monthly spend limit"
	s.ObserveProviderFailure(agenterr.ClassifyText(raw), raw)

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/fleet-intent", nil))
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["fleet_state"] != string(fleetintent.BlockedProvider) {
		t.Fatalf("body=%v", body)
	}
	if body["hard_block"] != true {
		t.Fatalf("hard_block=%v", body["hard_block"])
	}
}

// Over-broadness mutant: treating every failure class as a hard block would
// stand the fleet down on Internal error. The product path must not.
func TestT406OverBroadAnyErrorMutantFails(t *testing.T) {
	s, _ := t406Server(t)
	mutantObserve := func(class agenterr.Class, raw string) {
		if class.IsFailure() {
			_ = s.SetFleetIntent(fleetintent.BlockedProvider, "mutant", "any failure")
		}
	}
	mutantObserve(agenterr.ClassifyText("Internal error"), "Internal error")
	if s.fleetIntent().FleetState() != fleetintent.BlockedProvider {
		t.Fatal("mutant setup")
	}
	// Product path on the same fixture must leave working.
	s2, _ := t406Server(t)
	s2.ObserveProviderFailure(agenterr.ClassifyText("Internal error"), "Internal error")
	if s2.fleetIntent().FleetState() != fleetintent.Working {
		t.Fatal("product path must not treat Internal error as a hard block")
	}
}

// Pre-fix shape: without ObserveProviderFailure, a spend-limit refusal
// leaves the fleet working and spawn keeps burning into the wall.
func TestT406PreFixTreeWouldKeepSpawning(t *testing.T) {
	s, _ := t406Server(t)
	raw := "You've hit your monthly spend limit"
	class := agenterr.ClassifyText(raw)
	if !agenterr.HardBlock(class, raw) {
		t.Fatal("fixture")
	}
	// Deliberately skip ObserveProviderFailure — the pre-fix world.
	if s.fleetIntent().FleetState() != fleetintent.Working {
		t.Fatal("pre-fix default is working")
	}
	if !t406Spawned(s.fleetIntent().FleetState()) {
		t.Fatal("pre-fix tree keeps spawning into the wall — oracle must stay red without the observe call")
	}
}
