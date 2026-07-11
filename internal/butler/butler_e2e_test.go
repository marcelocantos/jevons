// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package butler_test

// This is the butler-loop integration oracle for 🎯T30. It drives the
// real Butler over the real thread store, session scanner, and
// transcript reader against on-disk Grok session fixtures — no live
// Grok, so it is deterministic and CI-runnable.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/thread"
	"github.com/marcelocantos/jevons/internal/transcript"
)

const ownerSession = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

// fixedNow anchors the harness clock so fixture timestamps and status
// derivation agree deterministically.
var fixedNow = time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)

// writeOwnerTranscript writes a Grok-style session under sessionsDir
// (chat_history.jsonl) that the butler will adopt observe-only.
func writeOwnerTranscript(t *testing.T, sessionsDir string) string {
	t.Helper()
	recent := fixedNow.Add(-1 * time.Minute).Format(time.RFC3339)

	lines := []string{
		fmt.Sprintf(`{"type":"user","timestamp":%q,"content":"rebuild the maze generator"}`, recent),
		fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","name":"Edit","id":"t1"}]}}`, recent),
		fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"Done — the generator now uses recursive backtracking."}]}}`, recent),
	}

	bucket := discovery.EncodeCWDBucket("/work/multimaze2")
	dir := filepath.Join(sessionsDir, bucket, ownerSession)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "chat_history.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture transcript: %v", err)
	}
	return path
}

// newButler builds a butler over a temp store + a scanner/reader rooted
// at sessionsDir, with the harness clock.
func newButler(t *testing.T, storePath, sessionsDir string) *butler.Butler {
	t.Helper()
	store, err := thread.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return butler.New(butler.Config{
		Store:   store,
		Scanner: discovery.NewScanner(sessionsDir),
		Reader:  transcript.NewReader(sessionsDir),
		Now:     func() time.Time { return fixedNow },
	})
}

// TestAdoptObserveAndStatus covers T30 criteria ADOPT-OBSERVE and STATUS.
func TestAdoptObserveAndStatus(t *testing.T) {
	dir := t.TempDir()
	projectsDir := filepath.Join(dir, "projects")
	storePath := filepath.Join(dir, "threads.json")

	jsonlPath := writeOwnerTranscript(t, projectsDir)
	statBefore, err := os.Stat(jsonlPath)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}

	b := newButler(t, storePath, projectsDir)

	// --- ADOPT-OBSERVE: register the owner's session as a durable thread.
	th, err := b.Adopt(butler.AdoptArgs{
		SessionID:   ownerSession,
		Description: "the multimaze2 rebuild",
		ObserveOnly: true,
	})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if th.Kind != thread.KindAdopted {
		t.Fatalf("kind = %q, want adopted", th.Kind)
	}
	if th.SessionID != ownerSession {
		t.Fatalf("session id = %q, want %q", th.SessionID, ownerSession)
	}
	if th.WorkDir != "/work/multimaze2" {
		t.Fatalf("workdir = %q, want resolved from transcript cwd", th.WorkDir)
	}
	if th.Description != "the multimaze2 rebuild" {
		t.Fatalf("description not stored: %q", th.Description)
	}

	// Non-invasive: adoption must not spawn/take over a process, and must
	// not mutate the observed transcript.
	statAfter, err := os.Stat(jsonlPath)
	if err != nil {
		t.Fatalf("stat fixture after adopt: %v", err)
	}
	if !statAfter.ModTime().Equal(statBefore.ModTime()) || statAfter.Size() != statBefore.Size() {
		t.Fatal("adoption mutated the observed transcript — not non-invasive")
	}

	// --- STATUS: readable, transcript-derived, on demand.
	got, err := b.Status(th.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Status.State != thread.StateDone {
		t.Fatalf("state = %q, want done (last turn concluded recently)", got.Status.State)
	}
	if got.Status.ProcessUp {
		t.Fatal("adopted thread reports a live process it should not own")
	}
	if !strings.Contains(got.Status.Summary, "recursive backtracking") {
		t.Fatalf("summary lacks recent-activity detail: %q", got.Status.Summary)
	}

	// --- Thread appears in the list with its status.
	list := b.List()
	if len(list) != 1 || list[0].Thread.ID != th.ID {
		t.Fatalf("list = %+v, want the single adopted thread", list)
	}
}

// TestNeverLoseThreadAcrossRestart covers the record-durability half of
// the NEVER LOSE A THREAD criterion: a fresh butler over the same store
// path recovers the full thread set. (Process rehydration is exercised
// by the live-claude tier.)
func TestNeverLoseThreadAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	projectsDir := filepath.Join(dir, "projects")
	storePath := filepath.Join(dir, "threads.json")
	writeOwnerTranscript(t, projectsDir)

	b := newButler(t, storePath, projectsDir)
	if _, err := b.Adopt(butler.AdoptArgs{SessionID: ownerSession, ID: "po", Description: "grandfathered", ObserveOnly: true}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	// Simulate a daemon restart: a brand-new butler + store over the same
	// file, with nothing carried in memory.
	b2 := newButler(t, storePath, projectsDir)
	got, err := b2.Status("po")
	if err != nil {
		t.Fatalf("thread lost across restart: %v", err)
	}
	if got.Thread.Description != "grandfathered" || got.Thread.SessionID != ownerSession {
		t.Fatalf("thread record corrupted across restart: %+v", got.Thread)
	}
}

// TestAdoptRejectsUnobservableSession asserts adoption of a session with
// no transcript on disk fails cleanly rather than registering a thread
// that can never be observed.
func TestAdoptRejectsUnobservableSession(t *testing.T) {
	dir := t.TempDir()
	b := newButler(t, filepath.Join(dir, "threads.json"), filepath.Join(dir, "projects"))

	if _, err := b.Adopt(butler.AdoptArgs{SessionID: "ffffffff-ffff-ffff-ffff-ffffffffffff"}); err == nil {
		t.Fatal("expected adoption of a session with no transcript to fail")
	}
	if _, err := b.Adopt(butler.AdoptArgs{SessionID: "not-a-uuid"}); err == nil {
		t.Fatal("expected adoption with a malformed session id to fail")
	}
}
