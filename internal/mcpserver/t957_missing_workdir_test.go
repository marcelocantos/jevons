// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"
)

// 🎯T957 hermetic: jevons_agent_start refuses a workdir that does not exist
// on disk, before any registry row or broker launch — instead of launching a
// seat into a missing cwd (the worktree.Ensure failure branch used to keep
// the given workdir rather than refusing).
func TestAgentStartRefusesMissingWorkdir(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)

	missing := filepath.Join(dir, "does-not-exist", "jv-t957-ghost")

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "jv-t957-ghost",
		"workdir": missing,
		"parent":  "jevons-po",
		"purpose": "work",
	}
	res, err := s.handleAgentStart(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsError {
		t.Fatal("expected tool error for nonexistent workdir")
	}
	text := toolText(res)
	if !strings.Contains(text, missing) {
		t.Fatalf("error text=%q, want it to name the missing path %q", text, missing)
	}
	if !strings.Contains(text, "shared clone") {
		t.Fatalf("error text=%q, want it to say to pass the shared clone", text)
	}
	// No registry row, no broker launch: the name must not be registered.
	if reg.Def("jv-t957-ghost") != nil {
		t.Fatal("agent must not be registered when workdir is missing")
	}
}

// A workdir that DOES exist still reaches the normal isolation path (not
// refused by the T957 check itself). We only assert the check doesn't fire
// for an existing directory; full isolation behaviour is covered elsewhere.
func TestAgentStartAllowsExistingWorkdir(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	// The assertion is only about the workdir check; never launch a seat.
	s.launchAgentFn = func(context.Context, string) (*claudia.Agent, error) {
		return nil, errors.New("test: no launch")
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "jv-t957-real",
		"workdir": dir,
		"parent":  "jevons-po",
		"purpose": "work",
	}
	res, err := s.handleAgentStart(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res != nil && res.IsError {
		text := toolText(res)
		if strings.Contains(text, "does not exist on disk") {
			t.Fatalf("existing workdir wrongly refused as missing: %q", text)
		}
	}
}
