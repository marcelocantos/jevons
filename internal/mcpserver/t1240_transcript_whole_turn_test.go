// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// 🎯T1240: a 4K-char assistant turn reads back whole by turn number — no
// "... (truncated)" that a supervisor cannot expand.
func TestT1240TurnArgReadsWholeTurn(t *testing.T) {
	long := strings.Repeat("y", 4000)
	turns := []map[string]any{
		{"role": "user", "text": "short turn"},
		{"role": "assistant", "text": long},
	}
	s := &Server{transcript: &TranscriptOps{
		GetID: func() string { return "sess" },
		Read:  func(string) ([]map[string]any, error) { return turns, nil },
	}}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"turn": float64(2)}
	res, err := s.handleTranscriptRead(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("read turn=2: %v %s", err, toolText(res))
	}
	got := toolText(res)
	if !strings.Contains(got, long) {
		t.Fatalf("turn=2 did not come back whole:\n%s", got)
	}
	if strings.Contains(got, "(truncated)") {
		t.Fatalf("turn=2 was truncated:\n%s", got)
	}
}

// 🎯T1240: `{"name":"x"}` (wrong key — should be "agent") returns an error
// naming the valid argument, instead of being silently ignored and reading
// the overseer's own transcript.
func TestT1240UnknownArgumentIsRefused(t *testing.T) {
	s := &Server{transcript: &TranscriptOps{
		GetID: func() string { return "sess-overseer" },
		Read:  func(string) ([]map[string]any, error) { return []map[string]any{{"role": "user", "text": "overseer turn"}}, nil },
	}}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "x"}
	res, err := s.handleTranscriptRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected an error result for unknown argument, got: %s", toolText(res))
	}
	got := toolText(res)
	if !strings.Contains(got, "name") || !strings.Contains(got, "agent") {
		t.Fatalf("error does not name the bad key and the valid key:\n%s", got)
	}
	if strings.Contains(got, "overseer turn") {
		t.Fatalf("unknown argument silently fell through to the overseer transcript:\n%s", got)
	}
}

// full=true keeps every turn on the page whole instead of the normal
// 200-char preview.
func TestT1240FullArgKeepsTurnsWhole(t *testing.T) {
	long := strings.Repeat("z", 300)
	turns := []map[string]any{
		{"role": "user", "text": long},
	}
	s := &Server{transcript: &TranscriptOps{
		GetID: func() string { return "sess" },
		Read:  func(string) ([]map[string]any, error) { return turns, nil },
	}}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"full": true}
	res, err := s.handleTranscriptRead(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("read full=true: %v %s", err, toolText(res))
	}
	got := toolText(res)
	if !strings.Contains(got, long) {
		t.Fatalf("full=true truncated the turn:\n%s", got)
	}
}
