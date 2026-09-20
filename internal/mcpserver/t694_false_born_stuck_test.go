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

func TestT694GrokHomesUpdatesJSONLIsPresentWithoutClaudeJSONL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	work := filepath.Join(home, "work")
	homes := discovery.ClaudiaGrokHomesRoot()
	want := plantT694GrokHomesUpdates(t, homes, work, t694SID, `{"method":"session/update"}`+"\n")

	got := LookupTranscriptExistence(t694Query(homes, work, t694SID))
	if got.Verdict != ExistencePresent || got.Path != want {
		t.Fatalf("grok-homes updates.jsonl: %+v want present %q", got, want)
	}

	claudePath := claudia.SessionJSONLPath(t694SID, work)
	if _, err := os.Stat(claudePath); !os.IsNotExist(err) {
		t.Fatalf("fixture must not plant Claude JSONL: path=%s err=%v", claudePath, err)
	}

	viaDefaults := LookupTranscriptExistence(TranscriptExistenceQuery{
		Name: "jevons-po", Provider: claudia.ProviderGrok,
		SessionID: t694SID, WorkDir: work,
		Roots: DefaultSessionRoots(),
	})
	if viaDefaults.Verdict != ExistencePresent || viaDefaults.Path != want {
		t.Fatalf("DefaultSessionRoots grok-homes: %+v want present %q", viaDefaults, want)
	}
}

func TestT694ClaudeJSONLOnlyMutationGoesRed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := filepath.Join(home, "work")
	homes := filepath.Join(home, ".local", "state", "claudia", "grok-homes")
	plantT694GrokHomesUpdates(t, homes, work, t694SID, `{"method":"session/update"}`+"\n")
	q := t694Query(homes, work, t694SID)

	if got := LookupTranscriptExistence(q); got.Verdict != ExistencePresent {
		t.Fatalf("control: real lookup must be present: %+v", got)
	}

	// Mutant: existence is only claudia.SessionExists / Claude JSONL. This
	// is the live false-green — Grok updates.jsonl growing, no Claude file.
	mutant := LookupTranscriptExistence(TranscriptExistenceQuery{
		Name: q.Name, Provider: claudia.ProviderClaude,
		SessionID: q.SessionID, WorkDir: q.WorkDir, Roots: q.Roots,
	})
	if mutant.Verdict == ExistencePresent {
		t.Fatal("claude-JSONL-only mutant reported present; this oracle would not catch the live miss")
	}
	if mutant.Verdict != ExistenceAbsent {
		t.Fatalf("claude-JSONL-only mutant: %+v want absent", mutant)
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
	if e.s.seatIsBornStuck(*e.reg.Def(e.name)) {
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
