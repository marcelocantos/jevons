// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

const t679SID = "019f4f4b-945a-7a23-ba4c-51a0c26e0fbf"

func plantGrokUpdates(t *testing.T, sessionsDir, workDir, sid, body string) string {
	t.Helper()
	dir := filepath.Join(sessionsDir, EncodeCWDBucket(workDir), sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, grokUpdatesName)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGrokUpdatesLookupPresentInOrdinaryRoot(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	want := plantGrokUpdates(t, sessions, "/tmp/repo", t679SID, `{"method":"session/update"}`+"\n")
	got := GrokUpdatesLookup(Roots{GrokSessions: sessions}, t679SID)
	if got.State != LookupPresent {
		t.Fatalf("state=%s reason=%s err=%v want present", got.State, got.Reason, got.Err)
	}
	if got.Path != want {
		t.Fatalf("path=%q want %q", got.Path, want)
	}
}

func TestGrokUpdatesLookupPresentInExclusiveMCPRoot(t *testing.T) {
	root := t.TempDir()
	ordinary := filepath.Join(root, "ordinary")
	if err := os.MkdirAll(ordinary, 0o755); err != nil {
		t.Fatal(err)
	}
	exclusive := filepath.Join(root, "claudia-mcp-grok-home", "sessions")
	want := plantGrokUpdates(t, exclusive, "/tmp/excl", t679SID, `{"method":"session/update"}`+"\n")
	got := GrokUpdatesLookup(Roots{
		GrokSessions:     ordinary,
		GrokHomeSessions: []string{exclusive},
	}, t679SID)
	if got.State != LookupPresent || got.Path != want {
		t.Fatalf("exclusive-MCP lookup = %+v want present %q", got, want)
	}
}

func TestGrokUpdatesLookupChatHistoryAloneIsAbsent(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	dir := filepath.Join(sessions, EncodeCWDBucket("/tmp/repo"), t679SID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chat_history.jsonl"), []byte(`{"type":"user"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := GrokUpdatesLookup(Roots{GrokSessions: sessions}, t679SID)
	if got.State != LookupAbsent {
		t.Fatalf("chat_history.jsonl alone: %+v want absent", got)
	}
}

func TestGrokUpdatesLookupQueueAttachmentFileIsPresent(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	body := `{"type":"attachment","attachment":{"type":"queued_command","prompt":"hi"}}` + "\n"
	want := plantGrokUpdates(t, sessions, "/tmp/repo", t679SID, body)
	got := GrokUpdatesLookup(Roots{GrokSessions: sessions}, t679SID)
	if got.State != LookupPresent || got.Path != want {
		t.Fatalf("queue-attachment-only file must be present: %+v", got)
	}
}

func TestGrokUpdatesLookupEmptyRootIsAbsent(t *testing.T) {
	sessions := t.TempDir()
	got := GrokUpdatesLookup(Roots{GrokSessions: sessions}, t679SID)
	if got.State != LookupAbsent {
		t.Fatalf("empty resolved root: %+v want absent", got)
	}
}

func TestGrokUpdatesLookupMissingRootDirIsAbsent(t *testing.T) {
	got := GrokUpdatesLookup(Roots{GrokSessions: filepath.Join(t.TempDir(), "no-such")}, t679SID)
	if got.State != LookupAbsent {
		t.Fatalf("missing sessions dir: %+v want absent", got)
	}
}

func TestGrokUpdatesLookupNoRootsIsUnobservable(t *testing.T) {
	got := GrokUpdatesLookup(Roots{}, t679SID)
	if got.State != LookupUnobservable {
		t.Fatalf("unresolved roots: %+v want unobservable", got)
	}
}

func TestGrokUpdatesLookupPermissionErrorIsUnobservable(t *testing.T) {
	sessions := t.TempDir()
	if err := os.Chmod(sessions, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sessions, 0o755) })
	if _, err := os.ReadDir(sessions); err == nil {
		t.Skip("chmod 000 did not deny listing; cannot exercise permission failure")
	}
	got := GrokUpdatesLookup(Roots{GrokSessions: sessions}, t679SID)
	if got.State != LookupUnobservable {
		t.Fatalf("permission failure: %+v want unobservable", got)
	}
	if got.Err == nil {
		t.Fatal("permission failure must retain the error")
	}
}

func TestGrokUpdatesLookupPermissionOnOneRootBlocksAbsence(t *testing.T) {
	root := t.TempDir()
	readable := filepath.Join(root, "readable")
	if err := os.MkdirAll(readable, 0o755); err != nil {
		t.Fatal(err)
	}
	denied := filepath.Join(root, "denied")
	if err := os.MkdirAll(denied, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(denied, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(denied, 0o755) })
	if _, err := os.ReadDir(denied); err == nil {
		t.Skip("chmod 000 did not deny listing")
	}
	got := GrokUpdatesLookup(Roots{
		GrokSessions:     readable,
		GrokHomeSessions: []string{denied},
	}, t679SID)
	if got.State != LookupUnobservable {
		t.Fatalf("unreadable exclusive root must not collapse to absent: %+v", got)
	}
}

func TestTranscriptPathStillCollapsesFailureAndAbsence(t *testing.T) {
	// The defect T679.1 replaces: empty string for both "not there" and
	// "could not look". The honest answer is GrokUpdatesLookup.
	if got := TranscriptPath(Roots{}, t679SID); got != "" {
		t.Fatalf("unresolved TranscriptPath = %q want empty", got)
	}
	if got := TranscriptPath(Roots{GrokSessions: t.TempDir()}, t679SID); got != "" {
		t.Fatalf("absent TranscriptPath = %q want empty", got)
	}
}
