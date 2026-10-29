// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/targetfile"
)

// 🎯T991: GET /api/agents reports the shared clone's ledger for a seat whose
// workdir is a linked worktree, not the worktree copy's path.

func t991Write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func t991FakeLinkedWorktree(t *testing.T, root string) (clone, wt, worker string) {
	t.Helper()
	clone = filepath.Join(root, "proj")
	worker = "jv-t991-worker"
	wt = filepath.Join(root, ".jevons-worktrees-proj", worker)
	admin := filepath.Join(clone, ".git", "worktrees", worker)
	t991Write(t, filepath.Join(clone, ".git", "HEAD"), "ref: refs/heads/master\n")
	t991Write(t, filepath.Join(admin, "commondir"), "../..\n")
	t991Write(t, filepath.Join(wt, ".git"), "gitdir: "+admin+"\n")
	ledger := "schema_version: 1\ntargets:\n  T991:\n    name: worktree ledger scope\n    status: identified\n"
	t991Write(t, filepath.Join(clone, "bullseye.yaml"), ledger)
	t991Write(t, filepath.Join(wt, "bullseye.yaml"), ledger)
	return clone, wt, worker
}

func TestT991WorktreeSeatReportsCloneLedgerOnAgentsAPI(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clone, wt, worker := t991FakeLinkedWorktree(t, root)
	wantLedger := filepath.Join(clone, "bullseye.yaml")

	reg, err := claudia.NewRegistry(filepath.Join(root, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: worker, WorkDir: wt, SessionID: "s1",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", TargetID: "T991",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}

	if got := targetfile.LedgerKey(wt); got != wantLedger {
		t.Fatalf("LedgerKey(worktree) = %q, want clone ledger %q", got, wantLedger)
	}
	if engaged := AgentsEngagedOnTarget(reg, "T991", clone); len(engaged) != 1 || engaged[0] != worker {
		t.Fatalf("AgentsEngagedOnTarget(T991, clone) = %v, want [%s]", engaged, worker)
	}

	byName := map[string]agentInfo{}
	for _, a := range listFleetAgents(reg) {
		byName[a.Name] = a
	}
	row := byName[worker]
	if row.Ledger != wantLedger {
		t.Fatalf("listFleetAgents ledger = %q, want %q", row.Ledger, wantLedger)
	}
	if row.Ledger == filepath.Join(wt, "bullseye.yaml") {
		t.Fatal("must not key on the worktree copy path")
	}

	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/agents")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var agents []agentInfo
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		t.Fatal(err)
	}
	var apiLedger string
	for _, a := range agents {
		if a.Name == worker {
			apiLedger = a.Ledger
			break
		}
	}
	if apiLedger != wantLedger {
		t.Fatalf("GET /api/agents ledger = %q, want %q", apiLedger, wantLedger)
	}
}
