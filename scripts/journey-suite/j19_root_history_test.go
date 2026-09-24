// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/statedb"
)

// TestT494_1_1J19SeedHasReplayEventMix fails if J19's isolate seed
// regresses to short user/assistant pairs. Daily connect replay of the
// last 30 owner turns includes agent_note / system / tool_use between
// those turns; a green J19 on text-only is a failed oracle (🎯T494.1.1).
func TestT494_1_1J19SeedHasReplayEventMix(t *testing.T) {
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
		t.Errorf("agent_note=%d — notes are the replay mix", mix.AgentNote)
	}
	if mix.System < 1 {
		t.Errorf("system=%d — system frames ride with notes on daily", mix.System)
	}
	if mix.ToolUse < 1 {
		t.Errorf("tool_use=%d — a text-only seed is the T494.1.1 miss", mix.ToolUse)
	}
	if mix.AssistantToolBlocks < 1 {
		t.Errorf("assistant tool_use blocks=%d — replay mix embeds tools in assistant frames", mix.AssistantToolBlocks)
	}
	if mix.Progress < 1 {
		t.Errorf("progress=%d — daily replay is mostly progress between owner turns", mix.Progress)
	}
	if mix.Status < 1 {
		t.Errorf("status=%d — recovery/status chrome rides the replay mix", mix.Status)
	}
	if mix.ProgressBetweenTurns < 1 {
		t.Errorf("progress between owner turns=%d", mix.ProgressBetweenTurns)
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
	// classifyJ19Seed must not report that as the replay mix.
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

// 🎯T494.1.1: the rich mix must survive the fold into the store the daemon
// serves the React pane from — a seed that folds to bubbles only is the miss
// again, one layer down.
func TestT494_1_1RichMixReachesTheStore(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "chatlog", "jevons.jsonl")
	if err := seedThroughStore(dir, "jevons", journal, func() error { return seedJ19Journal(journal, 4) }); err != nil {
		t.Fatal(err)
	}
	db, err := statedb.Open(statedb.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	evs, err := db.Range("jevons", 1, 100000)
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]int{}
	for _, e := range evs {
		types[e.Type]++
	}
	t.Logf("store rows=%d types=%v", len(evs), types)
	if types["user"] != 4 || types["assistant"] < 4 {
		t.Fatalf("bubbles missing: %v", types)
	}
	nonBubble := len(evs) - types["user"] - types["assistant"]
	if nonBubble < 4*10 {
		t.Fatalf("only %d non-bubble rows reached the store for 4 turns (want rich mix notes/tools/progress): %v", nonBubble, types)
	}
	if types["status"] < 4 {
		t.Fatalf("status rows=%d want ≥ 4: %v", types["status"], types)
	}
	if types["tool_use"] < 4 {
		t.Fatalf("tool_use rows=%d want ≥ 4 (assistant-embedded tools): %v", types["tool_use"], types)
	}
}

// 🎯T494.1.1 control wiring: the paint script carries the empty-pane control,
// and it runs before the census so the real verdict path judges it.
func TestT494_1_1J19PaintHasEmptyPaneControl(t *testing.T) {
	src, err := os.ReadFile("j19_paint.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	ctl := strings.Index(s, "J19_CONTROL === 'empty-pane'")
	census := strings.Index(s, "const sweep = await page.evaluate")
	if ctl < 0 || census < 0 || ctl > census {
		t.Fatalf("control must exist and precede the census (ctl=%d census=%d)", ctl, census)
	}
}
