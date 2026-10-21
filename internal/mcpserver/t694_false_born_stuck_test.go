// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
)

// Load-bearing specimen (2026-09-20): jevons-po session 01a0bdc7 was marked
// born-stuck elapsed=2m52s while its grok-homes updates.jsonl was 4.4MB
// and growing. UUID shape matches that live id.
const t694SID = "01a0bdc7-0627-7611-aa06-c8e40f7c9398"

func plantT694GrokHomesUpdates(t *testing.T, homes, workDir, sid, body string) string {
	t.Helper()
	dir := filepath.Join(discovery.ClaudiaGrokHomeSessionsDir(homes, sid), discovery.EncodeCWDBucket(workDir), sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "updates.jsonl")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func t694Query(homes, workDir, sid string) TranscriptExistenceQuery {
	return TranscriptExistenceQuery{
		Name: "jevons-po", Provider: claudia.ProviderGrok,
		SessionID: sid, WorkDir: workDir,
		Roots: discovery.Roots{
			GrokSessions:     filepath.Join(t694Home(workDir), ".grok", "sessions"),
			ClaudiaGrokHomes: homes,
			ClaudeProjects:   filepath.Join(t694Home(workDir), ".claude", "projects"),
		},
	}
}

func t694Home(workDir string) string {
	return filepath.Dir(workDir)
}

// 🎯T1079: the grok row the specimen names is the xai-oauth sidecar seat, so
// its grok-homes updates.jsonl is no longer read as its transcript at all:
// the seat reads unobservable, which can never accuse it of being born
// stuck. The born-stuck half of this target is pinned below.
func TestT694GrokHomesStoreIsNotTheSeatsTranscript(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	work := filepath.Join(home, "work")
	homes := discovery.ClaudiaGrokHomesRoot()
	plantT694GrokHomesUpdates(t, homes, work, t694SID, `{"method":"session/update"}`+"\n")

	got := LookupTranscriptExistence(t694Query(homes, work, t694SID))
	if got.Verdict != ExistenceUnobservable {
		t.Fatalf("grok row over a grok-homes store: %+v want unobservable", got)
	}
}

func TestT694GrokSeatWithUpdatesJSONLIsNotBornStuckAfterGrace(t *testing.T) {
	e := t679_2Harness(t, claudia.ProviderGrok, t694SID)
	state := filepath.Join(e.home, ".local", "state")
	t.Setenv("XDG_STATE_HOME", state)
	homes := filepath.Join(state, "claudia", "grok-homes")
	plantT694GrokHomesUpdates(t, homes, e.work, t694SID, `{"method":"session/update"}`+"\n")
	e.s.SetBirthRoots(discovery.Roots{
		GrokSessions:     filepath.Join(e.home, ".grok", "sessions"),
		ClaudiaGrokHomes: homes,
		ClaudeProjects:   filepath.Join(e.home, ".claude", "projects"),
	})
	e.accept()
	e.clock.add(BornStuckGrace)
	body := e.list()
	if strings.Contains(body, AgentStatusBornStuck) {
		t.Fatalf("Grok grok-homes updates.jsonl marked born-stuck:\n%s", body)
	}
	if n := e.parentNotices(); len(n) != 0 {
		t.Fatalf("Grok seat with updates.jsonl produced a born-stuck notice: %v", n)
	}
	if e.s.bornStuck(*e.reg.Def(e.name)) {
		t.Fatal("diagnoseBirth treated grok-homes updates.jsonl as absent")
	}
}

func TestT694DefaultSessionRootsIncludesClaudiaGrokHomes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	got := DefaultSessionRoots()
	want := discovery.ClaudiaGrokHomesRoot()
	if got.ClaudiaGrokHomes != want || want == "" {
		t.Fatalf("ClaudiaGrokHomes=%q want %q", got.ClaudiaGrokHomes, want)
	}
	if !strings.HasPrefix(want, home) {
		t.Fatalf("redirected HOME must own grok-homes root, got %q", want)
	}
}
