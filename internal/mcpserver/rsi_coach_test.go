// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/rsi"
)

func TestHandleRSICoachCycle(t *testing.T) {
	state := t.TempDir()
	logPath := filepath.Join(state, "logs", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for i := 0; i < 3; i++ {
		lines = append(lines, `{"ts":"2026-08-04T12:00:00Z","level":"error","msg":"call failed","component":"mcp","decision":"tool","fields":{"outcome":"error"},"corr":"c`+string(rune('0'+i))+`"}`)
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &capturingDeliverer{}
	coach, err := rsi.NewCoach(rsi.CoachArgs{
		StateDir:     state,
		EventLogPath: logPath,
		Deliverer:    d,
		Interval:     -1,
		SeedEOF:      false,
	})
	if err != nil {
		t.Fatal(err)
	}

	s := New(t.TempDir(), nil, nil)
	s.SetRSICoach(coach)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}
	res, err := s.handleRSICoachCycle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := rsiToolText(res)
	if !strings.Contains(text, "delivered") && !strings.Contains(text, "judgment") {
		t.Fatalf("want coach cycle summary, got %q", text)
	}
	if len(d.msgs) == 0 {
		t.Fatalf("expected deliverer called; text=%q", text)
	}
	if !strings.Contains(d.msgs[0], "Overseer: you alone decide") {
		t.Fatalf("wire not overseer-directed: %s", d.msgs[0])
	}
}

func TestHandleRSICoachConfigureAndStatus(t *testing.T) {
	state := t.TempDir()
	coach, err := rsi.NewCoach(rsi.CoachArgs{
		StateDir: state,
		Interval: -1,
		DryRun:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRSICoach(coach)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"rate_cap":       float64(1),
		"system_prompt":  "custom coach prompt for tests",
		"focus_filters":  "owner_chat,stuck",
		"updated_by":     "jevons",
	}
	res, err := s.handleRSICoachConfigure(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := rsiToolText(res)
	if !strings.Contains(text, "retuned") || !strings.Contains(text, "rate_cap=1") {
		t.Fatalf("configure: %q", text)
	}

	stReq := mcp.CallToolRequest{}
	st, err := s.handleRSICoachStatus(context.Background(), stReq)
	if err != nil {
		t.Fatal(err)
	}
	stText := rsiToolText(st)
	for _, want := range []string{"RSI coach status", "T243", "rate_cap=1", "custom coach prompt"} {
		if !strings.Contains(stText, want) {
			t.Errorf("status missing %q\n%s", want, stText)
		}
	}
}

func TestHandleRSICoachNotConfigured(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}
	res, err := s.handleRSICoachCycle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(rsiToolText(res)), "not configured") {
		t.Fatalf("got %q", rsiToolText(res))
	}
}

type capturingDeliverer struct {
	msgs []string
}

func (d *capturingDeliverer) DeliverJudgment(text string) error {
	d.msgs = append(d.msgs, text)
	return nil
}

// Ensure fake clock not needed; silence unused import if any.
var _ = time.Second

// TestHandleRSICoachCycleRetroMode covers the 🎯T353 MCP surface: mode=retro
// runs the bounded history pass, reports the window, and marks judgments.
func TestHandleRSICoachCycleRetroMode(t *testing.T) {
	state := t.TempDir()
	logPath := filepath.Join(state, "logs", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// History that predates the coach: the drip cursor is seeded at EOF, so only
	// a retrospective pass can see these rows.
	ts := time.Now().UTC().Add(-36 * time.Hour).Format(time.RFC3339)
	var lines []string
	for i := 0; i < 3; i++ {
		lines = append(lines,
			`{"ts":"`+ts+`","level":"error","msg":"call failed","component":"mcp","decision":"tool","fields":{"outcome":"error"},"corr":"h`+string(rune('0'+i))+`"}`)
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &capturingDeliverer{}
	coach, err := rsi.NewCoach(rsi.CoachArgs{
		StateDir:     state,
		EventLogPath: logPath,
		Deliverer:    d,
		Interval:     -1,
		SeedEOF:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRSICoach(coach)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"mode": "retro"}
	res, err := s.handleRSICoachCycle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := rsiToolText(res)
	if !strings.Contains(text, "mode=retro") || !strings.Contains(text, "retro window:") {
		t.Fatalf("want retro summary with window, got %q", text)
	}
	if len(d.msgs) == 0 {
		t.Fatalf("retro pass should deliver a history judgment; text=%q", text)
	}
	if !strings.Contains(d.msgs[0], "Mode: retrospective") {
		t.Fatalf("judgment not marked retrospective: %s", d.msgs[0])
	}

	// Status reports the retro dials and the last pass.
	statusRes, err := s.handleRSICoachStatus(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	status := rsiToolText(statusRes)
	if !strings.Contains(status, "retro(🎯T353)") || !strings.Contains(status, "lookback_hours=168") {
		t.Fatalf("status missing retro dials: %q", status)
	}

	// Unknown mode is refused, not silently treated as drip.
	bad := mcp.CallToolRequest{}
	bad.Params.Arguments = map[string]any{"mode": "sideways"}
	badRes, err := s.handleRSICoachCycle(context.Background(), bad)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rsiToolText(badRes), "unknown mode") {
		t.Fatalf("want unknown-mode error, got %q", rsiToolText(badRes))
	}
}
