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

// 🎯T222 hermetic: play/agent_start on engaged target → no second agent.
func TestAgentStartRefusesSecondEngagement(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t221-inspect-user-md", WorkDir: dir, SessionID: "s1",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Materialized: true, Provider: "grok", TargetID: "T221",
	}); err != nil {
		t.Fatal(err)
	}

	s := New(dir, nil, nil)
	s.SetRegistry(reg)

	// Gate before Launch: refuseEngagedOrClosedTarget must fire.
	msg := s.refuseEngagedOrClosedTarget("jv-t220-dup", dir, "T221", false)
	if msg == "" || !strings.Contains(msg, "jv-t221-inspect-user-md") {
		t.Fatalf("want engagement refuse, got %q", msg)
	}
	// Same-name resume is allowed (exclude self).
	if msg := s.refuseEngagedOrClosedTarget("jv-t221-inspect-user-md", dir, "T221", false); msg != "" {
		t.Fatalf("resume same name must allow: %q", msg)
	}
	// force_engage overrides.
	if msg := s.refuseEngagedOrClosedTarget("jv-t220-dup", dir, "T221", true); msg != "" {
		t.Fatalf("force must allow: %q", msg)
	}

	// handleAgentStart path returns tool error (no Launch of second worker).
	prevStatus := loadTargetStatusForKickoff
	t.Cleanup(func() { loadTargetStatusForKickoff = prevStatus })
	loadTargetStatusForKickoff = func(cwd, id string) (string, bool) {
		return "identified", true
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":      "jv-t220-dup",
		"workdir":   dir,
		"parent":    "jevons-po",
		"target_id": "T221",
		"purpose":   "work",
	}
	res, err := s.handleAgentStart(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsError {
		t.Fatal("expected tool error for second engagement")
	}
	text := toolText(res)
	if !strings.Contains(text, "already has engaged") && !strings.Contains(text, "jv-t221-inspect-user-md") {
		t.Fatalf("error text=%q", text)
	}
	// Registry still has only the original implementer.
	if reg.Def("jv-t220-dup") != nil {
		t.Fatal("second agent must not be registered")
	}
	if reg.Def("jv-t221-inspect-user-md") == nil {
		t.Fatal("original implementer must remain")
	}
}

func TestAgentStartRefusesClosedTarget(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)

	prevStatus := loadTargetStatusForKickoff
	t.Cleanup(func() { loadTargetStatusForKickoff = prevStatus })
	loadTargetStatusForKickoff = func(cwd, id string) (string, bool) {
		if id == "T220" {
			return "set_aside", true
		}
		return "", false
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":      "jv-t220-thrash",
		"workdir":   dir,
		"parent":    "jevons-po",
		"target_id": "T220",
	}
	res, err := s.handleAgentStart(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsError {
		t.Fatal("expected refuse on set_aside")
	}
	if got := toolText(res); !strings.Contains(got, "set_aside") {
		t.Fatalf("text=%q", got)
	}
	if reg.Def("jv-t220-thrash") != nil {
		t.Fatal("must not register agent on closed target")
	}
}

func TestWorkAgentsEngagedOnTargetSkipsAsideAndSelf(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jv-worker", WorkDir: dir, SessionID: "a", Purpose: claudia.PurposeWork, TargetID: "T1", Provider: "grok"},
		{Name: "aside-1", WorkDir: dir, SessionID: "b", Purpose: claudia.PurposeAside, TargetID: "T1", Provider: "grok"},
		{Name: "other", WorkDir: dir, SessionID: "c", Purpose: claudia.PurposeWork, TargetID: "T2", Provider: "grok"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	got := workAgentsEngagedOnTarget(reg, "T1", dir, "jv-worker")
	if len(got) != 0 {
		t.Fatalf("self excluded: %v", got)
	}
	got = workAgentsEngagedOnTarget(reg, "T1", dir, "new-one")
	if len(got) != 1 || got[0] != "jv-worker" {
		t.Fatalf("got %v", got)
	}
}
