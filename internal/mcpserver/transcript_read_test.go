// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
)

// 🎯T304: jevons_transcript_read agent=<name> must return THAT agent's
// transcript only. Pre-fix the handler ignored agent= and always used
// GetID() (overseer/caller session) — silent substitution.

func transcriptReadFixture(t *testing.T) (*Server, map[string][]map[string]any) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Overseer + two workers with distinct sessions and distinct marker text.
	// agent-empty uses whitespace-only SessionID so Registry.Register accepts
	// it (non-empty string) while TrimSpace in the handler treats it as empty
	// — models "registered seat, no real session yet" without file surgery.
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "sess-overseer", Materialized: true, Provider: "grok"},
		{Name: "agent-a", WorkDir: dir, SessionID: "sess-a", Materialized: true, Provider: "grok", Parent: "jevons-po"},
		{Name: "agent-b", WorkDir: dir, SessionID: "sess-b", Materialized: true, Provider: "grok", Parent: "jevons-po"},
		{Name: "agent-empty", WorkDir: dir, SessionID: "   ", Materialized: false, Provider: "grok", Parent: "jevons-po"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}

	bySession := map[string][]map[string]any{
		"sess-overseer": {
			{"role": "user", "text": "OVERSEER_MARKER owner said this"},
			{"role": "assistant", "text": "OVERSEER_MARKER jevons reply"},
		},
		"sess-a": {
			{"role": "user", "text": "AGENT_A_MARKER worker A brief"},
			{"role": "assistant", "text": "AGENT_A_MARKER worker A progress"},
		},
		"sess-b": {
			{"role": "user", "text": "AGENT_B_MARKER worker B brief"},
			{"role": "assistant", "text": "AGENT_B_MARKER worker B progress"},
		},
	}

	s := &Server{
		registry: reg,
		transcript: &TranscriptOps{
			GetID: func() string { return "sess-overseer" },
			Read: func(sessionID string) ([]map[string]any, error) {
				turns, ok := bySession[sessionID]
				if !ok {
					return nil, fmt.Errorf("no transcript file for session %q", sessionID)
				}
				// Copy so tests can compare freely.
				out := make([]map[string]any, len(turns))
				copy(out, turns)
				return out, nil
			},
		},
	}
	return s, bySession
}

func callTranscriptRead(t *testing.T, s *Server, agent string) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	args := map[string]any{}
	if agent != "" {
		args["agent"] = agent
	}
	req.Params.Arguments = args
	res, err := s.handleTranscriptRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handleTranscriptRead: %v", err)
	}
	return res
}

// TestTranscriptReadAgentReturnsNamedOnly is the primary 🎯T304 oracle:
// reading agent-a returns A markers and never overseer or agent-b markers.
// Against pre-fix code this FAIL (GetID always → overseer).
func TestTranscriptReadAgentReturnsNamedOnly(t *testing.T) {
	s, _ := transcriptReadFixture(t)

	resA := callTranscriptRead(t, s, "agent-a")
	if resA.IsError {
		t.Fatalf("agent-a read error: %s", toolText(resA))
	}
	textA := toolText(resA)
	if !strings.Contains(textA, "AGENT_A_MARKER") {
		t.Fatalf("agent-a read missing AGENT_A_MARKER:\n%s", textA)
	}
	if strings.Contains(textA, "OVERSEER_MARKER") {
		t.Fatalf("agent-a read leaked overseer transcript (silent substitution):\n%s", textA)
	}
	if strings.Contains(textA, "AGENT_B_MARKER") {
		t.Fatalf("agent-a read leaked agent-b transcript:\n%s", textA)
	}
	if !strings.Contains(textA, "agent=agent-a") {
		t.Fatalf("agent-a read should label the agent:\n%s", textA)
	}

	resB := callTranscriptRead(t, s, "agent-b")
	if resB.IsError {
		t.Fatalf("agent-b read error: %s", toolText(resB))
	}
	textB := toolText(resB)
	if !strings.Contains(textB, "AGENT_B_MARKER") {
		t.Fatalf("agent-b read missing AGENT_B_MARKER:\n%s", textB)
	}
	if strings.Contains(textB, "AGENT_A_MARKER") || strings.Contains(textB, "OVERSEER_MARKER") {
		t.Fatalf("agent-b read leaked another agent's transcript:\n%s", textB)
	}
}

