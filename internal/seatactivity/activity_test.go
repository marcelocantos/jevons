// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatactivity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
)

const (
	t702GrokSID     = "019f4f4b-945a-7a23-ba4c-51a0c26e0f70"
	t702GrokHomeSID = "019f4f4b-945a-7a23-ba4c-51a0c26e0f71"
)

func plantGrokUpdates(t *testing.T, sessionsDir, workDir, sid, body string) string {
	t.Helper()
	dir := filepath.Join(sessionsDir, discovery.EncodeCWDBucket(workDir), sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "updates.jsonl")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func plantClaudeJSONL(t *testing.T, workDir, sid, body string) string {
	t.Helper()
	p := claudia.SessionJSONLPath(sid, workDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func stamp(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got := fi.ModTime()
	if d := got.Sub(when); d > time.Second || d < -time.Second {
		t.Fatalf("chtimes did not stick on %s: got %s want %s", path, got, when)
	}
}

func TestLocateClaudePresentAndAbsent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	work := t.TempDir()
	sid := "t702-claude-sid"
	absent := Locate(Query{Name: "claude-seat", Provider: claudia.ProviderClaude, SessionID: sid, WorkDir: work})
	if absent.State != discovery.LookupAbsent {
		t.Fatalf("missing JSONL: %+v want absent", absent)
	}
	path := plantClaudeJSONL(t, work, sid, `{"type":"user"}`+"\n")
	present := Locate(Query{Name: "claude-seat", Provider: claudia.ProviderClaude, SessionID: sid, WorkDir: work})
	if present.State != discovery.LookupPresent || present.Path != path {
		t.Fatalf("claude JSONL: %+v want present %q", present, path)
	}
}

func TestLocateGrokPresentHomesUnobservableAndAbsent(t *testing.T) {
	sessions := filepath.Join(t.TempDir(), "sessions")
	work := "/tmp/t702-repo"
	want := plantGrokUpdates(t, sessions, work, t702GrokSID, `{"method":"session/update"}`+"\n")
	present := Locate(Query{
		Name: "grok-seat", Provider: claudia.ProviderGrok, SessionID: t702GrokSID,
		Roots: discovery.Roots{GrokSessions: sessions},
	})
	if present.State != discovery.LookupPresent || present.Path != want {
		t.Fatalf("ordinary grok: %+v want present %q", present, want)
	}

	homes := t.TempDir()
	homePath := plantGrokUpdates(t, discovery.ClaudiaGrokHomeSessionsDir(homes, t702GrokHomeSID), work, t702GrokHomeSID, `{"method":"session/update"}`+"\n")
	viaHomes := Locate(Query{
		Name: "grok-homes-seat", Provider: claudia.ProviderGrok, SessionID: t702GrokHomeSID,
		Roots: discovery.Roots{ClaudiaGrokHomes: homes},
	})
	if viaHomes.State != discovery.LookupPresent || viaHomes.Path != homePath {
		t.Fatalf("grok-homes: %+v want present %q", viaHomes, homePath)
	}

	unresolved := Locate(Query{Name: "grok-seat", Provider: claudia.ProviderGrok, SessionID: t702GrokSID})
	if unresolved.State != discovery.LookupUnobservable {
		t.Fatalf("no roots: %+v want unobservable", unresolved)
	}

	empty := filepath.Join(t.TempDir(), "sessions")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	absent := Locate(Query{
		Name: "grok-seat", Provider: claudia.ProviderGrok, SessionID: t702GrokSID,
		Roots: discovery.Roots{GrokSessions: empty},
	})
	if absent.State != discovery.LookupAbsent {
		t.Fatalf("empty grok root: %+v want absent", absent)
	}
}

func TestLocateCursorIsUnobservable(t *testing.T) {
	got := Locate(Query{
		Name: "jv-cursor", Provider: claudia.ProviderCursor,
		SessionID: t702GrokSID, WorkDir: t.TempDir(),
	})
	if got.State != discovery.LookupUnobservable {
		t.Fatalf("cursor: %+v want unobservable", got)
	}
	if got.Path != "" {
		t.Fatalf("cursor must not cite a path, got %q", got.Path)
	}
	if !strings.Contains(got.Reason, "store.db") {
		t.Fatalf("cursor reason must say why, got %q", got.Reason)
	}
}

func TestLocateEmptySessionIDIsUnobservable(t *testing.T) {
	got := Locate(Query{Name: "x", Provider: claudia.ProviderGrok, Roots: discovery.Roots{GrokSessions: t.TempDir()}})
	if got.State != discovery.LookupUnobservable {
		t.Fatalf("empty session id: %+v want unobservable", got)
	}
}

func TestLookupKnownAgeUsesInjectedClock(t *testing.T) {
	// Mutation: ignoring q.Now and using time.Now() makes both ages ~years,
	// not 10s and 45min, against this 2020 clock.
	now := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	sessions := filepath.Join(t.TempDir(), "sessions")
	freshPath := plantGrokUpdates(t, sessions, "/tmp/t702", t702GrokSID, "{}\n")
	stalePath := plantGrokUpdates(t, sessions, "/tmp/t702", t702GrokHomeSID, "{}\n")
	stamp(t, freshPath, now.Add(-10*time.Second))
	stamp(t, stalePath, now.Add(-45*time.Minute))
	roots := discovery.Roots{GrokSessions: sessions}

	fresh := Lookup(Query{Provider: claudia.ProviderGrok, SessionID: t702GrokSID, Roots: roots, Now: now})
	stale := Lookup(Query{Provider: claudia.ProviderGrok, SessionID: t702GrokHomeSID, Roots: roots, Now: now})
	if fresh.Verdict != VerdictKnown || stale.Verdict != VerdictKnown {
		t.Fatalf("fresh=%+v stale=%+v want known", fresh, stale)
	}
	if d := absDuration(fresh.Age - 10*time.Second); d > time.Second {
		t.Fatalf("fresh age=%s want 10s (q.Now was ignored?)", fresh.Age)
	}
	if d := absDuration(stale.Age - 45*time.Minute); d > time.Second {
		t.Fatalf("stale age=%s want 45min (wall-clock now substituted?)", stale.Age)
	}
	if fresh.Age >= 30*time.Minute {
		t.Fatalf("10s fixture aged past 30min: %s", fresh.Age)
	}
	if stale.Age < 30*time.Minute {
		t.Fatalf("45min fixture aged under 30min: %s", stale.Age)
	}
}

func TestLookupUnknownNeverReportsZeroAge(t *testing.T) {
	got := Lookup(Query{Provider: claudia.ProviderCursor, SessionID: t702GrokSID, Name: "cursor-seat"})
	if got.Verdict != VerdictUnknown {
		t.Fatalf("cursor lookup: %+v want unknown", got)
	}
	if !got.LastMove.IsZero() || got.Age != 0 {
		t.Fatalf("unknown reading must leave LastMove/Age zero-value, got %+v", got)
	}
}

func TestLookupFutureMtimeClampsAgeToZero(t *testing.T) {
	now := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	sessions := filepath.Join(t.TempDir(), "sessions")
	path := plantGrokUpdates(t, sessions, "/tmp/t702", t702GrokSID, "{}\n")
	stamp(t, path, now.Add(time.Hour))
	got := Lookup(Query{
		Provider: claudia.ProviderGrok, SessionID: t702GrokSID,
		Roots: discovery.Roots{GrokSessions: sessions}, Now: now,
	})
	if got.Verdict != VerdictKnown {
		t.Fatalf("future mtime: %+v want known", got)
	}
	if got.Age != 0 {
		t.Fatalf("future mtime age=%s want 0", got.Age)
	}
}

func TestDefaultRootsIncludesGrokHomes(t *testing.T) {
	got := DefaultRoots()
	if got.ClaudiaGrokHomes != discovery.ClaudiaGrokHomesRoot() {
		t.Fatalf("ClaudiaGrokHomes=%q want %q", got.ClaudiaGrokHomes, discovery.ClaudiaGrokHomesRoot())
	}
	if got.GrokSessions == "" || got.ClaudeProjects == "" {
		t.Fatalf("ordinary roots empty: %+v", got)
	}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
