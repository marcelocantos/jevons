// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// T36.1 / Fable F2: when spawnHalted (or any budget guard) refuses, the
// primary MCP spawn tools must return a tool error and never reach the
// underlying manager/registry/claude launch path.

func haltedSpawnServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{}
	s.SetBudgetGuards(
		func() error { return errors.New("spawning halted by budget clamp-down") },
		func(id string, auto bool) error { return errors.New("resume blocked: budget clamp-down") },
	)
	return s
}

func TestJworkBlockedWhenSpawnHalted(t *testing.T) {
	s := haltedSpawnServer(t)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"text": "burn money"}
	result, err := s.handleJwork(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected tool error when spawn halted")
	}
	if got := toolText(result); !strings.Contains(got, "spawning halted") {
		t.Fatalf("error text = %q, want spawn-halt message", got)
	}
}

func TestCreateSessionBlockedWhenSpawnHalted(t *testing.T) {
	s := haltedSpawnServer(t)
	// mgr is nil — if the guard is bypassed this panics. Guard must fire first.
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "w", "workdir": "/tmp"}
	result, err := s.handleCreateSession(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected tool error when spawn halted")
	}
	if got := toolText(result); !strings.Contains(got, "spawning halted") {
		t.Fatalf("error text = %q, want spawn-halt message", got)
	}
}

func TestAgentStartBlockedWhenSpawnHalted(t *testing.T) {
	s := haltedSpawnServer(t)
	// registry is nil — panic if guard skipped.
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "rogue",
		"workdir": "/tmp/work",
	}
	result, err := s.handleAgentStart(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected tool error when spawn halted")
	}
	if got := toolText(result); !strings.Contains(got, "spawning halted") {
		t.Fatalf("error text = %q, want spawn-halt message", got)
	}
}

func TestAgentStartBlockedWhenResumeGuardOnly(t *testing.T) {
	s := &Server{}
	s.SetBudgetGuards(
		func() error { return nil }, // spawn allowed
		func(id string, auto bool) error {
			return errors.New("resume \"" + id + "\" blocked: budget clamp-down in force")
		},
	)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "paused-agent",
		"workdir": "/tmp/work",
	}
	result, err := s.handleAgentStart(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected tool error when resume guard refuses")
	}
	if got := toolText(result); !strings.Contains(got, "paused-agent") {
		t.Fatalf("error text = %q, want agent id", got)
	}
}

func toolText(r *mcp.CallToolResult) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	if tc, ok := r.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}
