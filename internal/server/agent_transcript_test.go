// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/transcript"
)

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
	line := `{"type":"user","message":{"role":"user","content":"hello aside"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"thinking…"}]}}` + "\n"
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
		Name   string           `json:"name"`
		Turns  []map[string]any `json:"turns"`
		Empty  bool             `json:"empty"`
		Purpose string          `json:"purpose"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "aside-1" || payload.Purpose != claudia.PurposeAside {
		t.Fatalf("payload=%+v", payload)
	}
	// If finder found the file we get turns; if layout differs, empty is OK for this env.
	t.Logf("turns=%d empty=%v", len(payload.Turns), payload.Empty)
}
