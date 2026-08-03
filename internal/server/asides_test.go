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

// 🎯T136: POST /api/asides registers purpose=aside in the fleet registry
// (register-only, no process launch) so the RHS tree can show 💡 nodes.

func TestEnsureAsideAgentRegistersPurposeAside(t *testing.T) {
	state := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Overseer root so parent defaults correctly.
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: state, SessionID: "s-o", Provider: "grok", AutoStart: true,
		Purpose: claudia.PurposeOverseer,
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", state)
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)

	out, err := s.ensureAsideAgent("att-billing", "billing nit", "")
	if err != nil {
		t.Fatalf("ensureAsideAgent: %v", err)
	}
	if !out.Created {
		t.Fatal("want created=true on first register")
	}
	if out.Name != "att-billing" || out.Purpose != claudia.PurposeAside {
		t.Fatalf("out=%+v", out)
	}
	if out.Parent != "jevons" {
		t.Fatalf("parent=%q want jevons", out.Parent)
	}
	if out.Description != "billing nit" {
		t.Fatalf("description=%q", out.Description)
	}
	if out.Status != "stopped" {
		t.Fatalf("status=%q want stopped (no launch)", out.Status)
	}

	// List feed surfaces purpose + description for RHS tree.
	agents := listFleetAgents(reg)
	var found *agentInfo
	for i := range agents {
		if agents[i].Name == "att-billing" {
			found = &agents[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("aside missing from list: %+v", agents)
	}
	if found.Purpose != claudia.PurposeAside {
		t.Fatalf("list purpose=%q", found.Purpose)
	}
	if found.Description != "billing nit" {
		t.Fatalf("list description=%q", found.Description)
	}
	if found.Parent != "jevons" {
		t.Fatalf("list parent=%q", found.Parent)
	}

	// Idempotent: same id updates description, not a second agent.
	out2, err := s.ensureAsideAgent("att-billing", "billing nit v2", "")
	if err != nil {
		t.Fatal(err)
	}
	if out2.Created {
		t.Fatal("second call must not report created")
	}
	if out2.Description != "billing nit v2" {
		t.Fatalf("updated description=%q", out2.Description)
	}
	if len(listFleetAgents(reg)) != 2 { // jevons + att-billing
		t.Fatalf("len agents=%d want 2", len(listFleetAgents(reg)))
	}
}

func TestHandleCreateAsideHTTP(t *testing.T) {
	state := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: state, SessionID: "1", Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", state)
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(map[string]string{
		"id":    "att-smoke",
		"title": "smoke test",
	})
	resp, err := http.Post(srv.URL+"/api/asides", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out createAsideResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "att-smoke" || out.Purpose != "aside" || out.Description != "smoke test" {
		t.Fatalf("out=%+v", out)
	}

	// Second POST → 200, not 201.
	body2, _ := json.Marshal(map[string]string{"id": "att-smoke", "title": "smoke test"})
	resp2, err := http.Post(srv.URL+"/api/asides", "application/json", bytes.NewReader(body2))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second status %d want 200", resp2.StatusCode)
	}
}

func TestEnsureAsideAgentRefusesOverseerName(t *testing.T) {
	state := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New("test", state)
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	if _, err := s.ensureAsideAgent("jevons", "nope", ""); err == nil {
		t.Fatal("want error when id is overseer")
	}
}
