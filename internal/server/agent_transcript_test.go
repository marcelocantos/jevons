// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/transcript"
)

// slogCapture records Info-level records for 🎯T128.2 empty_reason assertions.
type slogCapture struct {
	records []slog.Record
}

func (h *slogCapture) Enabled(context.Context, slog.Level) bool { return true }
func (h *slogCapture) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *slogCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *slogCapture) WithGroup(string) slog.Handler      { return h }

func attrsMap(r slog.Record) map[string]any {
	m := map[string]any{}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})
	return m
}

func findEmptyReason(cap *slogCapture, want string) map[string]any {
	for _, rec := range cap.records {
		if rec.Message != "agent_transcript empty" {
			continue
		}
		got := attrsMap(rec)
		if got["empty_reason"] == want {
			return got
		}
	}
	return nil
}

func TestHandleAgentTranscriptNotFound(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New("test", dir)
	s.SetRegistry(reg)
	s.SetTranscriptReader(transcript.NewReader(filepath.Join(dir, "sessions")))

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/missing/transcript", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", rr.Code)
	}
}

func TestHandleAgentTranscriptEmptySession(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name:      "worker-x",
		WorkDir:   dir,
		SessionID: "019fc2aa-bbbb-7ccc-8ddd-eeeeeeeeeeee",
		Purpose:   claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", dir)
	s.SetRegistry(reg)
	s.SetTranscriptReader(transcript.NewReader(filepath.Join(dir, "sessions")))

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/worker-x/transcript", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["name"] != "worker-x" {
		t.Fatalf("name=%v", payload["name"])
	}
	// No on-disk transcript → empty turns (usable pane, not 500).
	if empty, _ := payload["empty"].(bool); !empty {
		// turns length 0 is also fine
		turns, _ := payload["turns"].([]any)
		if len(turns) != 0 {
			t.Fatalf("expected empty turns, got %+v", payload)
		}
	}
	if payload["empty_reason"] != emptyReasonReadError {
		t.Fatalf("empty_reason=%v want %s", payload["empty_reason"], emptyReasonReadError)
	}
}

