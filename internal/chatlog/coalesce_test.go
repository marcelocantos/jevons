// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package chatlog

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T245: pure silent sealed empty + agent_note + visible → only visible body.
// Mirrors owner screenshot: two ACP turns with only agent_note between must
// not leave silent text for display coalesce to glue onto the next reply.
func TestCoalesceStreamLinesSilentThenAgentNoteThenVisible(t *testing.T) {
	raw := []string{
		`{"type":"assistant","stream_id":"35728c40","message":{"role":"assistant","content":[{"type":"text","text":"[silent] PO already re-pressured jv-t244; no further action."}]}}`,
		`{"type":"assistant","stream_id":"35728c40","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}`,
		`{"type":"agent_note","text":"[Agent jevons-po responded] Independent gate…"}`,
		`{"type":"assistant","stream_id":"9cb609c1","message":{"role":"assistant","content":[{"type":"text","text":"**🎯T244 landed.**\n\nIndependent check."}]}}`,
		`{"type":"assistant","stream_id":"9cb609c1","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}`,
	}
	got := CoalesceStreamLines(raw)
	// empty silent sealed + agent_note + visible sealed → 3 lines
	if len(got) != 3 {
		t.Fatalf("count=%d want 3: %v", len(got), got)
	}
	if strings.Contains(got[0], "re-pressured") || strings.Contains(got[0], "[silent]") {
		t.Fatalf("silent body leaked: %s", got[0])
	}
	var silent struct {
		Message struct {
			Content []any `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(got[0]), &silent); err != nil {
		t.Fatal(err)
	}
	if len(silent.Message.Content) != 0 {
		t.Fatalf("silent content must be empty: %v", silent.Message.Content)
	}
	if !strings.Contains(got[1], "agent_note") && !strings.Contains(got[1], "Independent gate") {
		// agent_note passes through as-is
		if !strings.Contains(got[1], "Agent jevons-po") {
			t.Fatalf("agent_note missing: %s", got[1])
		}
	}
	if !strings.Contains(got[2], "T244 landed") {
		t.Fatalf("visible body missing: %s", got[2])
	}
	if strings.Contains(got[2], "[silent]") || strings.Contains(got[2], "re-pressured") {
		t.Fatalf("silent glued into visible: %s", got[2])
	}
}

// 🎯T240: multi-fragment [silent] stream seals to empty body (history strip).
func TestCoalesceStreamLinesStripsSilentSealed(t *testing.T) {
	raw := []string{
		`{"type":"user","message":{"role":"user","content":"ping"}}`,
		`{"type":"assistant","stream_id":"s9","message":{"role":"assistant","content":[{"type":"text","text":"[silent]"}]}}`,
		`{"type":"assistant","stream_id":"s9","message":{"role":"assistant","content":[{"type":"text","text":" continued"}]}}`,
		`{"type":"assistant","stream_id":"s9","message":{"role":"assistant","content":[{"type":"text","text":" jv-t240"}]}}`,
		`{"type":"assistant","stream_id":"s9","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}`,
		`{"type":"assistant","stream_id":"s10","message":{"role":"assistant","content":[{"type":"text","text":"Owner needs this."}]}}`,
		`{"type":"assistant","stream_id":"s10","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}`,
	}
	got := CoalesceStreamLines(raw)
	if len(got) != 3 {
		t.Fatalf("count=%d want 3 (user + empty silent + owner): %v", len(got), got)
	}
	// Silent sealed: no body text, no "continued".
	if strings.Contains(got[1], "continued") || strings.Contains(got[1], "[silent]") {
		t.Fatalf("silent body leaked: %s", got[1])
	}
	var silent struct {
		Message struct {
			Content    []any  `json:"content"`
			StopReason string `json:"stop_reason"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(got[1]), &silent); err != nil {
		t.Fatal(err)
	}
	if len(silent.Message.Content) != 0 {
		t.Fatalf("silent content must be empty: %v", silent.Message.Content)
	}
	if silent.Message.StopReason != "end_turn" {
		t.Fatalf("stop=%q", silent.Message.StopReason)
	}
	if !strings.Contains(got[2], "Owner needs this") {
		t.Fatalf("non-silent unchanged: %s", got[2])
	}
}

// 🎯T242: multi-fragment visible stream + empty end_turn → sealed body non-empty.
// Regression: silent strip / empty terminal must not wipe non-silent stream_id bodies.
func TestCoalesceStreamLinesVisibleMultiFragmentEmptyEndTurnKeepsBody(t *testing.T) {
	raw := []string{
		`{"type":"user","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","stream_id":"s42","message":{"role":"assistant","content":[{"type":"text","text":"Hel"}]}}`,
		`{"type":"assistant","stream_id":"s42","message":{"role":"assistant","content":[{"type":"text","text":"lo owner"}]}}`,
		`{"type":"assistant","stream_id":"s42","message":{"role":"assistant","content":[{"type":"text","text":" — stays."}]}}`,
		// Empty terminal (Grok ACP) — must not clear accumulated body.
		`{"type":"assistant","stream_id":"s42","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}`,
	}
	got := CoalesceStreamLines(raw)
	if len(got) != 2 {
		t.Fatalf("count=%d want 2 (user + sealed asst): %v", len(got), got)
	}
	var sealed struct {
		StreamID string `json:"stream_id"`
		Message  struct {
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(got[1]), &sealed); err != nil {
		t.Fatal(err)
	}
	if sealed.StreamID != "s42" {
		t.Fatalf("stream_id=%q", sealed.StreamID)
	}
	if sealed.Message.StopReason != "end_turn" {
		t.Fatalf("stop=%q", sealed.Message.StopReason)
	}
	if len(sealed.Message.Content) != 1 || sealed.Message.Content[0].Text != "Hello owner — stays." {
		t.Fatalf("sealed body wiped or wrong: %+v (raw=%s)", sealed.Message.Content, got[1])
	}
}

// 🎯T242: reused wire stream_id after terminal (daemon restart s1,s2,…) must not
// merge a prior [silent] turn into a later visible turn and strip the body.
func TestCoalesceStreamLinesStreamIDReuseAfterTerminalDoesNotWipeVisible(t *testing.T) {
	// Same stream_id "s2" for two logical turns (restart re-minted s2).
	raw := []string{
		`{"type":"user","message":{"role":"user","content":"ops"}}`,
		`{"type":"assistant","stream_id":"s2","message":{"role":"assistant","content":[{"type":"text","text":"[silent] continued workers"}]}}`,
		`{"type":"assistant","stream_id":"s2","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}`,
		`{"type":"user","message":{"role":"user","content":"what about the bug?"}}`,
		`{"type":"assistant","stream_id":"s2","message":{"role":"assistant","content":[{"type":"text","text":"Visible reply"}]}}`,
		`{"type":"assistant","stream_id":"s2","message":{"role":"assistant","content":[{"type":"text","text":" must stay."}]}}`,
		`{"type":"assistant","stream_id":"s2","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}`,
	}
	got := CoalesceStreamLines(raw)
	// user, empty silent, user, visible sealed  → 4
	if len(got) != 4 {
		t.Fatalf("count=%d want 4: %v", len(got), got)
	}
	if !strings.Contains(got[0], "ops") {
		t.Fatalf("0: %s", got[0])
	}
	// Silent turn stripped.
	if strings.Contains(got[1], "continued") || strings.Contains(got[1], "[silent]") {
		t.Fatalf("silent leaked: %s", got[1])
	}
	var silent struct {
		Message struct {
			Content []any `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(got[1]), &silent); err != nil {
		t.Fatal(err)
	}
	if len(silent.Message.Content) != 0 {
		t.Fatalf("silent content: %v", silent.Message.Content)
	}
	if !strings.Contains(got[2], "what about the bug") {
		t.Fatalf("2: %s", got[2])
	}
	var visible struct {
		StreamID string `json:"stream_id"`
		Message  struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(got[3]), &visible); err != nil {
		t.Fatal(err)
	}
	if visible.StreamID != "s2" {
		t.Fatalf("visible stream_id=%q", visible.StreamID)
	}
	if len(visible.Message.Content) != 1 || visible.Message.Content[0].Text != "Visible reply must stay." {
		t.Fatalf("visible body lost to silent false-merge: %+v line=%s", visible.Message.Content, got[3])
	}
}

// 🎯T242 acceptance: N owner turns with assistant text → N assistant bodies after seal.
func TestCoalesceStreamLinesNOwnerTurnsNAssistantBodies(t *testing.T) {
	raw := []string{
		`{"type":"user","message":{"role":"user","content":"q1"}}`,
		`{"type":"assistant","stream_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":"A1"}]}}`,
		`{"type":"assistant","stream_id":"s1","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}`,
		`{"type":"user","message":{"role":"user","content":"q2"}}`,
		// Reused stream_id after terminal (restart).
		`{"type":"assistant","stream_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":"A2"}]}}`,
		`{"type":"assistant","stream_id":"s1","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}`,
		`{"type":"user","message":{"role":"user","content":"q3"}}`,
		`{"type":"assistant","stream_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":"A3"}]}}`,
		`{"type":"assistant","stream_id":"s1","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}`,
	}
	got := CoalesceStreamLines(raw)
	if len(got) != 6 {
		t.Fatalf("count=%d want 6: %v", len(got), got)
	}
	wantBodies := []string{"A1", "A2", "A3"}
	bi := 0
	for _, ln := range got {
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(ln), &probe); err != nil {
			t.Fatal(err)
		}
		if probe.Type != "assistant" {
			continue
		}
		var m struct {
			Message struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatal(err)
		}
		if bi >= len(wantBodies) {
			t.Fatalf("extra assistant: %s", ln)
		}
		if len(m.Message.Content) != 1 || m.Message.Content[0].Text != wantBodies[bi] {
			t.Fatalf("asst[%d] body=%v want %q line=%s", bi, m.Message.Content, wantBodies[bi], ln)
		}
		bi++
	}
	if bi != 3 {
		t.Fatalf("assistant bodies=%d want 3", bi)
	}
}

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

func TestCoalesceStreamLinesContinuousFenceBareConcat(t *testing.T) {
	// 🎯T161: continuous token frames bare-concat (no ``` content sniff).
	// Display fence hygiene remains T145 ensureFenceNewlines.
	part, _ := json.Marshal(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "```go\n"}},
		},
	})
	raw := []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"see:"}]}}`,
		string(part),
		`{"type":"assistant","message":{"content":[],"stop_reason":"end_turn"}}`,
	}
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
	if text != "see:```go\n" {
		t.Fatalf("continuous stream should bare-concat, got %q", text)
	}
}

