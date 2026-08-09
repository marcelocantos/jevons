// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package handover_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/handover"
)

// TestSeedMessageNamesTranscriptAndOrdersTheRead: the whole handover is
// this prompt, so it must carry the path, say the session cannot be
// resumed, and tell the successor to read from the END — a long-lived
// agent's transcript is large, and a front-to-back crawl would burn the
// context the handover exists to preserve (🎯T285).
func TestSeedMessageNamesTranscriptAndOrdersTheRead(t *testing.T) {
	path := "/Users/marcelo/.grok/sessions/019fd13d/chat_history.jsonl"
	seed := handover.SeedMessage("grok", "claude", path)
	if seed == "" {
		t.Fatal("no seed produced for a known transcript")
	}
	if !strings.Contains(seed, path) {
		t.Errorf("seed does not name the transcript path:\n%s", seed)
	}
	low := strings.ToLower(seed)
	for _, want := range []string{
		"grok", "claude",
		"no memory of the previous session",
		"cannot be resumed",
		"start at the end",
		"do not redo work",
	} {
		if !strings.Contains(low, want) {
			t.Errorf("seed missing %q:\n%s", want, seed)
		}
	}
}

// TestSeedMessageWithoutTranscriptSeedsNothing: with no transcript to
// point at there is no handover to give. Returning a prompt that names
// nothing would look like continuity while delivering none — the caller
// must decide to refuse or cold-start deliberately.
func TestSeedMessageWithoutTranscriptSeedsNothing(t *testing.T) {
	for _, path := range []string{"", "   "} {
		if seed := handover.SeedMessage("grok", "claude", path); seed != "" {
			t.Errorf("path %q produced a seed:\n%s", path, seed)
		}
	}
}

// TestTranscriptFormatDescribesTheSourceBackend: the successor is opening
// a file written by a different tool, so the seed says which shape it is.
func TestTranscriptFormatDescribesTheSourceBackend(t *testing.T) {
	if got := handover.TranscriptFormat("grok"); !strings.Contains(got, "Grok") {
		t.Errorf("grok format = %q", got)
	}
	if got := handover.TranscriptFormat("claude"); !strings.Contains(got, "Claude") {
		t.Errorf("claude format = %q", got)
	}
	// An unknown provider still describes the encoding rather than lying.
	if got := handover.TranscriptFormat("bedrock"); !strings.Contains(got, "JSONL") {
		t.Errorf("unknown provider format = %q", got)
	}
}

// TestPendingUsable: a record is usable exactly while it points somewhere
// and has not already been delivered.
func TestPendingUsable(t *testing.T) {
	base := handover.Pending{Agent: "jevons", From: "grok", To: "claude", TranscriptPath: "/tmp/t.jsonl"}
	if !base.Usable() {
		t.Error("fresh record with a path is not usable")
	}
	delivered := base
	delivered.Delivered = true
	if delivered.Usable() {
		t.Error("delivered record is still usable — successor would be seeded twice")
	}
	cold := base
	cold.TranscriptPath = ""
	if cold.Usable() {
		t.Error("record with no transcript is usable")
	}
	if cold.Seed() != "" {
		t.Error("record with no transcript produced a seed")
	}
}

// TestPendingDescribeAdmitsColdSwitch: the owner-facing line must not
// report continuity that did not happen.
func TestPendingDescribeAdmitsColdSwitch(t *testing.T) {
	warm := handover.Pending{Agent: "jevons", From: "grok", To: "claude",
		TranscriptPath: "/x/y/chat_history.jsonl"}.Describe()
	if !strings.Contains(warm, "chat_history.jsonl") || !strings.Contains(warm, "jevons") {
		t.Errorf("warm describe = %q", warm)
	}
	cold := handover.Pending{Agent: "jevons", From: "grok", To: "claude"}.Describe()
	if !strings.Contains(strings.ToUpper(cold), "COLD") {
		t.Errorf("cold switch not admitted: %q", cold)
	}
}

// TestStoreSurvivesCrashBetweenRotationAndLaunch is the durability
// oracle: rotation overwrites the registry's session id, so the pointer
// must already be on disk when the daemon dies mid-switch (🎯T285).
func TestStoreSurvivesCrashBetweenRotationAndLaunch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "handover")
	rec := handover.Pending{
		Agent: "jevons-po", From: "grok", To: "claude",
		OldSessionID:   "019fd13d-e500-7913-b96c-981e50aa2e21",
		TranscriptPath: "/Users/marcelo/.grok/sessions/019fd13d/chat_history.jsonl",
	}
	if err := handover.NewStore(dir).Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// A brand-new Store stands in for the restarted daemon.
	got, ok, err := handover.NewStore(dir).Get("jevons-po")
	if err != nil || !ok {
		t.Fatalf("Get after restart: ok=%v err=%v", ok, err)
	}
	if got.TranscriptPath != rec.TranscriptPath || got.OldSessionID != rec.OldSessionID {
		t.Fatalf("pointer did not survive: %+v", got)
	}
	if got.CreatedAt == "" {
		t.Error("CreatedAt was not stamped")
	}
	if !got.Usable() {
		t.Error("recovered record is not usable")
	}
}

// TestStoreMarkDeliveredPreventsDoubleSeeding: a migration resumed after a
// crash must not seed the successor twice.
func TestStoreMarkDeliveredPreventsDoubleSeeding(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "handover")
	s := handover.NewStore(dir)
	if err := s.Put(handover.Pending{Agent: "w", TranscriptPath: "/t.jsonl"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.MarkDelivered("w"); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	got, ok, err := s.Get("w")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if !got.Delivered || got.Usable() {
		t.Fatalf("record still usable after delivery: %+v", got)
	}
	if got.TranscriptPath != "/t.jsonl" {
		t.Fatalf("MarkDelivered altered the pointer: %q", got.TranscriptPath)
	}
}

// TestStoreMissingRecordIsNotAnError: the normal case for an agent that is
// not mid-switch.
func TestStoreMissingRecordIsNotAnError(t *testing.T) {
	s := handover.NewStore(filepath.Join(t.TempDir(), "handover"))
	p, ok, err := s.Get("nobody")
	if err != nil {
		t.Fatalf("Get on empty store: %v", err)
	}
	if ok {
		t.Fatalf("found a record that was never written: %+v", p)
	}
	if err := s.Clear("nobody"); err != nil {
		t.Fatalf("Clear of a missing record: %v", err)
	}
}

// TestStoreCorruptRecordIsLoudNotSilent: an unreadable record must surface
// as an error — treating it as "none" would silently cold-start the
// successor with the pointer sitting right there on disk.
func TestStoreCorruptRecordIsLoudNotSilent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "handover")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "w.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := handover.NewStore(dir).Get("w"); err == nil {
		t.Fatalf("corrupt record reported ok=%v with no error", ok)
	}
}

// TestStoreRefusesPathEscapingNames: a name with a separator must not
// write outside the store.
func TestStoreRefusesPathEscapingNames(t *testing.T) {
	s := handover.NewStore(filepath.Join(t.TempDir(), "handover"))
	for _, name := range []string{"../escape", "a/b", ""} {
		if err := s.Put(handover.Pending{Agent: name, TranscriptPath: "/t.jsonl"}); err == nil {
			t.Errorf("Put accepted agent name %q", name)
		}
	}
}
