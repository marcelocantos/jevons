// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/missionbound"
	"github.com/marcelocantos/jevons/internal/worktreereap"
	"github.com/mark3labs/mcp-go/mcp"
)

func t998Server(t *testing.T) (*Server, string, *fakeSender) {
	t.Helper()
	dir := t.TempDir()
	s := New(dir, nil, nil)
	reg, e := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if e != nil {
		t.Fatal(e)
	}
	s.SetRegistry(reg)
	if e = reg.Register(claudia.AgentDef{Name: "jevons-po", Purpose: "work", WorkDir: dir, SessionID: "po", Materialized: true, Provider: "grok", Parent: "jevons"}); e != nil {
		t.Fatal(e)
	}
	st, e := missionbound.Open(filepath.Join(dir, "mission-starts.json"), missionbound.Policy{MaxStarts: 1, WindowHours: 24}, nil, nil, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	s.missionStarts = st
	parent := &fakeSender{alive: true}
	setObservedSenderResolver(s, func(name string) (agentSender, bool, error) {
		if name != "jevons-po" {
			t.Fatalf("unexpected recipient %s", name)
		}
		return parent, false, nil
	})
	s.SetTurnWitness(witnessYielding(TurnEvidence{Observed: true, PayloadSeen: true}))
	settle, e := s.reserveMissionStart("original", dir, "T998", "jevons-po", "work", "jevons-po", false, "")
	if e != nil {
		t.Fatal(e)
	}
	settle(true)
	return s, dir, parent
}
func TestT998ManualForceAndAutoCannotRemintPastBound(t *testing.T) {
	s, dir, parent := t998Server(t)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "renamed", "workdir": dir, "target_id": "T998", "actor": "jevons-po", "parent": "jevons-po", "force_engage": true}
	res, err := s.handleAgentStart(context.Background(), req)
	if err != nil || res == nil || !res.IsError || !strings.Contains(toolText(res), "mission start bound") {
		t.Fatalf("manual refusal %v %v", res, err)
	}
	if err = s.spawnFrontierWorker("auto-renamed", dir, "jevons-po", "T998", "implement"); err == nil || !strings.Contains(err.Error(), "mission start bound") {
		t.Fatalf("auto refusal %v", err)
	}
	if s.registry.Def("renamed") != nil || s.registry.Def("auto-renamed") != nil {
		t.Fatal("refused start registered a new seat")
	}
	if len(parent.sent) != 1 {
		t.Fatalf("parent notice count %d: %v", len(parent.sent), parent.sent)
	}
	for _, term := range []string{"seat=renamed", "target=T998", "metric=target_starts", "rank=", "sigma="} {
		if !strings.Contains(parent.sent[0], term) {
			t.Errorf("notice missing %s", term)
		}
	}
}
func TestT998OnlyOwnerOverseerCanOverrideOneStart(t *testing.T) {
	s, dir, _ := t998Server(t)
	if _, err := s.reserveMissionStart("worker", dir, "T998", "jevons-po", "work", "jevons-po", true, "PO retry"); err == nil {
		t.Fatal("PO override accepted")
	}
	settle, err := s.reserveMissionStart("worker", dir, "T998", "jevons-po", "work", "jevons", true, "explicit recovery")
	if err != nil {
		t.Fatal(err)
	}
	settle(true)
	if _, err = s.reserveMissionStart("worker", dir, "T998", "jevons-po", "work", "jevons", false, ""); err == nil {
		t.Fatal("override remained open")
	}
	for _, name := range []string{"jevons", "jevons-po"} {
		settle, err = s.reserveMissionStart(name, dir, "T998", "jevons", "work", "jevons-po", false, "")
		if err != nil {
			t.Fatal("durable seat denied", err)
		}
		settle(true)
	}
}

func TestT998LinkedWorktreesShareTargetBudget(t *testing.T) {
	base := filepath.Join(t.TempDir(), "repo")
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", base)
	run("-C", base, "-c", "user.email=test@example.invalid", "-c", "user.name=Test", "commit", "--allow-empty", "-m", "init")
	linked := filepath.Join(t.TempDir(), "renamed-worker")
	run("-C", base, "worktree", "add", "-b", "worker", linked)
	// Record ownership so the reaper can clean up after an interrupted test.
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: linked, Note: t.Name()}); err != nil {
		t.Fatal(err)
	}
	if missionScope(base) != missionScope(linked) {
		t.Fatalf("worktree got a fresh scope: %q vs %q", missionScope(base), missionScope(linked))
	}
}
