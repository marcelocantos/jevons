// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestT494_1_1J19SeedHasDailyReplayEventMix fails if J19's isolate seed
// regresses to short user/assistant pairs. Daily connect replay of the
// last 30 owner turns includes agent_note / system / tool_use between
// those turns; a green J19 on text-only is a failed oracle (🎯T494.1.1).
func TestT494_1_1J19SeedHasDailyReplayEventMix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jevons.jsonl")
	if err := seedJ19Journal(path, 4); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mix := classifyJ19Seed(body)
	if mix.User != 4 {
		t.Errorf("user=%d want 4", mix.User)
	}
	if mix.Assistant < 4 {
		t.Errorf("assistant=%d want ≥4", mix.Assistant)
	}
	if mix.AgentNote < 1 {
		t.Errorf("agent_note=%d — notes are the daily mix", mix.AgentNote)
	}
	if mix.System < 1 {
		t.Errorf("system=%d — system frames ride with notes on daily", mix.System)
	}
	if mix.ToolUse < 1 {
		t.Errorf("tool_use=%d — a text-only seed is the T494.1.1 miss", mix.ToolUse)
	}
	if mix.NotesBetweenTurns < 1 {
		t.Errorf("notes between owner turns=%d (trailing-only is the empty-tail cousin, not the 65-slot desert)", mix.NotesBetweenTurns)
	}
	if mix.ToolsBetweenTurns < 1 {
		t.Errorf("tools between owner turns=%d", mix.ToolsBetweenTurns)
	}

	src, err := os.ReadFile("j19_root_history.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `"type": "tool_use"`) &&
		!strings.Contains(string(src), `"type":"tool_use"`) {
		t.Error("seed source must write tool_use content blocks, not only mention the class")
	}
}

func TestT494_1_1TextOnlySeedIsTheMiss(t *testing.T) {
	// Mutation: user+assistant pairs with no notes/tools between them.
	// classifyJ19Seed must not report that as the daily mix.
	body := []byte(strings.Join([]string{
		`{"type":"user","message":{"content":"ROOThist-00"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"ack"}]}}`,
		`{"type":"user","message":{"content":"ROOThist-01"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"ack"}]}}`,
	}, "\n"))
	mix := classifyJ19Seed(body)
	if mix.ToolUse != 0 || mix.NotesBetweenTurns != 0 || mix.ToolsBetweenTurns != 0 {
		t.Fatalf("text-only seed must classify as empty mix, got %+v", mix)
	}
	if mix.User != 2 || mix.Assistant != 2 {
		t.Fatalf("text-only user/assistant counts: %+v", mix)
	}
}
