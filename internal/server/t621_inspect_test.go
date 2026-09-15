// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/transcript"
)

func TestT621InspectOmitsCompactedTurns(t *testing.T) {
	dir := t.TempDir()
	const (
		name = "jv-t621-inspect"
		sid  = "019fd13d-e500-7913-b96c-981e50aa6210"
	)
	claudeRoot := filepath.Join(dir, "claude-projects")
	path := filepath.Join(claudeRoot, "work-repo", sid+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		`{"type":"user","uuid":"u1","message":{"role":"user","content":"old discarded"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"role":"assistant","content":[{"type":"text","text":"old reply"}]}}`,
		`{"type":"system","subtype":"compact_boundary","uuid":"b1","compactMetadata":{"preservedSegment":{"headUuid":"u2","anchorUuid":"a1","tailUuid":"a2"}}}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","message":{"role":"user","content":"kept after compact"}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2","message":{"role":"assistant","content":[{"type":"text","text":"kept reply"}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: sid, Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", dir)
	s.SetRegistry(reg)
	s.SetTranscriptReader(transcript.NewReaderRoots(discovery.Roots{ClaudeProjects: claudeRoot}))
	writeAgentJournalLines(t, s, name, []string{
		t533UserLine("old discarded"),
		t533AssistantLine("old reply"),
	})

	joined := strings.Join(replayRoleRows(inspectReplay(t, s, name)), " | ")
	if strings.Contains(joined, "old discarded") || strings.Contains(joined, "old reply") {
		t.Fatalf("compacted-away turns painted inspect: %s", joined)
	}
	if !strings.Contains(joined, "kept after compact") || !strings.Contains(joined, "kept reply") {
		t.Fatalf("reconstructed turns missing: %s", joined)
	}
}

func TestT621InspectHydratesFromGrokUpdates(t *testing.T) {
	dir := t.TempDir()
	const (
		name = "jv-t621-grok"
		sid  = "019fd13d-e500-7913-b96c-981e50aa6211"
	)
	sessions := filepath.Join(dir, "grok-sessions")
	bucket := filepath.Join(sessions, discovery.EncodeCWDBucket("/work/repo"), sid)
	if err := os.MkdirAll(bucket, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "chat_history.jsonl"), []byte(
		`{"type":"user","content":"only the late turn survived compact"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "updates.jsonl"), []byte(strings.Join([]string{
		`{"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"early user turn"}}}}`,
		`{"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"early assistant"}}}}`,
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: "/work/repo", SessionID: sid, Provider: claudia.ProviderGrok,
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", dir)
	s.SetRegistry(reg)
	s.SetTranscriptReader(transcript.NewReaderRoots(discovery.Roots{GrokSessions: sessions}))

	joined := strings.Join(replayRoleRows(inspectReplay(t, s, name)), " | ")
	if !strings.Contains(joined, "early user turn") {
		t.Fatalf("updates.jsonl did not hydrate inspect: %s", joined)
	}
}