// TestTranscriptReadAgentNeverSubstitutesOverseer locks the dangerous class:
// even when GetID points at a rich overseer session, agent=<name> must not
// return those turns.
func TestTranscriptReadAgentNeverSubstitutesOverseer(t *testing.T) {
	s, _ := transcriptReadFixture(t)

	// Explicit overseer-default path still works without agent=.
	resDefault := callTranscriptRead(t, s, "")
	if resDefault.IsError {
		t.Fatalf("default read: %s", toolText(resDefault))
	}
	if !strings.Contains(toolText(resDefault), "OVERSEER_MARKER") {
		t.Fatalf("default (no agent) should still be overseer:\n%s", toolText(resDefault))
	}

	// Named path must not equal the default path content.
	resA := callTranscriptRead(t, s, "agent-a")
	if resA.IsError {
		t.Fatalf("agent-a: %s", toolText(resA))
	}
	if toolText(resA) == toolText(resDefault) {
		t.Fatal("agent-a response identical to overseer default — substitution still present")
	}
}

// TestTranscriptReadAgentEmptySessionExplicit: registered agent with no
// session_id returns empty/not-found signal, not overseer turns.
func TestTranscriptReadAgentEmptySessionExplicit(t *testing.T) {
	s, _ := transcriptReadFixture(t)

	res := callTranscriptRead(t, s, "agent-empty")
	if res.IsError {
		// Error is also an explicit signal (acceptable). Prefer non-error empty text.
		text := toolText(res)
		if strings.Contains(text, "OVERSEER_MARKER") {
			t.Fatalf("empty-session agent returned overseer content:\n%s", text)
		}
		return
	}
	text := toolText(res)
	if strings.Contains(text, "OVERSEER_MARKER") || strings.Contains(text, "AGENT_A_MARKER") {
		t.Fatalf("empty-session agent substituted another transcript:\n%s", text)
	}
	if !strings.Contains(strings.ToLower(text), "empty") &&
		!strings.Contains(strings.ToLower(text), "no session") {
		t.Fatalf("expected explicit empty/no-session signal, got:\n%s", text)
	}
}

// TestTranscriptReadAgentNotFoundExplicit: unknown name must not return
// overseer (or any other) turns.
func TestTranscriptReadAgentNotFoundExplicit(t *testing.T) {
	s, _ := transcriptReadFixture(t)

	res := callTranscriptRead(t, s, "no-such-agent")
	if !res.IsError {
		text := toolText(res)
		if strings.Contains(text, "OVERSEER_MARKER") || strings.Contains(text, "Turn ") {
			t.Fatalf("unknown agent returned a real transcript:\n%s", text)
		}
	}
	text := toolText(res)
	if strings.Contains(text, "OVERSEER_MARKER") {
		t.Fatalf("unknown agent substituted overseer:\n%s", text)
	}
	if !strings.Contains(strings.ToLower(text), "not found") {
		t.Fatalf("expected not-found signal, got:\n%s", text)
	}
}

// TestTranscriptReadAgentMissingTranscriptFile: session_id set but Read
// fails → explicit error, not GetID fallback.
func TestTranscriptReadAgentMissingTranscriptFile(t *testing.T) {
	s, _ := transcriptReadFixture(t)
	// Register agent with a session that Read does not know.
	dir := t.TempDir()
	if err := s.registry.Register(claudia.AgentDef{
		Name: "agent-lost", WorkDir: dir, SessionID: "sess-gone",
		Materialized: true, Provider: "grok", Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}

	res := callTranscriptRead(t, s, "agent-lost")
	if !res.IsError {
		t.Fatalf("expected error for missing transcript file, got: %s", toolText(res))
	}
	text := toolText(res)
	if strings.Contains(text, "OVERSEER_MARKER") {
		t.Fatalf("missing-file agent substituted overseer:\n%s", text)
	}
	if !strings.Contains(text, "agent-lost") {
		t.Fatalf("error should name the agent:\n%s", text)
	}
}
