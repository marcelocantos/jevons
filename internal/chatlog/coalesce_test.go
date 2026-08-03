// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package chatlog

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoalesceStreamLinesSealsTokenStream(t *testing.T) {
	// One user turn + many assistant token crumbs + end_turn.
	raw := []string{
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hel"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"lo"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"!"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"bye"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}`,
	}
	got := CoalesceStreamLines(raw)
	// Expect: user, sealed Hello!, user, sealed ok  → 4 frames (not 8).
	if len(got) != 4 {
		t.Fatalf("sealed count=%d want 4: %v", len(got), got)
	}
	if !strings.Contains(got[0], `"type":"user"`) {
		t.Fatalf("0: %s", got[0])
	}
	var a1 struct {
		Type    string `json:"type"`
		Message struct {
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(got[1]), &a1); err != nil {
		t.Fatal(err)
	}
	if a1.Type != "assistant" || a1.Message.StopReason != "end_turn" {
		t.Fatalf("sealed1: %+v", a1)
	}
	if len(a1.Message.Content) != 1 || a1.Message.Content[0].Text != "Hello!" {
		t.Fatalf("sealed text=%q want Hello!", a1.Message.Content[0].Text)
	}
	if !strings.Contains(got[2], "bye") {
		t.Fatalf("2: %s", got[2])
	}
	if err := json.Unmarshal([]byte(got[3]), &a1); err != nil {
		t.Fatal(err)
	}
	if a1.Message.Content[0].Text != "ok" {
		t.Fatalf("second seal %q", a1.Message.Content[0].Text)
	}
}

func TestCoalesceStreamLinesFenceEdge(t *testing.T) {
	// JSON-escaped newline in text field (as journal stores it).
	raw := []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"see:"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"\u0060\u0060\u0060go\n"}]}}`,
		`{"type":"assistant","message":{"content":[],"stop_reason":"end_turn"}}`,
	}
	// Build fence line with real backticks via Marshal for valid JSON.
	part, _ := json.Marshal(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "```go\n"}},
		},
	})
	raw[1] = string(part)
	got := CoalesceStreamLines(raw)
	if len(got) != 1 {
		t.Fatalf("n=%d %v", len(got), got)
	}
	var sealed struct {
		Message struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(got[0]), &sealed); err != nil {
		t.Fatal(err)
	}
	text := sealed.Message.Content[0].Text
	if text != "see:\n\n```go\n" {
		t.Fatalf("fence edge not repaired: %q", text)
	}
}

func TestReplayTailSealedBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "j.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	// 2 user turns; first has 50 assistant crumbs.
	if err := l.Append(`{"type":"user","message":{"content":"q0"}}`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if err := l.Append(`{"type":"assistant","message":{"content":[{"type":"text","text":"x"}]}}`); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Append(`{"type":"assistant","message":{"content":[],"stop_reason":"end_turn"}}`); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(`{"type":"user","message":{"content":"q1"}}`); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(`{"type":"assistant","message":{"content":[{"type":"text","text":"y"}]}}`); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(`{"type":"assistant","message":{"content":[],"stop_reason":"end_turn"}}`); err != nil {
		t.Fatal(err)
	}

	var rawN, sealedN int
	_, total, err := l.ReplayTail(99, func(string) error { rawN++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	start, total2, err := l.ReplayTailSealed(99, func(string) error { sealedN++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if total != total2 || start != 0 {
		t.Fatalf("start/total raw vs sealed: %d/%d vs start=%d total2=%d", total, rawN, start, total2)
	}
	// raw: 1 user + 50 crumbs + end + 1 user + crumb + end = 54
	if rawN < 50 {
		t.Fatalf("rawN=%d expected stream-like", rawN)
	}
	// sealed: user + 1 asst + user + 1 asst = 4
	if sealedN != 4 {
		t.Fatalf("sealedN=%d want 4 (bounded by turns, not tokens)", sealedN)
	}
	if sealedN >= rawN {
		t.Fatalf("sealed must be << raw: sealed=%d raw=%d", sealedN, rawN)
	}
}
