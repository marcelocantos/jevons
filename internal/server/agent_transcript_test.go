// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/transcript"
)

func TestMuxOmitsAgentTranscriptRoute(t *testing.T) {
	s := New("test", t.TempDir())
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/worker-x/transcript", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("dump HTTP must be gone: status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestProviderSessionDoesNotHydrateInspect(t *testing.T) {
	dir := t.TempDir()
	sessRoot := filepath.Join(dir, "sessions")
	sid := "019fc2aa-bbbb-7ccc-8ddd-ffffffffffff"
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
	chatDir := filepath.Join(sessRoot, "fake%2Fwork", sid)
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","content":[{"type":"text","text":"hello aside"}]}` + "\n" +
		`{"type":"assistant","content":"done with aside"}` + "\n"
	if err := os.WriteFile(filepath.Join(chatDir, "chat_history.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New("test", dir)
	s.SetRegistry(reg)
	s.SetTranscriptReader(transcript.NewReader(sessRoot))

	frames := inspectReplay(t, s, "aside-1")
	if replayUserCount(frames) != 0 {
		t.Fatalf("provider session must not hydrate inspect: %v", replayRoleRows(frames))
	}
	for _, m := range frames {
		if m["type"] == "agent_transcript" {
			t.Fatalf("dump envelope: %v", m)
		}
	}
}

func TestInspectReplayHydratesFromJournal(t *testing.T) {
	dir := t.TempDir()
	const name = "aside-journal"
	s := New("test", dir)
	s.journalAgentEvent(name, claudia.Event{Type: "user", Text: "hello aside"})
	s.journalAgentEvent(name, claudia.Event{Type: "assistant", Text: "done with aside", StopReason: "end_turn"})

	frames := inspectReplay(t, s, name)
	rows := replayRoleRows(frames)
	if !containsRow(rows, "user: hello aside") {
		t.Fatalf("journal user missing: %v", rows)
	}
	if !containsRow(rows, "assistant: done with aside") {
		t.Fatalf("journal assistant missing: %v", rows)
	}
}
