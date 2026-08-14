// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T198: engagement uses explicit TargetID only — never agent-name parse.

func TestNormalizeTargetID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"  T10.2  ", "T10.2"},
		{"🎯T10.2", "T10.2"},
		{"🎯 T198", "T198"},
	}
	for _, c := range cases {
		if got := NormalizeTargetID(c.in); got != c.want {
			t.Errorf("NormalizeTargetID(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestListFleetAgentsExposesTargetID(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// Worker name deliberately does NOT contain T10.2 — engagement must
	// come from TargetID field only.
	if err := reg.Register(claudia.AgentDef{
		Name:     "jv-fixture-worker",
		WorkDir:  dir,
		SessionID: "s-w",
		Provider: "grok",
		Parent:   "jevons-po",
		Purpose:  claudia.PurposeWork,
		TargetID: "T10.2",
	}); err != nil {
		t.Fatal(err)
	}
	got := listFleetAgents(reg)
	if len(got) != 1 {
		t.Fatalf("len=%d want 1: %+v", len(got), got)
	}
	if got[0].TargetID != "T10.2" {
		t.Fatalf("target_id=%q want T10.2", got[0].TargetID)
	}
	if got[0].Name != "jv-fixture-worker" {
		t.Fatalf("name=%q", got[0].Name)
	}
}

func TestAgentsEngagedOnTargetNoNameParse(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, d := range []claudia.AgentDef{
		// Name looks like a T-id — must NOT match without TargetID.
		{Name: "T10.2-worker", WorkDir: dir, SessionID: "s1", Provider: "grok", Parent: "jevons-po"},
		// Explicit TargetID is the only discovery path.
		{Name: "jv-real", WorkDir: dir, SessionID: "s2", Provider: "grok", Parent: "jevons-po", TargetID: "T10.2"},
		// Overseer skipped even with TargetID.
		{Name: "jevons", WorkDir: dir, SessionID: "s3", Provider: "grok", Purpose: claudia.PurposeOverseer, TargetID: "T10.2"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	names := AgentsEngagedOnTarget(reg, "T10.2", dir)
	if len(names) != 1 || names[0] != "jv-real" {
		t.Fatalf("engaged=%v want [jv-real] (name-parse of T10.2-worker forbidden)", names)
	}
}

func TestStopEngagementKillsByTargetID(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, d := range []claudia.AgentDef{
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Provider: "grok", Parent: "jevons"},
		{Name: "jv-fixture-worker", WorkDir: dir, SessionID: "s-w", Provider: "grok", Parent: "jevons-po", TargetID: "T10.2"},
		{Name: "jv-other", WorkDir: dir, SessionID: "s-o", Provider: "grok", Parent: "jevons-po", TargetID: "T1"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	stopped, err := stopEngagement(reg, "T10.2", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 || stopped[0] != "jv-fixture-worker" {
		t.Fatalf("stopped=%v want [jv-fixture-worker]", stopped)
	}
	if reg.Def("jv-fixture-worker") != nil {
		t.Fatal("engaged worker still registered after stop")
	}
	if reg.Def("jv-other") == nil || reg.Def("jevons-po") == nil {
		t.Fatal("unrelated agents must remain")
	}
}

func TestHandleEngagementStopHTTP(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-fixture-worker", WorkDir: dir, SessionID: "s-w",
		Provider: "grok", Parent: "jevons-po", TargetID: "T10.2",
	}); err != nil {
		t.Fatal(err)
	}

	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// 🎯T389: the stop names the ledger it means — the frontier table is bound
	// to one repo at a time and the owner is pressing stop on that repo's row.
	body, _ := json.Marshal(map[string]string{"target_id": "T10.2", "cwd": dir})
	resp, err := http.Post(srv.URL+"/api/agents/engagement/stop", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out engagementStopResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "ok" || out.TargetID != "T10.2" {
		t.Fatalf("response %+v", out)
	}
	if len(out.Stopped) != 1 || out.Stopped[0] != "jv-fixture-worker" {
		t.Fatalf("stopped=%v", out.Stopped)
	}
	if reg.Def("jv-fixture-worker") != nil {
		t.Fatal("worker still registered")
	}

	// Also prove /api/agents exposed target_id before stop via list path.
	reg2, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "a2.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg2.Register(claudia.AgentDef{
		Name: "jv-fixture-worker", WorkDir: dir, SessionID: "s-w2",
		Provider: "grok", TargetID: "T10.2",
	}); err != nil {
		t.Fatal(err)
	}
	s2 := New("test2", t.TempDir())
	s2.SetRegistry(reg2)
	mux2 := http.NewServeMux()
	s2.RegisterRoutes(mux2)
	srv2 := httptest.NewServer(mux2)
	t.Cleanup(srv2.Close)
	resp2, err := http.Get(srv2.URL + "/api/agents")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var agents []agentInfo
	if err := json.NewDecoder(resp2.Body).Decode(&agents); err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].TargetID != "T10.2" {
		t.Fatalf("/api/agents rows=%+v", agents)
	}
}
