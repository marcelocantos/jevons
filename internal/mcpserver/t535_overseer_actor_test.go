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

// 🎯T535: actor=overseer (role word) resolves to the registered overseer
// seat (jevons). Hermetic registry matches acceptance; control keeps unknown
// actors refused.
func t535Registry(t *testing.T) *claudia.Registry {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Purpose: claudia.PurposeOverseer, Materialized: true, Provider: "grok"},
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Purpose: claudia.PurposeWork, Parent: "jevons", Materialized: true, Provider: "grok"},
		{Name: "worker", WorkDir: dir, SessionID: "s-w", Purpose: claudia.PurposeWork, Parent: "jevons-po", Materialized: true, Provider: "grok"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

func TestT535KillActorOverseerResolvesToSeat(t *testing.T) {
	reg := t535Registry(t)
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "worker", "actor": "overseer"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("actor=overseer kill must succeed, got: %s", toolText(res))
	}
	if reg.Def("worker") != nil {
		t.Fatal("worker still registered after actor=overseer kill")
	}
	if reg.Def("jevons") == nil || reg.Def("jevons-po") == nil {
		t.Fatal("overseer seat and PO must remain")
	}
	out := toolText(res)
	if !strings.Contains(out, `killed by "jevons"`) {
		t.Fatalf("want resolved actor jevons in result, got %q", out)
	}
	if strings.Contains(out, `killed by "overseer"`) {
		t.Fatalf("must not keep role word as actor, got %q", out)
	}
}

func TestT535KillActorJevonsStillWorks(t *testing.T) {
	reg := t535Registry(t)
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "worker", "actor": "jevons"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("actor=jevons kill: %s", toolText(res))
	}
	if reg.Def("worker") != nil {
		t.Fatal("worker still registered")
	}
}

func TestT535KillUnknownActorStillRefuses(t *testing.T) {
	reg := t535Registry(t)
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "worker", "actor": "not-a-seat"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected unknown actor refused")
	}
	got := toolText(res)
	if !strings.Contains(got, `actor "not-a-seat" is not a registered agent`) {
		t.Fatalf("want not-a-seat refuse, got %q", got)
	}
	if reg.Def("worker") == nil {
		t.Fatal("worker removed on denied kill")
	}
}

func TestT535StopActorOverseerResolvesToSeat(t *testing.T) {
	reg := t535Registry(t)
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "worker", "actor": "overseer", "reason": "t535"}
	res, err := s.handleAgentStop(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("actor=overseer stop must succeed, got: %s", toolText(res))
	}
	if reg.Def("worker") == nil {
		t.Fatal("stop must not deregister")
	}
	if got := s.resolveLifecycleActor("overseer"); got != "jevons" {
		t.Fatalf("resolveLifecycleActor(overseer)=%q want jevons", got)
	}
}

func TestT535ResolveLifecycleActor(t *testing.T) {
	reg := t535Registry(t)
	s := &Server{registry: reg}
	if got := s.resolveLifecycleActor("overseer"); got != "jevons" {
		t.Fatalf("overseer → %q want jevons", got)
	}
	if got := s.resolveLifecycleActor("Overseer"); got != "jevons" {
		t.Fatalf("Overseer → %q want jevons", got)
	}
	if got := s.resolveLifecycleActor("jevons"); got != "jevons" {
		t.Fatalf("jevons → %q want jevons", got)
	}
	if got := s.resolveLifecycleActor("not-a-seat"); got != "not-a-seat" {
		t.Fatalf("unknown must pass through, got %q", got)
	}
}
