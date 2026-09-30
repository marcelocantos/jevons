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

// 🎯T936: Tail's byte prefilter drops no match. Only lines carrying the
// wanted component, decision and source values are decoded, and the exact
// field match still decides: a value that appears elsewhere in a line (in
// msg, or another field) does not make it match.
func TestT936TailPrefilterKeepsExactMatches(t *testing.T) {
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
	for i := 0; i < 500; i++ {
		write(Event{TS: fmt.Sprintf("2026-09-30T00:00:%02dZ", i%60), Component: "other", Decision: "noise", Msg: "start agent_lifecycle mentioned in msg"})
		if i%50 == 0 {
			write(Event{TS: fmt.Sprintf("2026-09-30T01:00:%02dZ", i/50), Component: "agent_lifecycle", Decision: "start", Msg: fmt.Sprint("hit ", i)})
		}
	}
	write(Event{TS: "2026-09-30T02:00:00Z", Component: "agent_lifecycle", Decision: "stop", Msg: "start of something"})
	b.WriteString("{not json agent_lifecycle start\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Tail(path, TailOptions{Limit: 100, Component: "agent_lifecycle", Decision: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 {
		t.Fatalf("matches = %d, want 10", len(got))
	}
	if got[0].Msg != "hit 450" || got[9].Msg != "hit 0" {
		t.Fatalf("order or content: first %q last %q", got[0].Msg, got[9].Msg)
	}
	for _, ev := range got {
		if ev.Component != "agent_lifecycle" || ev.Decision != "start" {
			t.Fatalf("a non-match got through: %+v", ev)
		}
	}
}