// TestHandleAgentTranscriptEmptyReasons covers 🎯T128.2: no_session, read_error,
// and zero_turns each produce structured slog empty_reason (and journal when open).
func TestHandleAgentTranscriptEmptyReasons(t *testing.T) {
	dir := t.TempDir()
	sessRoot := filepath.Join(dir, "sessions")
	// Seed agents.json including empty session_id (Register requires non-empty;
	// load path still allows no_session for pre-mint / partial registry rows).
	sidMissing := "019fc2aa-bbbb-7ccc-8ddd-eeeeeeeeeeee"
	sidEmpty := "019fc2aa-bbbb-7ccc-8ddd-ffffffffffff"
	agentsJSON := `[
  {"name":"no-sess","workdir":` + jsonString(dir) + `,"session_id":"","purpose":"work"},
  {"name":"read-err","workdir":` + jsonString(dir) + `,"session_id":"` + sidMissing + `","purpose":"work"},
  {"name":"zero-turns","workdir":` + jsonString(dir) + `,"session_id":"` + sidEmpty + `","purpose":"aside"}
]`
	if err := os.WriteFile(filepath.Join(dir, "agents.json"), []byte(agentsJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}

	chatDir := filepath.Join(sessRoot, "fake%2Fwork", sidEmpty)
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// progress-only lines are not user turns and are not "payload" → calm zero turns.
	meta := `{"type":"progress","content":"booting"}` + "\n"
	if err := os.WriteFile(filepath.Join(chatDir, "chat_history.jsonl"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	j, err := eventlog.Open(eventlog.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })

	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := New("test", dir)
	s.SetRegistry(reg)
	s.SetTranscriptReader(transcript.NewReader(sessRoot))
	s.SetEventLog(j)

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	type caseSpec struct {
		path   string
		reason string
		name   string
	}
	cases := []caseSpec{
		{"/api/agents/no-sess/transcript", emptyReasonNoSession, "no-sess"},
		{"/api/agents/read-err/transcript", emptyReasonReadError, "read-err"},
		{"/api/agents/zero-turns/transcript", emptyReasonZeroTurns, "zero-turns"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status=%d body=%s", tc.reason, rr.Code, rr.Body.String())
		}
		var payload map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("%s: decode: %v", tc.reason, err)
		}
		if empty, _ := payload["empty"].(bool); !empty {
			t.Fatalf("%s: want empty payload, got %+v", tc.reason, payload)
		}
		if payload["empty_reason"] != tc.reason {
			t.Fatalf("%s: body empty_reason=%v", tc.reason, payload["empty_reason"])
		}
		got := findEmptyReason(cap, tc.reason)
		if got == nil {
			t.Fatalf("%s: no slog record with empty_reason (records=%d)", tc.reason, len(cap.records))
		}
		if got["component"] != "agent_transcript" {
			t.Fatalf("%s: component=%v", tc.reason, got["component"])
		}
		if got["name"] != tc.name {
			t.Fatalf("%s: name=%v want %s", tc.reason, got["name"], tc.name)
		}
		if tc.reason == emptyReasonReadError {
			if got["err"] == nil || got["err"] == "" {
				t.Fatalf("read_error should include err attr, got %v", got)
			}
		}
	}

	// Journal dual-write: all three empty_reason values greppable via fields.
	events, err := eventlog.Tail(j.Path(), eventlog.TailOptions{
		Limit:     20,
		Component: "agent_transcript",
		Source:    "server",
	})
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, ev := range events {
		if ev.Fields == nil {
			continue
		}
		if r, ok := ev.Fields["empty_reason"].(string); ok {
			found[r] = true
		}
	}
	for _, want := range []string{emptyReasonNoSession, emptyReasonReadError, emptyReasonZeroTurns} {
		if !found[want] {
			t.Fatalf("journal missing empty_reason=%s (found=%v events=%d)", want, found, len(events))
		}
	}
}

func TestHandleAgentTranscriptWithFixture(t *testing.T) {
	dir := t.TempDir()
	sessRoot := filepath.Join(dir, "sessions")
	sid := "019fc2aa-bbbb-7ccc-8ddd-ffffffffffff"
	// discovery.ChatHistoryPath layout: sessions/<encoded-cwd>/<sid>/chat_history.jsonl
	// Use a fake tree the finder accepts — write via discovery if needed.
	// Minimal: create a path findJSONL would find. Check discovery.ChatHistoryPath.
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name:      "aside-1",
		WorkDir:   dir,
		SessionID: sid,
		Purpose:   claudia.PurposeAside,
		Parent:    "jevons",
	}); err != nil {
		t.Fatal(err)
	}

	// Grok layout: sessions/<url-encoded-cwd>/<session-id>/chat_history.jsonl
	bucket := filepath.Join(sessRoot, "fake%2Fwork")
	chatDir := filepath.Join(bucket, sid)
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Grok Build shape (top-level content) — the live fleet path that was empty before T124 residual.
	line := `{"type":"user","content":[{"type":"text","text":"hello aside"}]}` + "\n" +
		`{"type":"assistant","content":"thinking…","tool_calls":[]}` + "\n" +
		`{"type":"tool_result","tool_call_id":"c1","content":"noise"}` + "\n" +
		`{"type":"assistant","content":"done with aside"}` + "\n"
	if err := os.WriteFile(filepath.Join(chatDir, "chat_history.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New("test", dir)
	s.SetRegistry(reg)
	s.SetTranscriptReader(transcript.NewReader(sessRoot))
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/aside-1/transcript", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Name    string           `json:"name"`
		Turns   []map[string]any `json:"turns"`
		Empty   bool             `json:"empty"`
		Purpose string           `json:"purpose"`
		Error   string           `json:"error"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "aside-1" || payload.Purpose != claudia.PurposeAside {
		t.Fatalf("payload=%+v", payload)
	}
	if payload.Empty || len(payload.Turns) < 2 {
		t.Fatalf("want non-empty Grok turns, got empty=%v turns=%+v err=%q", payload.Empty, payload.Turns, payload.Error)
	}
	if payload.Turns[0]["role"] != "user" {
		t.Fatalf("first turn: %+v", payload.Turns[0])
	}
	ut, _ := payload.Turns[0]["text"].(string)
	if ut != "hello aside" {
		t.Fatalf("user text=%q", ut)
	}
	// Assistant text should include both assistant lines (tool_result ignored).
	at, _ := payload.Turns[1]["text"].(string)
	if !strings.Contains(at, "thinking") || !strings.Contains(at, "done with aside") {
		t.Fatalf("assistant text=%q", at)
	}
}

// 🎯T213: RHS inspect loads sealed history from Claude projects only (no Grok tree).
func TestHandleAgentTranscriptClaudeProjectsOnly(t *testing.T) {
	dir := t.TempDir()
	projects := filepath.Join(dir, "projects")
	sid := "019fc2aa-bbbb-7ccc-8ddd-eeeeeeeeee01"
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name:      "claude-worker",
		WorkDir:   dir,
		SessionID: sid,
		Purpose:   claudia.PurposeWork,
		Provider:  "claude",
		Parent:    "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}

	bucket := filepath.Join(projects, "fake-claude-work")
	if err := os.MkdirAll(bucket, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"user","message":{"role":"user","content":"hello claude"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"claude reply"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(bucket, sid+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New("test", dir)
	s.SetRegistry(reg)
	// Claude-only roots: empty Grok sessions must still resolve the transcript.
	s.SetTranscriptReader(transcript.NewReaderRoots(discovery.Roots{
		ClaudeProjects: projects,
	}))
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/claude-worker/transcript", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Name  string           `json:"name"`
		Turns []map[string]any `json:"turns"`
		Empty bool             `json:"empty"`
		Error string           `json:"error"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Empty || len(payload.Turns) < 2 {
		t.Fatalf("want Claude turns, got empty=%v turns=%+v err=%q", payload.Empty, payload.Turns, payload.Error)
	}
	if payload.Turns[0]["role"] != "user" {
		t.Fatalf("first turn: %+v", payload.Turns[0])
	}
	ut, _ := payload.Turns[0]["text"].(string)
	if ut != "hello claude" {
		t.Fatalf("user text=%q", ut)
	}
}
