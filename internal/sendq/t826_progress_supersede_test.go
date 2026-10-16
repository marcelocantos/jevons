// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func t826Progress(worker string, n int) string {
	return fmt.Sprintf("[Agent %s responded] report_id=20260922T%02d0000Z-prog\n%s still running, waiting on the monitor (round %d)",
		worker, n, worker, n)
}

func t826Terminal(worker string) string {
	return fmt.Sprintf("[Agent %s responded] report_id=20260922T990000Z-fin\n"+
		"```jevons\njevons: kind finish-report\njevons: target T826\njevons: silent-ledger none\n```\n\nDone on T826.", worker)
}

func t826GateReport(worker string) string {
	return fmt.Sprintf("[Agent %s responded] report_id=20260922T880000Z-gate\n"+
		"Gate still running mid-work.\n  GATE make-test-go exit=0 GREEN id=9f13c0a2 out=6b1d9e4f2a01 dur=1.0s\n", worker)
}

func t826AskReport(worker string) string {
	return fmt.Sprintf("[Agent %s responded] report_id=20260922T770000Z-ask\n"+
		"## 3. Asks\n\n- MatchTurn needs an owner decision before I go further.\n", worker)
}

func TestT826NProgressReportsQueueAsOne(t *testing.T) {
	s := NewStore("")
	const po, worker = "jevons-po", "jv-t826-w1"
	at := time.Now().Add(-time.Hour)
	var last Entry
	for i := 0; i < 8; i++ {
		at = at.Add(time.Minute)
		e, depth, replaced, err := s.AppendSuperseding(po, t826Progress(worker, i), at,
			SameWorkerSupersedableProgress(t826Progress(worker, i)))
		if err != nil {
			t.Fatal(err)
		}
		last = e
		if i == 0 {
			if depth != 1 || replaced != 0 {
				t.Fatalf("first: depth=%d replaced=%d", depth, replaced)
			}
			continue
		}
		if depth != 1 {
			t.Fatalf("round %d: depth=%d, want 1 pending progress", i, depth)
		}
		if replaced != 1 {
			t.Fatalf("round %d: replaced=%d, want 1", i, replaced)
		}
	}
	entries, err := s.Snapshot(po)
	if err != nil || len(entries) != 1 {
		t.Fatalf("snapshot: %v %+v", err, entries)
	}
	if entries[0].ID != last.ID || !strings.Contains(entries[0].Text, "round 7") {
		t.Fatalf("held progress is not the newest: %+v", entries[0])
	}
	if strings.Contains(entries[0].Text, "round 0") {
		t.Fatal("stale progress survived")
	}
}

func TestT826TerminalBetweenProgressIsKept(t *testing.T) {
	s := NewStore("")
	const po, worker = "jevons-po", "jv-t826-w2"
	at := time.Now()
	appendAt := func(text string) {
		at = at.Add(time.Second)
		if _, _, _, err := s.AppendSuperseding(po, text, at, SameWorkerSupersedableProgress(text)); err != nil {
			t.Fatal(err)
		}
	}
	appendAt(t826Progress(worker, 0))
	appendAt(t826Terminal(worker))
	appendAt(t826Progress(worker, 1))
	appendAt(t826Progress(worker, 2))

	entries, err := s.Snapshot(po)
	if err != nil {
		t.Fatal(err)
	}
	// Terminal kept + newest progress = 2.
	if len(entries) != 2 {
		t.Fatalf("depth=%d want 2 (terminal + newest progress); texts=%v", len(entries), texts(entries))
	}
	var sawTerm, sawProg bool
	for _, e := range entries {
		if strings.Contains(e.Text, "finish-report") {
			sawTerm = true
		}
		if strings.Contains(e.Text, "round 2") {
			sawProg = true
		}
		if strings.Contains(e.Text, "round 0") || strings.Contains(e.Text, "round 1") {
			t.Fatalf("stale progress kept: %q", e.Text)
		}
	}
	if !sawTerm || !sawProg {
		t.Fatalf("missing terminal or newest progress: %v", texts(entries))
	}
}

