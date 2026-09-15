// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package handover_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/handover"
)

func TestT621DistillReadsGrokUpdatesNotChatHistory(t *testing.T) {
	sid := "019f4f4b-945a-7a23-ba4c-51a0c26e0fbf"
	sessions := filepath.Join(t.TempDir(), "sessions")
	dir := filepath.Join(sessions, discovery.EncodeCWDBucket("/work/repo"), sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chat_history.jsonl"), []byte(
		`{"type":"user","content":"only the late turn survived compact"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "updates.jsonl"), []byte(strings.Join([]string{
		`{"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"early user turn"}}}}`,
		`{"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"early assistant"}}}}`,
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), []byte(
		`{"generated_title":"early work","info":{"id":"`+sid+`","cwd":"/work/repo"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	path := discovery.TranscriptPath(discovery.Roots{GrokSessions: sessions}, sid)
	if filepath.Base(path) != "updates.jsonl" {
		t.Fatalf("TranscriptPath = %q", path)
	}
	got := handover.Distill(path)
	if !strings.Contains(got, "early user turn") {
		t.Fatalf("Distill missed updates.jsonl history:\n%s", got)
	}
	if strings.Contains(got, filepath.Dir(path)) || strings.Contains(got, "updates.jsonl") && strings.Contains(got, "Read it") {
		t.Fatalf("Distill cited the predecessor path:\n%s", got)
	}
	seed := handover.ComposeSeed(handover.Pending{
		From: "grok", To: "claude", Kind: handover.KindMigrate, TranscriptPath: path, Brief: got,
	})
	if strings.Contains(seed, path) || strings.Contains(seed, "Read it before") {
		t.Fatalf("ComposeSeed walked the predecessor file:\n%s", seed)
	}
}
