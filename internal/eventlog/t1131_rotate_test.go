// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestT1131JournalRotatesAtSizeCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	j.maxSegmentBytes = 180
	j.keepSegments = 3
	for i := 0; i < 6; i++ {
		if err := j.Append(Event{TS: "2026-10-21T00:00:00Z", Source: "server", Component: "agent_lifecycle", Decision: "start", Msg: "hit"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	segs, err := SegmentPaths(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) < 2 {
		t.Fatalf("expected rotation, segments=%v", segs)
	}
	if _, err := os.Stat(segmentPath(path, 1)); err != nil {
		t.Fatalf("missing archived segment: %v", err)
	}
}

func TestT1131CollectWalksRotatedSegments(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "events.jsonl")
	archived := segmentPath(live, 1)
	write := func(path string, n int) {
		var b []byte
		for i := 0; i < n; i++ {
			line := `{"ts":"2026-10-21T00:00:0` + string(rune('0'+i)) + `Z","source":"server","level":"info","component":"agent_lifecycle","decision":"start","msg":"old"}` + "\n"
			b = append(b, line...)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(archived, 3)
	write(live, 2)
	got, err := Collect(live, TailOptions{Limit: 10, Component: "agent_lifecycle", Decision: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("Collect returned %d events across segments, want 5", len(got))
	}
}
