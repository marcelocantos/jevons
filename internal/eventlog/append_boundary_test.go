// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenPreservesUnterminatedTail(t *testing.T) {
	for _, tail := range []string{`{"partial":`, `{"complete":true}`} {
		t.Run(tail, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.jsonl")
			before := "{\"decision\":\"older\"}\n" + tail
			if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
				t.Fatal(err)
			}
			if j, err := Open(path); err == nil || j != nil || !strings.Contains(err.Error(), "unterminated") {
				t.Fatalf("accepted unsafe append boundary: journal=%v err=%v", j, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != before {
				t.Fatalf("existing bytes changed: %q err=%v", after, err)
			}
		})
	}
}

func TestOpenAcceptsEmptyAndTerminatedBoundaries(t *testing.T) {
	// An earlier malformed line is deliberately not whole-log validation.
	for _, before := range []string{"", "{\"decision\":\"older\"}\n", "malformed earlier record\n"} {
		path := filepath.Join(t.TempDir(), "events.jsonl")
		if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
			t.Fatal(err)
		}
		j, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := j.Append(Event{Decision: "new"}); err != nil {
			t.Fatal(err)
		}
		if err := j.Close(); err != nil {
			t.Fatal(err)
		}
		events, err := Tail(path, TailOptions{Decision: "new"})
		if err != nil || len(events) != 1 {
			t.Fatalf("new append unreadable: %+v err=%v", events, err)
		}
	}
}

func TestAppendReaderSizeBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	ev := Event{TS: "2026-10-16T01:00:00Z", Source: "server", Level: "info", Decision: "receipt"}
	encoded, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	ev.Msg = strings.Repeat("x", maxEventLineBytes-len(encoded)-1)
	if err := j.Append(ev); err != nil {
		t.Fatalf("reader-sized event refused: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil || len(before) != maxEventLineBytes {
		t.Fatalf("wrong boundary fixture: bytes=%d err=%v", len(before), err)
	}
	ev.Msg += "x"
	if err := j.Append(ev); err == nil || !strings.Contains(err.Error(), "reader limit") {
		t.Fatalf("oversized event accepted: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("oversized event changed journal: err=%v", err)
	}
	if rows, err := Tail(path, TailOptions{}); err != nil || len(rows) != 1 {
		t.Fatalf("Tail cannot read accepted boundary event: count=%d err=%v", len(rows), err)
	}
	if page, err := Page(path, nil, 10, Query{}); err != nil || len(page.Events) != 1 {
		t.Fatalf("Page cannot read accepted boundary event: count=%d err=%v", len(page.Events), err)
	}
	if stream, err := ReadAfter(path, 0, 10, Query{}); err != nil || len(stream.Events) != 1 {
		t.Fatalf("ReadAfter cannot read accepted boundary event: count=%d err=%v", len(stream.Events), err)
	}
	if err := j.Append(Event{Decision: "small-after-rejection"}); err != nil {
		t.Fatalf("size rejection poisoned unwritten journal: %v", err)
	}
}

func TestAppendWriteFailurePoisonsOpenJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// A real descriptor write error follows a partial record. The same open
	// Journal must never attempt another write, even if its descriptor works
	// again; reopening must independently reject the unsafe tail.
	if _, err := j.f.Write([]byte(`{"partial":`)); err != nil {
		t.Fatal(err)
	}
	if err := j.f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(Event{Decision: "write-fails"}); err == nil {
		t.Fatal("closed descriptor write succeeded")
	}
	j.f, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if err := j.Append(Event{Decision: "must-not-concatenate"}); err == nil || !strings.Contains(err.Error(), "previous append failed") {
		t.Fatalf("open instance forgot write failure: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != `{"partial":` {
		t.Fatalf("poisoned append changed torn tail: %q err=%v", got, err)
	}
	if reopened, err := Open(path); err == nil || reopened != nil {
		t.Fatalf("reopen accepted partial tail: journal=%v err=%v", reopened, err)
	}
}
