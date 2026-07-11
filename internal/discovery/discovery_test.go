// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIsUUID(t *testing.T) {
	if !IsUUID("019f4f4b-945a-7a23-ba4c-51a0c26e0fbc") {
		t.Fatal("expected grok-style session id to pass")
	}
	if IsUUID("not-a-uuid") {
		t.Fatal("expected rejection")
	}
}

func TestScanGrokSessions(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	cwd := "/tmp/demo-repo"
	bucket := EncodeCWDBucket(cwd)
	sid := "019f4f4b-945a-7a23-ba4c-51a0c26e0fbc"
	dir := filepath.Join(sessions, bucket, sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	hist := filepath.Join(dir, "chat_history.jsonl")
	if err := os.WriteFile(hist, []byte(`{"type":"user","content":"hi"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	active := []map[string]any{{
		"session_id": sid,
		"pid":        os.Getpid(),
		"cwd":        cwd,
	}}
	data, _ := json.Marshal(active)
	if err := os.WriteFile(filepath.Join(root, "active_sessions.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewScanner(sessions)
	list, err := s.Scan(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("Scan len = %d, want 1", len(list))
	}
	if list[0].UUID != sid || list[0].WorkDir != cwd {
		t.Fatalf("got %+v", list[0])
	}
	if !list[0].Active {
		t.Fatal("expected session active (our pid)")
	}

	got, err := s.Get(sid)
	if err != nil || got == nil {
		t.Fatalf("Get: %v %+v", err, got)
	}
	if ChatHistoryPath(sessions, sid) != hist {
		t.Fatalf("ChatHistoryPath = %q", ChatHistoryPath(sessions, sid))
	}
}
