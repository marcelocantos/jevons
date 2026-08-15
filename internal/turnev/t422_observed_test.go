// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turnev

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T422 clause 8: the four sessions that were read wrongly on 2026-08-10,
// replayed from their own bytes rather than from a synthetic reconstruction.
//
// Each fixture is a trimmed excerpt of a real session JSONL under
// ~/.claude/projects, with the payload it carries kept verbatim in a paired
// .txt so the needle under test is the text that was actually sent — a fixture
// whose payload was retyped from memory tests the retyping.
//
// The synthetic tests above pin the decoder's rules; these pin the rules
// against the shapes the CLI really writes, which is where every wrong answer
// that morning came from: nobody's model of the file was wrong in the
// abstract, it was wrong about which records the CLI emits for a message
// accepted behind a live turn.

func fixture(t *testing.T, name string) (path, payload string) {
	t.Helper()
	path = filepath.Join("testdata", name+".jsonl")
	raw, err := os.ReadFile(filepath.Join("testdata", name+"_payload.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return path, string(raw)
}

func TestT422ObservedSessionsDecodeAsWhatHappened(t *testing.T) {
	for _, tc := range []struct {
		name    string
		want    Fate
		because string
	}{{
		name: "t422_ac031f05_queue_lifecycle",
		want: FateEnteredTurn,
		because: "the whole queued lifecycle — enqueue, remove, queued_command " +
			"attachment. No user message carries this payload and none ever will; " +
			"the queue records are the only evidence of a delivery that had " +
			"already been taken into the receiver's turn when it was reported absent.",
	}, {
		name: "t422_b365c048_post_window",
		want: FateUserMessage,
		because: "the brief to jv-t374 that landed as an authored user message " +
			"after the 45s window had closed. The decoder has no window to close: " +
			"it reads the region it was given to its end, so late is still landed.",
	}, {
		name: "t422_pane_capture_tool_result",
		want: FateUnseen,
		because: "the payload appears only inside a tool_result — an agent " +
			"capturing its own composer while investigating a stuck send. Bytes " +
			"in the file are not delivery; authored content is.",
	}, {
		name: "t422_prefix_collision",
		want: FateUnseen,
		because: "a different message sharing 631 characters of standing brief " +
			"with the payload. Any prefix pass shorter than that scores this as " +
			"landed; the needle is the distinguishing tail, so it does not.",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			path, payload := fixture(t, tc.name)
			// from=0, hadTranscript=false: the whole file, read at any later
			// time. Confirmation is not a race against a timer (clause 5).
			if got := Scan(path, 0, false, Needle(payload)); got != tc.want {
				t.Fatalf("fate=%s want %s — %s", got, tc.want, tc.because)
			}
		})
	}
}

// The two readings that were live before this decoder, run against the same
// bytes, so the fixtures are demonstrably red on the pre-fix predicates rather
// than merely green on the new one. A regression fixture that passes both ways
// is not a regression fixture.
func TestT422PreFixReadingsAreWrongOnTheseSameFiles(t *testing.T) {
	t.Run("user messages only reports absent for the queued delivery", func(t *testing.T) {
		path, payload := fixture(t, "t422_ac031f05_queue_lifecycle")
		if userMessageOnly(t, path, Needle(payload)) {
			t.Fatal("fixture no longer carries the payload solely in queue records")
		}
		if got := Scan(path, 0, false, Needle(payload)); !got.Delivered() {
			t.Fatalf("fate=%s — the decoder inherited the defect it exists to fix", got)
		}
	})

	t.Run("a 120-character prefix scores the collision as landed", func(t *testing.T) {
		path, payload := fixture(t, "t422_prefix_collision")
		if !prefixPass(t, path, payload, 120) {
			t.Fatal("fixture no longer collides on a 120-character prefix; it has stopped testing clause 4")
		}
		if got := Scan(path, 0, false, Needle(payload)); got.Delivered() {
			t.Fatalf("fate=%s — a message that was never sent was confirmed by boilerplate it shares", got)
		}
	})
}

// userMessageOnly is 🎯T416's reading: authored user messages, nothing else.
func userMessageOnly(t *testing.T, path, needle string) bool {
	t.Helper()
	for _, line := range lines(t, path) {
		rec, ok := Decode([]byte(line))
		if ok && rec.Kind == KindUserMessage && strings.Contains(Normalize(rec.Text), needle) {
			return true
		}
	}
	return false
}

// prefixPass is the check that scored 25 boilerplate payloads as landed: the
// payload's first n characters, looked for in a record's text. Only the needle
// differs from the shipped scan — which is the point, since the standing brief
// the daemon prepends is the first n characters of nearly every payload the
// fleet sends.
func prefixPass(t *testing.T, path, payload string, n int) bool {
	t.Helper()
	norm := []rune(Normalize(payload))
	if len(norm) > n {
		norm = norm[:n]
	}
	prefix := string(norm)
	for _, line := range lines(t, path) {
		rec, ok := Decode([]byte(line))
		if ok && rec.Delivers() && strings.Contains(Normalize(rec.Payload()), prefix) {
			return true
		}
	}
	return false
}

func lines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}
