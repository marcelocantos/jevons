// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/discovery"
)

// 🎯T422 clause 7, against the bytes of the session that produced the error it
// names. jevons_transcript_read answered "105 lines but no user turns
// (unrecognized format?)" for a live 286KB conversation, and the cause was the
// same one the send confirmation had: a message accepted behind a live turn is
// replayed as a queued_command attachment and never becomes a user message.
//
// The fixture is turnev's, deliberately not a copy. Two packages asserting
// different things about one session is exactly what this target is about, and
// a duplicated fixture would let them drift into asserting it about two.

func t422Fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "turnev", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// readSession writes body as a Claude session and reads it back the way
// jevons_transcript_read does.
func readSession(t *testing.T, sid string, body []byte) []Turn {
	t.Helper()
	projects := filepath.Join(t.TempDir(), "projects")
	dir := filepath.Join(projects, discovery.EncodeClaudeProject("/tmp/claude-work"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sid+".jsonl"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := NewReaderRoots(discovery.Roots{ClaudeProjects: projects}).Read(sid)
	if err != nil {
		t.Fatalf("a session with content read as unreadable: %v", err)
	}
	turns := make([]Turn, 0, len(raw))
	for _, m := range raw {
		role, _ := m["role"].(string)
		text, _ := m["text"].(string)
		turns = append(turns, Turn{Role: role, Text: text})
	}
	return turns
}

func TestT422ReadRendersTheQueuedAttachmentSession(t *testing.T) {
	fixture := t422Fixture(t, "t422_ac031f05_queue_lifecycle.jsonl")
	payload := string(t422Fixture(t, "t422_ac031f05_queue_lifecycle_payload.txt"))

	// The tail is what identifies this brief among the many the fleet sends —
	// the same discriminator turnev.Needle uses, for the same reason.
	tail := strings.TrimSpace(payload)
	if r := []rune(tail); len(r) > 120 {
		tail = string(r[len(r)-120:])
	}

	if turns := readSession(t, "ac031f05-e7d2-4fc1-a7fb-29c0e828ca87", fixture); len(turns) == 0 {
		t.Fatal("no turns — the empty answer this clause exists to abolish")
	}

	// A fleet worker's session, which is the shape that broke: every prompt it
	// receives carries the standing brief, so the 🎯T329 strict pass filters the
	// conversation down to nothing and the reader used to call the file
	// unreadable. Dropping the fixture's one plain user record leaves exactly
	// that session — prompts only in the queue records.
	rest := strings.SplitN(strings.TrimSpace(string(fixture)), "\n", 2)[1]
	turns := readSession(t, "ac031f05-e7d2-4fc1-a7fb-29c0e828ca88", []byte(rest+"\n"))
	var found bool
	for _, tr := range turns {
		if tr.Role == "user" && strings.Contains(tr.Text, tail) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the queued prompt is the only prompt this session has, and it is not in the conversation: %+v", turns)
	}
}
