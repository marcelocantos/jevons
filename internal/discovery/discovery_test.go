// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	if list[0].Provider != "grok" {
		t.Fatalf("provider=%q want grok", list[0].Provider)
	}

	got, err := s.Get(sid)
	if err != nil || got == nil {
		t.Fatalf("Get: %v %+v", err, got)
	}
	if ChatHistoryPath(sessions, sid) != hist {
		t.Fatalf("ChatHistoryPath = %q", ChatHistoryPath(sessions, sid))
	}
}

// 🎯T213: Claude projects layout is discovered without a Grok sessions root.
func TestScanClaudeSessions(t *testing.T) {
	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	workDir := "/Users/demo/work/repo"
	bucket := EncodeClaudeProject(workDir)
	sid := "019f4f4b-945a-7a23-ba4c-51a0c26e0fbd"
	dir := filepath.Join(projects, bucket)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(dir, sid+".jsonl")
	body := `{"type":"user","message":{"role":"user","content":"claude hi"}}` + "\n"
	if err := os.WriteFile(jsonl, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sidecars must not appear as sessions.
	if err := os.WriteFile(filepath.Join(dir, "chat_history.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewScannerRoots(Roots{ClaudeProjects: projects})
	list, err := s.Scan(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("Scan len = %d, want 1 (sidecars excluded)", len(list))
	}
	if list[0].UUID != sid || list[0].Provider != "claude" {
		t.Fatalf("got %+v", list[0])
	}
	if list[0].ProjectDir != bucket {
		t.Fatalf("ProjectDir=%q want %q", list[0].ProjectDir, bucket)
	}

	got, err := s.Get(sid)
	if err != nil || got == nil {
		t.Fatalf("Get: %v %+v", err, got)
	}
	if got.Provider != "claude" {
		t.Fatalf("Get provider=%q", got.Provider)
	}
	if ClaudeJSONLPath(projects, sid) != jsonl {
		t.Fatalf("ClaudeJSONLPath = %q want %q", ClaudeJSONLPath(projects, sid), jsonl)
	}
	if p := ClaudeJSONLPathForWorkDir(projects, workDir, sid); p != jsonl {
		t.Fatalf("ClaudeJSONLPathForWorkDir = %q", p)
	}
}

// 🎯T213: TranscriptPath prefers Grok when both stores have the same id,
// and falls back to Claude when Grok is empty.
func TestTranscriptPathPreferGrokThenClaude(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	projects := filepath.Join(root, "projects")
	sid := "019f4f4b-945a-7a23-ba4c-51a0c26e0fbe"

	// Claude-only first.
	cdir := filepath.Join(projects, "bucket-a")
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatal(err)
	}
	claudePath := filepath.Join(cdir, sid+".jsonl")
	if err := os.WriteFile(claudePath, []byte(`{"type":"user","message":{"role":"user","content":"c"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Roots{GrokSessions: sessions, ClaudeProjects: projects}
	if got := TranscriptPath(r, sid); got != claudePath {
		t.Fatalf("claude-only path = %q want %q", got, claudePath)
	}

	// Add Grok — must win.
	gdir := filepath.Join(sessions, "enc%2Fcwd", sid)
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	grokPath := filepath.Join(gdir, "chat_history.jsonl")
	if err := os.WriteFile(grokPath, []byte(`{"type":"user","content":"g"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := TranscriptPath(r, sid); got != grokPath {
		t.Fatalf("prefer grok = %q want %q", got, grokPath)
	}
}

func TestEncodeClaudeProject(t *testing.T) {
	got := EncodeClaudeProject("/Users/demo/work/repo")
	if !strings.HasPrefix(got, "-Users-demo-work-repo") && got != "-Users-demo-work-repo" {
		// Claude maps every non-alnum/dash to '-'; leading slash → leading '-'.
		if got != "-Users-demo-work-repo" {
			t.Fatalf("EncodeClaudeProject = %q", got)
		}
	}
	if got != "-Users-demo-work-repo" {
		t.Fatalf("EncodeClaudeProject = %q want -Users-demo-work-repo", got)
	}
}
