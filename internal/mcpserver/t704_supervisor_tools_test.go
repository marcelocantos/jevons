// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/supervisormem"
)

// 🎯T704 MCP half: notes read/append and escalate without seat filesystem or Bash.
func TestT704SupervisorToolsNotesAndEscalate(t *testing.T) {
	state := t.TempDir()
	s := New(t.TempDir(), nil, nil)
	s.stateDir = state

	block := "### 2026-10-22 07:31\nT704 MCP append."
	appendReq := mcp.CallToolRequest{}
	appendReq.Params.Arguments = map[string]any{
		"action": "append_rolling",
		"block":  block,
	}
	appendRes, err := s.handleSupervisorNotes(context.Background(), appendReq)
	if err != nil || appendRes == nil || appendRes.IsError {
		t.Fatalf("append: err=%v res=%+v", err, appendRes)
	}

	readReq := mcp.CallToolRequest{}
	readReq.Params.Arguments = map[string]any{"action": "read"}
	readRes, err := s.handleSupervisorNotes(context.Background(), readReq)
	if err != nil || readRes == nil || readRes.IsError {
		t.Fatalf("read: err=%v res=%+v", err, readRes)
	}
	text := resultText(t, readRes)
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("decode read json: %v", err)
	}
	if !strings.Contains(payload.Content, "T704 MCP append.") {
		t.Fatalf("read missing block in %q", payload.Content)
	}
	if !strings.Contains(payload.Content, "## ROLLING") {
		t.Fatalf("missing ROLLING section: %q", payload.Content)
	}

	old := supervisormem.DefaultBlurterSend
	supervisormem.DefaultBlurterSend = func(subject, body, severity, key string) (string, error) {
		if subject == "" || body == "" {
			t.Fatal("blurter missing subject/body")
		}
		if severity == "" {
			t.Fatal("expected default severity")
		}
		return "/tmp/spool", nil
	}
	defer func() { supervisormem.DefaultBlurterSend = old }()

	escReq := mcp.CallToolRequest{}
	escReq.Params.Arguments = map[string]any{
		"subject": "T704",
		"body":    "hermetic escalate",
	}
	escRes, err := s.handleSupervisorEscalate(context.Background(), escReq)
	if err != nil || escRes == nil || escRes.IsError {
		t.Fatalf("escalate: err=%v res=%+v", err, escRes)
	}
	if !strings.Contains(resultText(t, escRes), "spooled") {
		t.Fatalf("escalate text=%q", resultText(t, escRes))
	}
}
