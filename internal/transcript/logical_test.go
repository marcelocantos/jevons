// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/discovery"
)

func writeJSONL(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func textsOf(turns []Turn) []string {
	var out []string
	for _, t := range turns {
		out = append(out, t.Role+":"+t.Text)
	}
	return out
}

func TestT621ClaudeCompactOmitsDiscardedTurns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess.jsonl")
	writeJSONL(t, path,
		`{"type":"user","uuid":"u1","message":{"role":"user","content":"old discarded"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"role":"assistant","content":[{"type":"text","text":"old reply"}]}}`,
		`{"type":"system","subtype":"compact_boundary","uuid":"b1","compactMetadata":{"preservedSegment":{"headUuid":"u2","anchorUuid":"a1","tailUuid":"a2"}}}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","message":{"role":"user","content":"kept after compact"}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2","message":{"role":"assistant","content":[{"type":"text","text":"kept reply"}]}}`,
	)
	got, err := ReadLogical(path)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(textsOf(got.Turns), " | ")
	if strings.Contains(joined, "old discarded") || strings.Contains(joined, "old reply") {
		t.Fatalf("pre-compact turns survived: %s", joined)
	}
	if !strings.Contains(joined, "kept after compact") || !strings.Contains(joined, "kept reply") {
		t.Fatalf("preserved turns missing: %s", joined)
	}
}

func TestT621ClaudeIncompletePreservedKeepsHistoryWithWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess.jsonl")
	writeJSONL(t, path,
		`{"type":"user","uuid":"u1","message":{"role":"user","content":"keep me, metadata broken"}}`,
		`{"type":"system","subtype":"compact_boundary","uuid":"b1","compactMetadata":{"preservedSegment":{"headUuid":"u2"}}}`,
		`{"type":"user","uuid":"u2","parentUuid":"u1","message":{"role":"user","content":"after"}}`,
	)
	got, err := ReadLogical(path)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(textsOf(got.Turns), " | ")
	if !strings.Contains(joined, "keep me, metadata broken") {
		t.Fatalf("incomplete segment must retain pre-compact: %s", joined)
	}
	if len(got.Warnings) == 0 || !strings.Contains(got.Warnings[0], "preserved_segment_unavailable") {
		t.Fatalf("warnings = %v", got.Warnings)
	}
}

func TestT621ClaudeSnipAndMetaThinkingDropped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess.jsonl")
	writeJSONL(t, path,
		`{"type":"user","uuid":"u1","message":{"role":"user","content":"visible"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"role":"assistant","content":[{"type":"thinking","text":"secret thought"}]}}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","isMeta":true,"message":{"role":"user","content":"harness inject"}}`,
		`{"type":"user","uuid":"u3","parentUuid":"u2","isSidechain":true,"message":{"role":"user","content":"subagent only"}}`,
		`{"type":"assistant","uuid":"snip","parentUuid":"u1","snipMetadata":{"removedUuids":["gone"]},"message":{"role":"assistant","content":[{"type":"text","text":"ok"}]}}`,
		`{"type":"user","uuid":"gone","parentUuid":"u1","message":{"role":"user","content":"snipped away"}}`,
	)
	got, err := ReadLogical(path)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(textsOf(got.Turns), " | ")
	for _, bad := range []string{"secret thought", "harness inject", "subagent only", "snipped away"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("dropped turn survived (%s): %s", bad, joined)
		}
	}
	if !strings.Contains(joined, "visible") {
		t.Fatalf("kept turn missing: %s", joined)
	}
}

func TestT621CodexCompactAndRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeJSONL(t, path,
		`{"type":"session_meta","payload":{"id":"c1"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"pre-compact user"}]}}`,
		`{"type":"compacted","payload":{"replacement_history":[{"type":"message","role":"user","content":[{"type":"input_text","text":"summary of old"}]}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"new work"}]}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"Bash","arguments":"rm -rf /"}}`,
		`{"type":"event_msg","payload":{"type":"thread_rolled_back","num_turns":1}}`,
	)
	got, err := ReadLogical(path)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(textsOf(got.Turns), " | ")
	if strings.Contains(joined, "pre-compact user") || strings.Contains(joined, "new work") {
		t.Fatalf("rollback/compact failed: %s", joined)
	}
	if !strings.Contains(joined, "summary of old") {
		t.Fatalf("replacement_history missing: %s", joined)
	}
	if strings.Contains(joined, "rm -rf") {
		t.Fatalf("executable tool call leaked into Distill turns: %s", joined)
	}
}

func TestT621GrokUpdatesNotChatHistory(t *testing.T) {
	sid := "019f4f4b-945a-7a23-ba4c-51a0c26e0fbf"
	sessions := filepath.Join(t.TempDir(), "sessions")
	dir := filepath.Join(sessions, discovery.EncodeCWDBucket("/work/repo"), sid)
	writeJSONL(t, filepath.Join(dir, "chat_history.jsonl"),
		`{"type":"user","content":"only the late turn survived compact"}`,
	)
	writeJSONL(t, filepath.Join(dir, "updates.jsonl"),
		`{"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"early user turn"}}}}`,
		`{"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"early assistant"}}}}`,
		`{"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"only the late turn survived compact"}}}}`,
		`{"method":"session/update","params":{"update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"thinking must not distill"}}}}`,
	)
	path := discovery.TranscriptPath(discovery.Roots{GrokSessions: sessions}, sid)
	if path == "" || filepath.Base(path) != "updates.jsonl" {
		t.Fatalf("TranscriptPath = %q, want updates.jsonl", path)
	}
	if filepath.Base(path) == "chat_history.jsonl" {
		t.Fatal("chat_history.jsonl must never be the conversation source")
	}
	got, err := ReadLogical(path)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(textsOf(got.Turns), " | ")
	if !strings.Contains(joined, "early user turn") {
		t.Fatalf("updates.jsonl lost the early turn: %s", joined)
	}
	if strings.Contains(joined, "thinking must not distill") {
		t.Fatalf("thought chunk became a Distill turn: %s", joined)
	}
}
