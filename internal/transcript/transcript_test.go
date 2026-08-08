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

// Grok Build chat_history.jsonl shapes (real sessions under ~/.grok/sessions).
const grokFixture = `{"type":"system","content":"You are Grok."}
{"type":"user","content":[{"type":"text","text":"spawn a worker for tern"}]}
{"type":"reasoning","summary":[{"type":"summary_text","text":"planning…"}]}
{"type":"assistant","content":"Starting tern worker.","tool_calls":[{"id":"call-1","name":"run_terminal_command","arguments":"{}"}]}
{"type":"tool_result","tool_call_id":"call-1","content":"ok"}
{"type":"assistant","content":"Done — worker is up."}
{"type":"user","content":[{"type":"text","text":"status?"}]}
{"type":"assistant","content":"Still green."}
`

// Claude-style nested message envelopes (legacy / still present on some paths).
const claudeFixture = `{"type":"user","message":{"role":"user","content":"hello aside"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"thinking…"}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"noise"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"after tool"}]}}
{"type":"user","message":{"role":"user","content":"second prompt"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"reply two"}]}}
`

func writeSession(t *testing.T, sessionsDir, sid, body string) {
	t.Helper()
	// discovery.SessionPath walks any cwd bucket for the session id.
	dir := filepath.Join(sessionsDir, "bucket%2Fwork", sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "chat_history.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExtractTurns_GrokShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat_history.jsonl")
	if err := os.WriteFile(path, []byte(grokFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readLines(path)
	if err != nil {
		t.Fatal(err)
	}
	turns := extractTurns(lines)
	// 2 user turns → user+assistant pairs (second assistant may be multi-line within turn 1)
	if len(turns) < 4 {
		t.Fatalf("want ≥4 turn entries (2 user + assistants), got %d: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || !strings.Contains(turns[0].Text, "spawn a worker") {
		t.Fatalf("turn0 user: %+v", turns[0])
	}
	if turns[1].Role != "assistant" {
		t.Fatalf("turn1 want assistant: %+v", turns[1])
	}
	// tool_result ignored; both assistant strings for turn 1 concatenated
	if !strings.Contains(turns[1].Text, "Starting tern") || !strings.Contains(turns[1].Text, "Done") {
		t.Fatalf("assistant text missing pieces: %q", turns[1].Text)
	}
	// Second user turn
	var foundStatus bool
	for _, tr := range turns {
		if tr.Role == "user" && strings.Contains(tr.Text, "status?") {
			foundStatus = true
		}
	}
	if !foundStatus {
		t.Fatalf("second user turn missing: %+v", turns)
	}
	// Pure tool_result / reasoning must not become user turns
	for _, l := range lines {
		if l.typ == "tool_result" || l.typ == "reasoning" {
			if l.isUserTurn {
				t.Fatalf("%s must not be isUserTurn", l.typ)
			}
		}
	}
}

// 🎯T329: system-reminder / background-task user lines are not owner turn boundaries.
const t329NonBoundaryFixture = `{"type":"user","content":[{"type":"text","text":"owner ask"}]}
{"type":"assistant","content":"Part one of answer."}
{"type":"user","content":[{"type":"text","text":"<system-reminder>\\nBackground task \\"call-1\\" completed (exit code: 0).\\n</system-reminder>"}]}
{"type":"assistant","content":"Part two after inject."}
{"type":"user","content":[{"type":"text","text":"Background task call-2 completed"}]}
{"type":"assistant","content":"Part three after bare background-task user."}
{"type":"user","content":[{"type":"text","text":"second owner turn"}]}
{"type":"assistant","content":"New turn reply."}
`

func TestExtractTurns_T329NonBoundaryUserInject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat_history.jsonl")
	if err := os.WriteFile(path, []byte(t329NonBoundaryFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readLines(path)
	if err != nil {
		t.Fatal(err)
	}
	turns := extractTurns(lines)
	var users, assistants []Turn
	for _, tr := range turns {
		if tr.Role == "user" {
			users = append(users, tr)
		}
		if tr.Role == "assistant" {
			assistants = append(assistants, tr)
		}
	}
	if len(users) != 2 {
		t.Fatalf("want 2 owner user turns (injects skipped), got %d: %+v", len(users), turns)
	}
	if !strings.Contains(users[0].Text, "owner ask") {
		t.Fatalf("first user: %+v", users[0])
	}
	if !strings.Contains(users[1].Text, "second owner turn") {
		t.Fatalf("second user: %+v", users[1])
	}
	// All assistant fragments between the two owner users coalesce into one turn body.
	if len(assistants) < 1 {
		t.Fatalf("want assistant turns, got %+v", turns)
	}
	if !strings.Contains(assistants[0].Text, "Part one") ||
		!strings.Contains(assistants[0].Text, "Part two") ||
		!strings.Contains(assistants[0].Text, "Part three") {
		t.Fatalf("first assistant must join multi-tool parts across injects: %q", assistants[0].Text)
	}
	if strings.Contains(assistants[0].Text, "system-reminder") ||
		strings.Contains(assistants[0].Text, "Background task") {
		t.Fatalf("inject body must not land in assistant text: %q", assistants[0].Text)
	}
}

func TestExtractTurns_ClaudeShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat_history.jsonl")
	if err := os.WriteFile(path, []byte(claudeFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readLines(path)
	if err != nil {
		t.Fatal(err)
	}
	turns := extractTurns(lines)
	// tool_result-as-user-content is NOT a turn boundary → 2 user turns
	userCount := 0
	for _, tr := range turns {
		if tr.Role == "user" {
			userCount++
		}
	}
	if userCount != 2 {
		t.Fatalf("want 2 user turns (tool_result array excluded), got %d: %+v", userCount, turns)
	}
	if turns[0].Text != "hello aside" {
		t.Fatalf("first user text: %q", turns[0].Text)
	}
	if turns[1].Role != "assistant" || !strings.Contains(turns[1].Text, "thinking") {
		t.Fatalf("first assistant: %+v", turns[1])
	}
}

func TestReader_Read_GrokSession(t *testing.T) {
	sessions := t.TempDir()
	// Valid Grok session id shape (discovery.IsSessionID).
	sid := "019fc1c6-9c23-7261-b8e0-c7bc5967a744"
	writeSession(t, sessions, sid, grokFixture)

	r := NewReader(sessions)
	turns, err := r.Read(sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) < 4 {
		t.Fatalf("turns=%d payload=%+v", len(turns), turns)
	}
	if turns[0]["role"] != "user" {
		t.Fatalf("first role: %+v", turns[0])
	}
	text, _ := turns[0]["text"].(string)
	if !strings.Contains(text, "spawn a worker") {
		t.Fatalf("user text: %q", text)
	}
}

// 🎯T213: Claude-only projects root is enough for Reader.Read (no Grok tree).
func TestReader_Read_ClaudeSessionOnly(t *testing.T) {
	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	sid := "019fc1c6-9c23-7261-b8e0-c7bc5967a746"
	bucket := discovery.EncodeClaudeProject("/tmp/claude-work")
	dir := filepath.Join(projects, bucket)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, sid+".jsonl")
	if err := os.WriteFile(path, []byte(claudeFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewReaderRoots(discovery.Roots{ClaudeProjects: projects})
	turns, err := r.Read(sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) < 2 {
		t.Fatalf("turns=%d payload=%+v", len(turns), turns)
	}
	if turns[0]["role"] != "user" {
		t.Fatalf("first role: %+v", turns[0])
	}
	text, _ := turns[0]["text"].(string)
	if text != "hello aside" {
		t.Fatalf("user text: %q", text)
	}
}

func TestReader_Read_UnrecognizedFormatErrors(t *testing.T) {
	sessions := t.TempDir()
	sid := "019fc1c6-9c23-7261-b8e0-c7bc5967a745"
	// Lines present but no extractable user turns (old bug shape was similar).
	body := `{"type":"assistant","content":"orphan reply"}
{"type":"tool_result","tool_call_id":"x","content":"y"}
`
	writeSession(t, sessions, sid, body)
	r := NewReader(sessions)
	_, err := r.Read(sid)
	if err == nil {
		t.Fatal("expected error when file has payload but no user turns")
	}
	if !strings.Contains(err.Error(), "no user turns") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseEntries_GrokTopLevel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat_history.jsonl")
	if err := os.WriteFile(path, []byte(grokFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := parseEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	var userTurns int
	for _, e := range entries {
		if e.IsUserTurn {
			userTurns++
			if e.Text == "" {
				t.Fatalf("user turn with empty text: %+v", e)
			}
		}
		if e.Type == "assistant" && strings.Contains(e.Text, "Starting tern") {
			// ok
		}
	}
	if userTurns != 2 {
		t.Fatalf("IsUserTurn count=%d entries=%+v", userTurns, entries)
	}
}

func TestExtractAssistantText_GrokStringAndClaudeBlocks(t *testing.T) {
	grok := `{"type":"assistant","content":"plain reply","tool_calls":[]}`
	if got := extractAssistantText(grok); got != "plain reply" {
		t.Fatalf("grok string: %q", got)
	}
	claude := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"block a"},{"type":"text","text":"block b"}]}}`
	if got := extractAssistantText(claude); got != "block a\nblock b" {
		t.Fatalf("claude blocks: %q", got)
	}
	// Empty content + tool_calls only → no display text
	empty := `{"type":"assistant","content":"","tool_calls":[{"id":"1"}]}`
	if got := extractAssistantText(empty); got != "" {
		t.Fatalf("empty content want \"\", got %q", got)
	}
}