func TestCoalesceStreamLinesToolGapStructuralJoin(t *testing.T) {
	// tool_use between text segments → structural blank line (no content sniff).
	raw := []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"First paragraph ends here."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"read_file","input":{}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Second paragraph starts here."}]}}`,
		`{"type":"assistant","message":{"content":[],"stop_reason":"end_turn"}}`,
	}
	got := CoalesceStreamLines(raw)
	if len(got) != 1 {
		t.Fatalf("n=%d want 1 sealed bubble: %v", len(got), got)
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
	want := "First paragraph ends here.\n\nSecond paragraph starts here."
	if sealed.Message.Content[0].Text != want {
		t.Fatalf("got %q want %q", sealed.Message.Content[0].Text, want)
	}
}

// 🎯T223: user mid-assistant must not split one response into two seals.
// Fixture mirrors the durable journal failure: stream tokens → user insert
// → more tokens → end_turn. Hard-reload uses CoalesceStreamLines.
func TestCoalesceStreamLinesUserMidStreamOneSeal(t *testing.T) {
	raw := []string{
		`{"type":"user","message":{"role":"user","content":"what about filing?"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"First half ends mid"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"-sentence at diagram."}]}}`,
		// Premature owner follow-up journaled while stream still open.
		`{"type":"user","message":{"role":"user","content":"observe bullseye"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":" Second half continues the list."}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}`,
	}
	got := CoalesceStreamLines(raw)
	// user + sealed full asst + mid user  → 3 frames (not 4 with split asst).
	if len(got) != 3 {
		t.Fatalf("sealed count=%d want 3: %v", len(got), got)
	}
	if !strings.Contains(got[0], "what about filing") {
		t.Fatalf("0 want opening user: %s", got[0])
	}
	var sealed struct {
		Type     string `json:"type"`
		StreamID string `json:"stream_id"`
		Message  struct {
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(got[1]), &sealed); err != nil {
		t.Fatal(err)
	}
	if sealed.Type != "assistant" || sealed.Message.StopReason != "end_turn" {
		t.Fatalf("sealed1: %+v", sealed)
	}
	text := sealed.Message.Content[0].Text
	want := "First half ends mid-sentence at diagram. Second half continues the list."
	if text != want {
		t.Fatalf("joined text=%q want %q", text, want)
	}
	if !strings.Contains(got[2], "observe bullseye") {
		t.Fatalf("2 want mid user after sealed body: %s", got[2])
	}
}

// 🎯T223: explicit stream_id join across interleave; two ids stay two blocks.
func TestCoalesceStreamLinesByStreamID(t *testing.T) {
	raw := []string{
		`{"type":"assistant","stream_id":"s-a","message":{"role":"assistant","content":[{"type":"text","text":"A1"}]}}`,
		`{"type":"assistant","stream_id":"s-b","message":{"role":"assistant","content":[{"type":"text","text":"B1"}]}}`,
		`{"type":"user","message":{"role":"user","content":"noise"}}`,
		`{"type":"assistant","stream_id":"s-a","message":{"role":"assistant","content":[{"type":"text","text":"A2"}]}}`,
		`{"type":"assistant","stream_id":"s-b","message":{"role":"assistant","content":[{"type":"text","text":"B2"}]}}`,
		`{"type":"assistant","stream_id":"s-a","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}`,
		`{"type":"assistant","stream_id":"s-b","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}`,
	}
	got := CoalesceStreamLines(raw)
	// sealed A, sealed B, user  — order follows first fragment of each stream + user pass-through.
	if len(got) != 3 {
		t.Fatalf("n=%d want 3: %v", len(got), got)
	}
	var a, b struct {
		StreamID string `json:"stream_id"`
		Message  struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(got[0]), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(got[1]), &b); err != nil {
		t.Fatal(err)
	}
	if a.StreamID != "s-a" || a.Message.Content[0].Text != "A1A2" {
		t.Fatalf("A: id=%q text=%q", a.StreamID, a.Message.Content[0].Text)
	}
	if b.StreamID != "s-b" || b.Message.Content[0].Text != "B1B2" {
		t.Fatalf("B: id=%q text=%q", b.StreamID, b.Message.Content[0].Text)
	}
	if !strings.Contains(got[2], "noise") {
		t.Fatalf("user pass-through: %s", got[2])
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
