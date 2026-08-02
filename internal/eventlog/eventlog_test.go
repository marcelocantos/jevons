// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"path/filepath"
	"testing"
)

func TestAppendAndTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "events.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	for i, dec := range []string{"no-match", "match", "enqueue"} {
		ev := Event{
			Source:    "browser",
			Level:     "info",
			Msg:       "decision.route",
			Component: "thread_route",
			Decision:  dec,
			Corr:      "c1",
			Fields:    map[string]any{"i": i},
		}
		if err := j.Append(ev); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	all, err := Tail(path, TailOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("len=%d want 3", len(all))
	}
	// Newest first.
	if all[0].Decision != "enqueue" {
		t.Fatalf("newest decision=%q", all[0].Decision)
	}

	matched, err := Tail(path, TailOptions{Limit: 10, Decision: "match"})
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 1 || matched[0].Decision != "match" {
		t.Fatalf("matched=%+v", matched)
	}

	byComp, err := Tail(path, TailOptions{Component: "thread_route", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(byComp) != 3 {
		t.Fatalf("byComp=%d", len(byComp))
	}
}

func TestTailMissingFile(t *testing.T) {
	got, err := Tail(filepath.Join(t.TempDir(), "nope.jsonl"), TailOptions{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got != nil && len(got) != 0 {
		t.Fatalf("got=%v", got)
	}
}

func TestDefaultPath(t *testing.T) {
	p := DefaultPath("/tmp/jevons-state")
	if p != filepath.Join("/tmp/jevons-state", "logs", "events.jsonl") {
		t.Fatalf("path=%s", p)
	}
}
