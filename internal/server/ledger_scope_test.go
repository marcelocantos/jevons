// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T389: target ids are per-ledger. Two repos each hold a live T19, and the
// fleet holds a worker on each at the same time. Every place that asks "who is
// working on T19?" must ask it of one ledger.
//
// This is the hermetic oracle for acceptance 4. It is red against the pre-fix
// tree, where AgentsEngagedOnTarget compared TargetID alone: each assertion
// below would see two agents where it must see one, and the achieve at the end
// would reap the other repo's worker mid-flight.

func twoRepoRegistry(t *testing.T, id string) (repoA, repoB string, ledgerA, ledgerB string, reg *claudia.Registry) {
	t.Helper()
	root := t.TempDir()
	body := "schema_version: 1\ntargets:\n  " + id + ":\n    name: local work\n    status: open\n"
	mk := func(name string) (string, string) {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		led := filepath.Join(dir, "bullseye.yaml")
		if err := os.WriteFile(led, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir, led
	}
	repoA, ledgerA = mk("claudia")
	repoB, ledgerB = mk("orthograph")

	var err error
	reg, err = claudia.NewRegistry(filepath.Join(root, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: repoA, SessionID: "s-o", Purpose: claudia.PurposeOverseer,
			Materialized: true, Provider: "grok"},
		{Name: "cl-t19-worker", WorkDir: repoA, SessionID: "s-a", Purpose: claudia.PurposeWork,
			Parent: "claudia-po", Materialized: true, Provider: "grok", TargetID: id},
		{Name: "og-t19-worker", WorkDir: repoB, SessionID: "s-b", Purpose: claudia.PurposeWork,
			Parent: "og-po", Materialized: true, Provider: "grok", TargetID: id},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	return repoA, repoB, ledgerA, ledgerB, reg
}

// TestT389ControlUnscopedStillCollides is the control for the oracles below:
// it asks the same fixture the pre-🎯T389 question — unscoped — and asserts it
// still gets the wrong answer. Without this, a fixture that stopped colliding
// (say, one repo's registration silently failing) would let every scoped
// assertion pass while proving nothing.
func TestT389ControlUnscopedStillCollides(t *testing.T) {
	_, _, _, _, reg := twoRepoRegistry(t, "T19")
	got := AgentsEngagedOnTarget(reg, "T19", "")
	if len(got) != 2 {
		t.Fatalf("unscoped T19=%v want both repos' workers — the fixture no longer collides, so the scoped tests below prove nothing", got)
	}
}

func TestT389EngagementOverlayShowsOnlyItsOwnLedger(t *testing.T) {
	repoA, repoB, _, _, reg := twoRepoRegistry(t, "T19")

	if got := AgentsEngagedOnTarget(reg, "T19", repoA); len(got) != 1 || got[0] != "cl-t19-worker" {
		t.Fatalf("claudia T19 engaged=%v want [cl-t19-worker]", got)
	}
	if got := AgentsEngagedOnTarget(reg, "T19", repoB); len(got) != 1 || got[0] != "og-t19-worker" {
		t.Fatalf("orthograph T19 engaged=%v want [og-t19-worker]", got)
	}

	// The RHS merges client-side on (ledger, target_id), so the feed has to
	// carry each agent's ledger — and the two repos must not share a key.
	byName := map[string]agentInfo{}
	for _, a := range listFleetAgents(reg) {
		byName[a.Name] = a
	}
	a, b := byName["cl-t19-worker"], byName["og-t19-worker"]
	if a.Ledger == "" || b.Ledger == "" {
		t.Fatalf("feed must carry ledger keys: %q / %q", a.Ledger, b.Ledger)
	}
	if a.Ledger == b.Ledger {
		t.Fatalf("two repos share a ledger key %q", a.Ledger)
	}
}

func TestT389StopEngagementKillsOnlyItsOwnRepo(t *testing.T) {
	repoA, repoB, _, _, reg := twoRepoRegistry(t, "T19")

	stopped, err := stopEngagement(reg, "T19", repoB)
	if err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 || stopped[0] != "og-t19-worker" {
		t.Fatalf("stopped=%v want [og-t19-worker]", stopped)
	}
	if reg.Def("cl-t19-worker") == nil {
		t.Fatal("stopping orthograph T19 killed claudia's T19 worker")
	}
	if reg.Def("og-t19-worker") != nil {
		t.Fatal("own repo's worker survived its own stop")
	}
	_ = repoA
}

func TestT389AchieveReapsOnlyTheAchievingLedger(t *testing.T) {
	_, _, ledgerA, _, reg := twoRepoRegistry(t, "T19")

	// claudia achieves T19. orthograph's T19 worker is mid-flight and must
	// survive: this is the destructive direction of the collision.
	achieved := "schema_version: 1\ntargets:\n  T19:\n    name: local work\n    status: achieved\n"
	if err := os.WriteFile(ledgerA, []byte(achieved), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Server{registry: reg, overseerName: "jevons"}
	s.seedAchievedFromLedger(filepath.Join(filepath.Dir(ledgerA), "bullseye.yaml"))
	// Seed observed the achieve already; re-seed from the open state so the
	// diff below is a genuine transition rather than a cold start.
	s.mu.Lock()
	s.achievedTargetsSeen = map[string]bool{}
	s.mu.Unlock()

	s.maybeReapOnLedgerAchieve(ledgerA)

	if reg.Def("cl-t19-worker") != nil {
		t.Fatal("claudia's own T19 implementer must be reaped by its achieve")
	}
	if reg.Def("og-t19-worker") == nil {
		t.Fatal("claudia's achieve reaped orthograph's T19 worker mid-flight")
	}
}

func TestT389EngagementStopHTTPScopesByRequestCwd(t *testing.T) {
	repoA, repoB, _, _, reg := twoRepoRegistry(t, "T19")

	s := &Server{registry: reg, overseerName: "jevons", frontierCwd: repoA}
	srv := httptest.NewServer(http.HandlerFunc(s.handleEngagementStop))
	defer srv.Close()

	// The table is bound to orthograph (🎯T253), so the stop says so — the
	// daemon's own frontier cwd is claudia and must not decide this.
	body, _ := json.Marshal(map[string]string{"target_id": "T19", "cwd": repoB})
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out engagementStopResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Stopped) != 1 || out.Stopped[0] != "og-t19-worker" {
		t.Fatalf("stopped=%v want [og-t19-worker]", out.Stopped)
	}
	if reg.Def("cl-t19-worker") == nil {
		t.Fatal("stop on orthograph killed claudia's T19 worker")
	}
}