func TestT826GateAndAskReportsAreNeverDropped(t *testing.T) {
	s := NewStore("")
	const po, worker = "jevons-po", "jv-t826-w3"
	at := time.Now()
	appendAt := func(text string) {
		at = at.Add(time.Second)
		if _, _, _, err := s.AppendSuperseding(po, text, at, SameWorkerSupersedableProgress(text)); err != nil {
			t.Fatal(err)
		}
	}
	appendAt(t826Progress(worker, 0))
	appendAt(t826GateReport(worker))
	appendAt(t826AskReport(worker))
	appendAt(t826Progress(worker, 1))
	appendAt(t826Progress(worker, 2))

	entries, err := s.Snapshot(po)
	if err != nil {
		t.Fatal(err)
	}
	// gate + ask + newest progress = 3 (first progress superseded).
	if len(entries) != 3 {
		t.Fatalf("depth=%d want 3; texts=%v", len(entries), texts(entries))
	}
	joined := strings.Join(texts(entries), "\n---\n")
	if !strings.Contains(joined, "GATE make-test-go") {
		t.Fatal("GATE-carrying report was dropped")
	}
	if !strings.Contains(joined, "## 3. Asks") {
		t.Fatal("ask-carrying report was dropped")
	}
	if !strings.Contains(joined, "round 2") {
		t.Fatal("newest progress missing")
	}
	if strings.Contains(joined, "round 0") {
		t.Fatal("stale progress survived alongside keep-worthy reports")
	}
}

func TestT826DigestCollapsesProgressStatesCount(t *testing.T) {
	const po, worker = "jevons-po", "jv-t826-w4"
	at := time.Now().Add(-time.Hour)
	var entries []Entry
	add := func(text string) {
		at = at.Add(time.Minute)
		entries = append(entries, Entry{ID: NewID(), Text: text, EnqueuedAt: at})
	}
	for i := 0; i < 5; i++ {
		add(t826Progress(worker, i))
	}
	add(t826Terminal(worker))
	add(t826GateReport(worker))
	add(t826Progress(worker, 9))

	digest := BuildDigest(po, entries)
	if !strings.Contains(digest, "finish-report") {
		t.Fatalf("digest dropped terminal:\n%s", digest)
	}
	if !strings.Contains(digest, "GATE make-test-go") {
		t.Fatalf("digest dropped GATE report:\n%s", digest)
	}
	if !strings.Contains(digest, "round 9") {
		t.Fatalf("digest lost newest progress:\n%s", digest)
	}
	// Five early progress + one later progress → 5 superseded (keep newest of 6 progress).
	if !strings.Contains(digest, "superseded progress from "+worker+": 5 earlier non-terminal report(s)") {
		t.Fatalf("digest does not state per-worker superseded count:\n%s", digest)
	}
	if strings.Contains(digest, "round 0") || strings.Contains(digest, "round 3") {
		t.Fatalf("stale progress body survived in digest:\n%s", digest)
	}
}

func TestT826OtherWorkersDoNotCrossSupersede(t *testing.T) {
	s := NewStore("")
	const po = "jevons-po"
	at := time.Now()
	for _, w := range []string{"w-a", "w-b"} {
		for i := 0; i < 3; i++ {
			at = at.Add(time.Second)
			text := t826Progress(w, i)
			if _, _, _, err := s.AppendSuperseding(po, text, at, SameWorkerSupersedableProgress(text)); err != nil {
				t.Fatal(err)
			}
		}
	}
	entries, _ := s.Snapshot(po)
	if len(entries) != 2 {
		t.Fatalf("depth=%d want 2 (one per worker)", len(entries))
	}
	joined := strings.Join(texts(entries), "\n")
	if !strings.Contains(joined, "w-a still running") || !strings.Contains(joined, "w-b still running") {
		t.Fatalf("lost a worker: %v", texts(entries))
	}
}

func texts(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Text
	}
	return out
}

func TestT826ClassifierPinsKeepShapes(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"progress", t826Progress("w", 1), true},
		{"terminal", t826Terminal("w"), false},
		{"gate", t826GateReport("w"), false},
		{"ask", t826AskReport("w"), false},
		{"refusal", "[Agent w responded] report_id=x\nprovider refused the submit", false},
		{"freeform", "SUPERVISOR: hello", false},
	}
	for _, tc := range cases {
		if got := IsSupersedableProgressReport(tc.text); got != tc.want {
			t.Errorf("%s: IsSupersedableProgressReport=%v want %v", tc.name, got, tc.want)
		}
	}
}
