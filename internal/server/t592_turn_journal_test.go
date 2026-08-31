// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/statedb"
)

// 🎯T592. Once statedb has rows it is the store of record for turns —
// the 🎯T328 resume path reads it there. Turns must land in statedb, and
// the JSONL must NOT re-grow: it is import-once history (🎯T548.2), and
// double-writing turns back into it was the rejected first fix.
func TestT592TurnsLiveInStatedbNotJSONL(t *testing.T) {
	db, err := statedb.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	dir := t.TempDir()
	s := New("test", dir)
	s.SetStateDB(db)
	clog, err := chatlog.Open(dir + "/chat.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	s.SetChatLog(clog)
	name := s.overseerAgentName()

	s.persistChatLine(chatUserEcho("seed"))
	if n := s.statedbN(name); n == 0 {
		t.Fatal("fixture did not populate statedb; the JSONL fallback would mask the defect")
	}
	jsonlBaseline := statedb.JSONLSize(dir + "/chat.jsonl")

	s.persistChatLine(chatUserEcho("run the Cursor monthly cycle"))
	s.persistChatLine(`{"type":"assistant","message":{"content":[{"type":"text","text":"Filed 🎯T550 and ran it."}]}}`)
	s.persistChatLine(`{"type":"progress","progress_type":"tool_use","raw":{"update":{"sessionUpdate":"tool_call","title":"Bash"}}}`)

	// The turns are in the store 🎯T328 reads.
	var haveUser, haveAssistant bool
	for _, ev := range s.statedbRange(name, 1, s.statedbN(name)+1) {
		if ev.Type == "user" && strings.Contains(string(ev.Body), "Cursor monthly cycle") {
			haveUser = true
		}
		if ev.Type == "assistant" && strings.Contains(string(ev.Body), "Filed 🎯T550") {
			haveAssistant = true
		}
	}
	if !haveUser || !haveAssistant {
		t.Fatalf("turns missing from statedb (user=%v assistant=%v) — the 🎯T328 store lost them", haveUser, haveAssistant)
	}
	// And the JSONL did not re-grow: statedb is the live store, the file
	// is history.
	if grew := statedb.JSONLSize(dir+"/chat.jsonl") - jsonlBaseline; grew != 0 {
		t.Fatalf("JSONL grew by %d bytes after statedb had rows — import-once history must stay frozen (🎯T548.2)", grew)
	}
}

// 🎯T592: frames arriving with no turn behind them is the exact shape of
// the five silent days, so it is loud.
func TestT592TurnGapGoesLoudAndNamesTheLastTurn(t *testing.T) {
	g := &chatTurnGap{}
	start := time.Date(2026, 8, 26, 8, 44, 51, 0, time.UTC)
	if w := g.observe(chatUserEcho("run the Cursor monthly cycle"), start, true); w != "" {
		t.Fatalf("a durable turn must never warn: %q", w)
	}

	now := start
	var warning string
	for i := 0; i < turnGapProgressFrames*2; i++ {
		now = now.Add(2 * time.Minute)
		if w := g.observe(`{"type":"progress","progress_type":"tool_use"}`, now, true); w != "" && warning == "" {
			warning = w
		}
	}
	if warning == "" {
		t.Fatal("frames ran for hours with no turn and the daemon said nothing — the 🎯T592 defect")
	}
	if !strings.Contains(warning, start.Format(time.RFC3339)) {
		t.Fatalf("warning must name the last journaled turn, got %q", warning)
	}
	if !strings.Contains(warning, "Cursor monthly cycle") {
		t.Fatalf("warning must quote the last journaled turn, got %q", warning)
	}

	// Once per gap, not once per frame.
	for i := 0; i < turnGapProgressFrames; i++ {
		now = now.Add(time.Minute)
		if w := g.observe(`{"type":"progress"}`, now, true); w != "" {
			t.Fatalf("gap warned twice without an intervening turn: %q", w)
		}
	}
	// A durable turn re-arms it.
	now = now.Add(time.Minute)
	if w := g.observe(chatUserEcho("still there?"), now, true); w != "" {
		t.Fatalf("a durable turn must never warn: %q", w)
	}
	if ts, kind := g.lastTurn(); !ts.Equal(now) || kind != "user" {
		t.Fatalf("last turn = %v/%s, want %v/user", ts, kind, now)
	}
}

// 🎯T592 alarm fix: a turn whose append FAILED reached no store the
// resume path can read, so it must not reset the gap — before this, the
// alarm reset on attempted input and a dead store looked durable.
func TestT592FailedAppendDoesNotResetAlarm(t *testing.T) {
	g := &chatTurnGap{}
	start := time.Date(2026, 8, 26, 8, 44, 51, 0, time.UTC)
	if w := g.observe(chatUserEcho("run the Cursor monthly cycle"), start, true); w != "" {
		t.Fatalf("a durable turn must never warn: %q", w)
	}

	// The store dies: every subsequent turn append fails.
	now := start
	var warning string
	for i := 0; i < turnGapProgressFrames*2; i++ {
		now = now.Add(2 * time.Minute)
		if w := g.observe(chatUserEcho("are you there?"), now, false); w != "" && warning == "" {
			warning = w
		}
	}
	if warning == "" {
		t.Fatal("failed turn appends kept resetting the alarm — a dead store looked durable")
	}
	if !strings.Contains(warning, "Cursor monthly cycle") {
		t.Fatalf("warning must quote the last DURABLE turn, not a failed one: %q", warning)
	}

	// A durable turn afterwards re-arms it.
	now = now.Add(time.Minute)
	if w := g.observe(chatUserEcho("back again"), now, true); w != "" {
		t.Fatalf("a durable turn must never warn: %q", w)
	}
	if ts, kind := g.lastTurn(); !ts.Equal(now) || kind != "user" {
		t.Fatalf("last turn = %v/%s, want %v/user", ts, kind, now)
	}
}

// A busy hour of chat is not a gap: turns keep the alarm silent however
// many frames run between them.
func TestT592HealthyChatNeverWarns(t *testing.T) {
	g := &chatTurnGap{}
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	for exchange := 0; exchange < 20; exchange++ {
		now = now.Add(20 * time.Minute)
		if w := g.observe(chatUserEcho("question"), now, true); w != "" {
			t.Fatalf("healthy chat warned: %q", w)
		}
		for i := 0; i < turnGapProgressFrames; i++ {
			now = now.Add(time.Second)
			if w := g.observe(`{"type":"progress"}`, now, true); w != "" {
				t.Fatalf("healthy chat warned mid-exchange: %q", w)
			}
		}
		now = now.Add(time.Minute)
		if w := g.observe(`{"type":"assistant","message":{"content":[{"type":"text","text":"answer"}]}}`, now, true); w != "" {
			t.Fatalf("healthy chat warned: %q", w)
		}
	}
}

// chatTurnLine must not mistake chrome for conversation — that is what
// keeps the gap watch armed on the frames that matter.
func TestT592ChatTurnLineClassification(t *testing.T) {
	turns := []string{
		chatUserEcho("hello"),
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"user","message":{"content":"plain string content"}}`,
	}
	for _, line := range turns {
		if _, _, ok := chatTurnLine(line); !ok {
			t.Fatalf("real turn classified as chrome: %s", line)
		}
	}
	chrome := []string{
		`{"type":"progress","progress_type":"tool_use"}`,
		`{"type":"status","level":"back"}`,
		`{"type":"agent_note","text":"note"}`,
		`{"type":"system","text":"boot"}`,
		`{"type":"assistant","message":{"content":[]}}`,
		`{"type":"assistant","message":{"content":[],"stop_reason":"end_turn"}}`,
		`not json`,
	}
	for _, line := range chrome {
		if kind, _, ok := chatTurnLine(line); ok {
			t.Fatalf("chrome classified as a %s turn: %s", kind, line)
		}
	}
}
