// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T72.1: GET /api/agents is the RHS fleet panel source of truth.
// Completeness means every registered fleet agent appears — including
// PO-spawned children (AutoStart false) — not only top-level durable
// entries like jevons / jevons-po. Tree parent edges are 🎯T68.

func TestListFleetAgentsNilRegistry(t *testing.T) {
	got := listFleetAgents(nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("nil reg: got %#v, want empty non-nil slice", got)
	}
}

func TestListFleetAgentsIncludesEphemeralChildren(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// Top-level durable + PO-spawned ephemeral child (AutoStart false).
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Provider: "grok", AutoStart: true},
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Provider: "grok", AutoStart: true, Parent: "jevons"},
		{Name: "jv-ephemeral-child", WorkDir: dir, SessionID: "s-c", Provider: "grok", AutoStart: false, Parent: "jevons-po"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}

	got := listFleetAgents(reg)
	if len(got) != 3 {
		t.Fatalf("len=%d want 3: %+v", len(got), got)
	}
	// Stable name order (not Go map iteration).
	if got[0].Name != "jevons" || got[1].Name != "jevons-po" || got[2].Name != "jv-ephemeral-child" {
		t.Fatalf("order: %q, %q, %q", got[0].Name, got[1].Name, got[2].Name)
	}
	byName := map[string]agentInfo{}
	for _, a := range got {
		byName[a.Name] = a
		if a.Status != "stopped" {
			// No Launch in hermetic test — processes are absent.
			t.Fatalf("%s status=%q want stopped", a.Name, a.Status)
		}
	}
	if byName["jv-ephemeral-child"].WorkDir != dir {
		t.Fatalf("child workdir=%q", byName["jv-ephemeral-child"].WorkDir)
	}
	// 🎯T68/T72.1: parent lineage is part of the feed for tree rendering.
	if byName["jevons-po"].Parent != "jevons" {
		t.Fatalf("po parent=%q want jevons", byName["jevons-po"].Parent)
	}
	if byName["jv-ephemeral-child"].Parent != "jevons-po" {
		t.Fatalf("child parent=%q want jevons-po", byName["jv-ephemeral-child"].Parent)
	}
}

func TestHandleListAgentsHTTPCompleteness(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "1", Provider: "grok"},
		{Name: "boss", WorkDir: dir, SessionID: "2", Provider: "grok"},
		{Name: "worker", WorkDir: dir, SessionID: "3", Provider: "grok"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
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
	if len(agents) != 3 {
		t.Fatalf("got %d agents: %+v", len(agents), agents)
	}
	seen := map[string]bool{}
	for _, a := range agents {
		seen[a.Name] = true
	}
	for _, name := range []string{"jevons", "boss", "worker"} {
		if !seen[name] {
			t.Fatalf("missing %q in /api/agents: %+v", name, agents)
		}
	}
}

func TestHandleListAgentsEmptyWithoutRegistry(t *testing.T) {
	s := New("test", t.TempDir())
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/agents")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var agents []agentInfo
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		t.Fatal(err)
	}
	if len(agents) != 0 {
		t.Fatalf("want empty, got %+v", agents)
	}
}

// 🎯T118: /api/agents surfaces ACP-derived progress for fleet-row secondary.
func TestListFleetAgentsProgressFields(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, d := range []claudia.AgentDef{
		{Name: "po", WorkDir: dir, SessionID: "1", Provider: "grok"},
		{Name: "worker", WorkDir: dir, SessionID: "2", Provider: "grok", Parent: "po"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	hub := NewAgentProgressHub()
	changed := hub.Observe("worker", claudia.Event{
		Type:         "progress",
		ProgressType: "tool_use",
		Raw:          []byte(`{"update":{"sessionUpdate":"tool_call","title":"Bash: go test"}}`),
	})
	if !changed {
		t.Fatal("observe should change")
	}
	got := listFleetAgentsNotifying(reg, nil, hub, nil)
	byName := map[string]agentInfo{}
	for _, a := range got {
		byName[a.Name] = a
	}
	w := byName["worker"]
	// Process is not launched → stopped; ACP step must still surface.
	if w.Step == "" || !strings.Contains(w.Step, "Bash") {
		t.Fatalf("worker step=%q progress=%q phase=%q", w.Step, w.Progress, w.Phase)
	}
	if w.Progress == "" {
		t.Fatalf("worker progress empty: %+v", w)
	}
	// Stopped process baseline still present for po (no ACP events).
	if byName["po"].Progress == "" {
		t.Fatalf("po should have status baseline progress: %+v", byName["po"])
	}
}
