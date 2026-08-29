// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
)

func regWithTree(t *testing.T) *claudia.Registry {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	// overseer -> po -> worker
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Materialized: true, Provider: "grok"},
		{Name: "po", WorkDir: dir, SessionID: "s-po", Materialized: true, Provider: "grok", Parent: "jevons"},
		{Name: "worker", WorkDir: dir, SessionID: "s-w", Materialized: true, Provider: "grok", Parent: "po"},
		{Name: "peer", WorkDir: dir, SessionID: "s-p", Materialized: true, Provider: "grok", Parent: "jevons"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

func TestAgentKillParentMayKillDescendant(t *testing.T) {
	reg := regWithTree(t)
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "worker", "actor": "po"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("kill: %s", toolText(res))
	}
	if reg.Def("worker") != nil {
		t.Fatal("worker still registered")
	}
	if reg.Def("po") == nil {
		t.Fatal("po should remain")
	}
}

func TestAgentKillPeerDenied(t *testing.T) {
	reg := regWithTree(t)
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "worker", "actor": "peer"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected peer kill denied")
	}
	if !strings.Contains(toolText(res), "denied") {
		t.Fatalf("got %q", toolText(res))
	}
	if reg.Def("worker") == nil {
		t.Fatal("worker removed on denied kill")
	}
}

func TestAgentKillChildCannotKillParent(t *testing.T) {
	reg := regWithTree(t)
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "po", "actor": "worker"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected reverse lineage denied")
	}
}

func TestAgentKillOverseerMayKillAny(t *testing.T) {
	reg := regWithTree(t)
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	// 🎯T560: descendants leave only on an explicit subtree kill.
	req.Params.Arguments = map[string]any{"name": "po", "actor": "jevons", "subtree": true}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("overseer kill: %s", toolText(res))
	}
	// Subtree: po + worker
	if reg.Def("po") != nil || reg.Def("worker") != nil {
		t.Fatal("po subtree should be gone")
	}
	if reg.Def("peer") == nil || reg.Def("jevons") == nil {
		t.Fatal("peer and overseer should remain")
	}
}

func TestAgentKillRefusesOverseer(t *testing.T) {
	reg := regWithTree(t)
	s := &Server{
		registry: reg,
		transcript: &TranscriptOps{
			GetID: func() string { return "s-o" },
		},
	}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "jevons", "actor": "po"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected refuse kill overseer")
	}
	if reg.Def("jevons") == nil {
		t.Fatal("overseer was removed")
	}
}

func TestAgentKillCascadesDescendants(t *testing.T) {
	reg := regWithTree(t)
	// grand-child under worker
	if err := reg.Register(claudia.AgentDef{
		Name: "leaf", WorkDir: t.TempDir(), SessionID: "s-l", Parent: "worker",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	// 🎯T560: cascade is the explicit subtree=true form.
	req.Params.Arguments = map[string]any{"name": "po", "actor": "jevons", "subtree": true}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatal(toolText(res))
	}
	for _, n := range []string{"po", "worker", "leaf"} {
		if reg.Def(n) != nil {
			t.Fatalf("%s still registered after cascade", n)
		}
	}
}

func TestAgentKillStopDoesNotDeregister(t *testing.T) {
	reg := regWithTree(t)
	s := &Server{registry: reg}
	stopReq := mcp.CallToolRequest{}
	stopReq.Params.Arguments = map[string]any{"name": "worker"}
	if _, err := s.handleAgentStop(context.Background(), stopReq); err != nil {
		t.Fatal(err)
	}
	if reg.Def("worker") == nil {
		t.Fatal("stop must not deregister")
	}
}

func TestCanKillRequiresActor(t *testing.T) {
	reg := regWithTree(t)
	err := canKill(reg, "", "worker", func(string) bool { return false })
	if err == nil || !strings.Contains(err.Error(), "actor") {
		t.Fatalf("want actor required, got %v", err)
	}
}

// 🎯T229: kill of an already-absent agent is success (idempotent), not
// lifecycle_error — matches T165/T195 auto-reap + hygiene double-kill.
func TestAgentKillAlreadyGoneIdempotent(t *testing.T) {
	reg := regWithTree(t)
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "never-existed", "actor": "po"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("already-gone kill must not error: %s", toolText(res))
	}
	if !strings.Contains(toolText(res), "already not registered") {
		t.Fatalf("want idempotent message, got %q", toolText(res))
	}
}

// 🎯T229: second kill after a successful kill is a no-op success.
func TestAgentKillDoubleKillIdempotent(t *testing.T) {
	reg := regWithTree(t)
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "worker", "actor": "po"}
	res1, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res1.IsError {
		t.Fatalf("first kill: %s", toolText(res1))
	}
	res2, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res2.IsError {
		t.Fatalf("second kill must be idempotent ok: %s", toolText(res2))
	}
	if !strings.Contains(toolText(res2), "already not registered") {
		t.Fatalf("want already-gone message, got %q", toolText(res2))
	}
	if reg.Def("worker") != nil {
		t.Fatal("worker still registered")
	}
}

// 🎯T229: already-gone kill still requires actor (audit trail).
func TestAgentKillAlreadyGoneRequiresActor(t *testing.T) {
	reg := regWithTree(t)
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "ghost"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected actor required on already-gone without actor")
	}
	if !strings.Contains(toolText(res), "actor") {
		t.Fatalf("got %q", toolText(res))
	}
}
