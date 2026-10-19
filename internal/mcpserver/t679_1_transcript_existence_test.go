// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
)

const t679Session = "019f4f4b-945a-7a23-ba4c-51a0c26e0fc0"

func TestT679_1ClaudePresentAbsentAndPermission(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := t.TempDir()
	sid := "dfb91d71-t6791"

	absent := LookupTranscriptExistence(TranscriptExistenceQuery{
		Name: "jv-t679.1-claude", Provider: claudeJSONLSeat,
		SessionID: sid, WorkDir: workDir,
	})
	if absent.Verdict != ExistenceAbsent {
		t.Fatalf("claude missing JSONL: %+v want absent", absent)
	}

	path := claudia.SessionJSONLPath(sid, workDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"type":"user"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	present := LookupTranscriptExistence(TranscriptExistenceQuery{
		Name: "jv-t679.1-claude", Provider: claudeJSONLSeat,
		SessionID: sid, WorkDir: workDir,
	})
	if present.Verdict != ExistencePresent || present.Path != path {
		t.Fatalf("claude JSONL: %+v want present %q", present, path)
	}

	bucket := filepath.Dir(path)
	if err := os.Chmod(bucket, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bucket, 0o755) })
	if _, err := os.Stat(path); err == nil {
		t.Skip("chmod 000 did not deny stat; cannot exercise claude permission failure")
	}
	denied := LookupTranscriptExistence(TranscriptExistenceQuery{
		Name: "jv-t679.1-claude", Provider: claudeJSONLSeat,
		SessionID: sid, WorkDir: workDir,
	})
	if denied.Verdict != ExistenceUnobservable {
		t.Fatalf("claude permission failure: %+v want unobservable", denied)
	}
}

// A grok row is the xai-oauth sidecar seat it launches as (🎯T1079). The
// Grok Build store this test once resolved (exclusive-MCP homes, 🎯T679.1)
// is the CLI transport claudia v0.58 refuses, so a session file left there
// is not the seat's conversation: both spellings read unobservable, never a
// present or absent verdict drawn from it. GrokUpdatesLookup's own error
// retention stays pinned in internal/discovery.
func TestT679_1GrokRowIsTheSidecarSeat(t *testing.T) {
	root := t.TempDir()
	ordinary := filepath.Join(root, "sessions")
	exclusive := filepath.Join(root, "claudia-mcp-grok-home", "sessions")
	dir := filepath.Join(exclusive, discovery.EncodeCWDBucket("/tmp/repo"), t679Session)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "updates.jsonl"), []byte(`{"method":"session/update"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	roots := discovery.Roots{GrokSessions: ordinary, GrokHomeSessions: []string{exclusive}}
	for _, provider := range []claudia.Provider{claudia.ProviderGrok, "xai-oauth"} {
		got := LookupTranscriptExistence(TranscriptExistenceQuery{
			Name: "jv-t679.1-grok", Provider: provider, SessionID: t679Session, Roots: roots,
		})
		if got.Verdict != ExistenceUnobservable || got.Path != "" {
			t.Fatalf("%s with a Grok Build store: %+v want unobservable", provider, got)
		}
	}
}

func TestT679_1CursorIsUnobservableEvenWithPhantomJSONLAndStoreDB(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := t.TempDir()
	sid := t679Session

	phantom := claudia.SessionJSONLPath(sid, workDir)
	if err := os.MkdirAll(filepath.Dir(phantom), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(phantom, []byte(`{"type":"user","message":{"content":"phantom"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := claudia.CursorACPStorePath(sid)
	if store == "" {
		t.Fatal("CursorACPStorePath empty under redirected HOME")
	}
	if err := os.MkdirAll(filepath.Dir(store), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store, []byte("sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	sessions := filepath.Join(t.TempDir(), "sessions")
	gdir := filepath.Join(sessions, discovery.EncodeCWDBucket(workDir), sid)
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gdir, "updates.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := LookupTranscriptExistence(TranscriptExistenceQuery{
		Name: "jv-t81-jevons-pin", Provider: claudia.ProviderCursor,
		SessionID: sid, WorkDir: workDir,
		Roots: discovery.Roots{GrokSessions: sessions, ClaudeProjects: filepath.Join(home, ".claude", "projects")},
	})
	if got.Verdict != ExistenceUnobservable {
		t.Fatalf("cursor must not treat phantom JSONL or store.db as existence: %+v", got)
	}
	if got.Path != "" {
		t.Fatalf("cursor unobservable must not cite a path, got %q", got.Path)
	}
	if !strings.Contains(got.Reason, "sidecar seat has no spool record") {
		t.Fatalf("cursor reason must record why, got %q", got.Reason)
	}
	ok, err := claudia.SessionExists(sid, workDir)
	if err != nil || !ok {
		t.Fatalf("fixture invalid: phantom Claude JSONL should exist for SessionExists: ok=%v err=%v", ok, err)
	}
}

func TestT679_1CodexIsUnobservable(t *testing.T) {
	got := LookupTranscriptExistence(TranscriptExistenceQuery{
		Name: "jv-t679.1-codex", Provider: claudia.ProviderCodex,
		SessionID: t679Session, WorkDir: t.TempDir(),
	})
	if got.Verdict != ExistenceUnobservable {
		t.Fatalf("codex: %+v want unobservable", got)
	}
}

func TestT679_1EmptySessionIDIsUnobservable(t *testing.T) {
	got := LookupTranscriptExistence(TranscriptExistenceQuery{
		Name: "x", Provider: claudia.ProviderGrok, WorkDir: t.TempDir(),
		Roots: discovery.Roots{GrokSessions: t.TempDir()},
	})
	if got.Verdict != ExistenceUnobservable {
		t.Fatalf("empty session id: %+v want unobservable", got)
	}
}

func TestT679_1DefaultSessionRootsIncludesExclusiveMCP(t *testing.T) {
	got := DefaultSessionRoots()
	wantHomes := discovery.ExclusiveGrokSessionRoots()
	if !reflect.DeepEqual(got.GrokHomeSessions, wantHomes) {
		t.Fatalf("GrokHomeSessions=%v want %v", got.GrokHomeSessions, wantHomes)
	}
	if got.GrokSessions == "" || got.ClaudeProjects == "" {
		t.Fatalf("ordinary roots missing: %+v", got)
	}
}

func TestT679_1ProviderKeepsClaudeTranscriptStillRefusesCursor(t *testing.T) {
	if providerKeepsClaudeTranscript(claudia.ProviderCursor) {
		t.Fatal("cursor must stay a live-stream surface; the existence seam must not bypass T501")
	}
	if !providerKeepsClaudeTranscript(claudeJSONLSeat) {
		t.Fatal("a seat with no stored provider must remain the durable JSONL surface")
	}
}
