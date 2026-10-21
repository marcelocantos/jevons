// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T1131: Page and Collect apply the same byte prefilter Tail does
// (🎯T936), so the daemon's spawn-order evidence read stops decoding the
// whole journal. The prefilter drops no match: the exact field match still
// decides, and a value that appears only in msg or another field does not
// make a line match.
func TestT1131CollectPrefilterKeepsExactMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	var b strings.Builder
	write := func(ev Event) {
		line, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	want := 0
	for i := 0; i < 600; i++ {
		write(Event{TS: fmt.Sprintf("2026-10-21T00:%02d:%02dZ", i/60, i%60), Component: "other", Decision: "noise", Msg: "start agent_lifecycle named in msg"})
		if i%40 == 0 {
			write(Event{TS: fmt.Sprintf("2026-10-21T01:%02d:00Z", i/40), Component: "agent_lifecycle", Decision: "start", Msg: fmt.Sprint("hit ", i)})
			want++
		}
	}
	write(Event{TS: "2026-10-21T02:00:00Z", Component: "agent_lifecycle", Decision: "stop", Msg: "start of something"})
	b.WriteString("{not json agent_lifecycle start\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Collect(path, TailOptions{Limit: 2000, Component: "agent_lifecycle", Decision: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != want {
		t.Fatalf("Collect returned %d events, want %d", len(got), want)
	}
	for _, ev := range got {
		if ev.Component != "agent_lifecycle" || ev.Decision != "start" || !strings.HasPrefix(ev.Msg, "hit ") {
			t.Fatalf("Collect returned a non-matching event: %+v", ev)
		}
	}
	// Newest first, as before the prefilter.
	if got[0].Msg != "hit 600" && got[0].Msg != fmt.Sprint("hit ", (want-1)*40) {
		t.Fatalf("first event %q is not the newest match", got[0].Msg)
	}
}

// A query value that is not a plain identifier is not used as a raw needle,
// so a line whose JSON escapes it still matches through the decoded check.
func TestT1131PrefilterSkipsNonIdentifierValues(t *testing.T) {
	if n := (Query{Component: "a<b"}).rawNeedles(); len(n) != 0 {
		t.Fatalf("rawNeedles for an escapable value = %q, want none", n)
	}
	if n := (Query{Component: "agent_lifecycle", Decision: "start", Contains: "Hit"}).rawNeedles(); len(n) != 2 {
		t.Fatalf("rawNeedles = %q, want component and decision only", n)
	}
}
