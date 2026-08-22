// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/transcript"
)

// 🎯T533: inspect history is a recent window, not the whole journal.

func t533AgentServer(t *testing.T, name string) *Server {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name:      name,
		WorkDir:   dir,
		SessionID: "sess-t533",
		Purpose:   claudia.PurposeWork,
		Parent:    "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", dir)
	s.SetRegistry(reg)
	s.SetTranscriptReader(transcript.NewReader(filepath.Join(dir, "sessions")))
	return s
}

func writeAgentJournalLines(t *testing.T, s *Server, name string, lines []string) {
	t.Helper()
	path := s.agentJournalsFor().path(name)
	if path == "" {
		t.Fatal("empty journal path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func t533UserLine(text string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []map[string]any{{"type": "text", "text": text}},
		},
	})
	return string(b)
}

func t533AssistantLine(text string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
			"stop_reason": "end_turn",
		},
	})
	return string(b)
}

func t533ToolUseLine() string {
	return `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read"}]}}`
}

func TestT533InspectHistoryWindow(t *testing.T) {
	const name = "jv-t533-probe"
	s := t533AgentServer(t, name)

	var lines []string
	for i := 1; i <= 80; i++ {
		lines = append(lines, t533UserLine(fmt.Sprintf("turn-%02d", i)))
		if i == 80 {
			lines = append(lines, t533ToolUseLine())
		}
		lines = append(lines, t533AssistantLine(fmt.Sprintf("ack-%02d", i)))
	}
	writeAgentJournalLines(t, s, name, lines)

	frames := inspectReplay(t, s, name)
	users := replayUserCount(frames)
	if users > inspectHistoryTurns {
		t.Fatalf("history user turns=%d want ≤%d", users, inspectHistoryTurns)
	}
	if users != inspectHistoryTurns {
		t.Fatalf("80-user journal should show the full window, got %d", users)
	}
	rows := replayRoleRows(frames)
	if !containsRow(rows, "user: turn-51") {
		t.Fatalf("window start missing turn-51: %v", rows[:3])
	}
	if containsRow(rows, "user: turn-50") {
		t.Fatalf("window leaked turn-50: %v", rows[:3])
	}
	for _, m := range frames {
		if m["type"] == "agent_transcript" {
			t.Fatalf("dump envelope must not appear: %v", m)
		}
	}
}


