// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/muxwin"
	"github.com/marcelocantos/jevons/internal/statedb"
)

// t1089Specimen is the event stream behind the child store of the second
// full journey suite on 024bbdf1 (orch-midturn-58826, J38). The worker said
// it would run the first sleep, ran it, was steered the overseer's question,
// answered it, ran the second sleep and said SLEPTTWICE. The store folded
// "Paris" and "SLEPTTWICE" back into row 2, above the tool call and the
// question they follow.
var t1089Specimen = []string{
	`{"type":"user","turn_origin":"agent","timestamp":"2026-10-20T01:13:46.138481Z","message":{"role":"user","content":[{"type":"text","text":"Run exactly ` + "`sleep 30`" + ` with your Bash tool. Then run exactly ` + "`sleep 30`" + ` again. Then reply with exactly SLEPTTWICE."}]}}`,
	`{"type":"assistant","timestamp":"2026-10-20T01:13:48.59285Z","message":{"role":"assistant","content":[{"type":"text","text":"I'll run the first ` + "`sleep 30`" + ` now."}]}}`,
	`{"type":"tool_use","id":"call-f0651629-0","name":"Bash","input":{"command":"sleep 30"},"timestamp":"2026-10-20T01:13:48.647397Z"}`,
	`{"type":"user","turn_origin":"agent","timestamp":"2026-10-20T01:13:56.21421Z","message":{"role":"user","content":[{"type":"text","text":"Overseer here: what is the capital of France? Answer in one word, then carry on."}]}}`,
	`{"type":"assistant","timestamp":"2026-10-20T01:14:18.70Z","message":{"role":"assistant","content":[{"type":"text","text":"Paris"}]}}`,
	`{"type":"tool_use","id":"call-a21ae2a7-1","name":"Bash","input":{"command":"sleep 30"},"timestamp":"2026-10-20T01:14:21.92239Z"}`,
	`{"type":"assistant","timestamp":"2026-10-20T01:14:54.92823Z","message":{"role":"assistant","content":[{"type":"text","text":"SLEPTTWICE"}],"stop_reason":"end_turn"}}`,
	`{"type":"assistant","timestamp":"2026-10-20T01:15:18.265707Z","message":{"role":"assistant","content":[{"type":"text","text":"GOAL_STATUS: complete"}],"stop_reason":"end_turn"}}`,
}

// t1089Want is the specimen in the order it happened: each piece of text
// sits after the tool call or question it follows.
var t1089Want = []string{
	"user:Run exactly",
	"assistant:I'll run the first `sleep 30` now.",
	"tool_use:Bash",
	"user:Overseer here",
	"assistant:Paris",
	"tool_use:Bash",
	"assistant:SLEPTTWICE",
	"assistant:GOAL_STATUS: complete",
}

func t1089Rows(t *testing.T, evs []statedb.Event) []string {
	t.Helper()
	var out []string
	for _, ev := range evs {
		var m map[string]any
		if err := json.Unmarshal([]byte(ev.Body), &m); err != nil {
			t.Fatalf("row %d body: %v", ev.Index, err)
		}
		switch ev.Type {
		case "assistant":
			out = append(out, "assistant:"+statedb.AssistantProse(ev.Type, ev.Body))
		case "tool_use":
			name, _ := m["name"].(string)
			out = append(out, "tool_use:"+name)
		case "user":
			text := proseFromEvent(m)
			if i := strings.IndexAny(text, ":?"); i > 0 && i < 20 {
				text = text[:i]
			}
			out = append(out, "user:"+t1089Head(text))
		default:
			out = append(out, ev.Type)
		}
	}
	return out
}

func t1089Head(text string) string {
	for _, head := range []string{"Run exactly", "Overseer here"} {
		if strings.HasPrefix(text, head) {
			return head
		}
	}
	return text
}

// 🎯T1089: replay the specimen through the canonical store — the live fold
// that writes statedb, and the JSONL import a reload falls back to — and
// read the rows back in index order.
func TestT1089TextAfterToolOrSteerOpensANewRowInOrder(t *testing.T) {
	const name = "orch-midturn-58826"
	dir := t.TempDir()
	db, err := statedb.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := New("test", dir)
	s.overseerName = "jevons"
	s.SetStateDB(db)
	for _, line := range t1089Specimen {
		if !s.muxFanTranscript(name, line) {
			t.Fatalf("line was not stored: %s", line)
		}
	}
	n, err := db.N(name)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Range(name, 1, n+1)
	if err != nil {
		t.Fatal(err)
	}
	if got := t1089Rows(t, rows); strings.Join(got, "\n") != strings.Join(t1089Want, "\n") {
		t.Errorf("live store rows out of order:\n got: %q\nwant: %q", got, t1089Want)
	}

	imported := muxwin.EventsFromLines(t1089Specimen)
	stored := make([]statedb.Event, len(imported))
	for i, ev := range imported {
		stored[i] = muxEventToRow(ev)
	}
	if got := t1089Rows(t, stored); strings.Join(got, "\n") != strings.Join(t1089Want, "\n") {
		t.Errorf("imported rows out of order:\n got: %q\nwant: %q", got, t1089Want)
	}
}

// 🎯T1089, second specimen (gate c3afc8fc, overseer row jevons/430): a
// fleet-health agent_note row came to carry the name and input of a
// concurrent overseer MCP call. A tool stamp belongs on a tool row; a note is
// a different event and keeps its own fields.
func TestT1089ToolStampNeverMergesIntoANote(t *testing.T) {
	dir := t.TempDir()
	db, err := statedb.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := New("test", dir)
	s.overseerName = "jevons"
	s.overseerOwnerTurn = true
	s.SetStateDB(db)
	note := `{"type":"agent_note","text":"[Fleet health] Stalled backlog on \"t952-stall\": 1 message(s) held by the daemon","timestamp":"2026-10-20T00:52:35Z"}`
	if !s.muxFanTranscript("jevons", note) {
		t.Fatal("note was not stored")
	}
	s.ObserveMCPToolCall("jevonsmcp__jevons_sendq_reconcile", map[string]any{"name": "t952-hold"})
	row, ok, err := db.Last("jevons")
	if err != nil || !ok {
		t.Fatalf("no stored row: ok=%v err=%v", ok, err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(row.Body), &m); err != nil {
		t.Fatal(err)
	}
	if _, has := m["name"]; has {
		t.Errorf("note row took a tool call's name: %s", row.Body)
	}
	if _, has := m["input"]; has {
		t.Errorf("note row took a tool call's input: %s", row.Body)
	}

	// The same stamp, persisted as a progress line and replayed on import.
	evs := muxwin.EventsFromLines([]string{note, chatToolStampLine("jevons_agent_list", map[string]any{"query": "running"})})
	if len(evs) != 1 {
		t.Fatalf("import rows=%d want the note alone", len(evs))
	}
	if strings.Contains(string(evs[0].Body), "jevons_agent_list") || strings.Contains(string(evs[0].Body), `"input"`) {
		t.Errorf("imported note row took a tool call's fields: %s", evs[0].Body)
	}
}

// Stamps that find no generic tool row are bounded, oldest dropped first:
// notes no longer absorb them (🎯T1089).
func TestT1089QueuedToolStampsAreBounded(t *testing.T) {
	s := &Server{mux: newMuxHub(), overseerName: "jevons", overseerStreamID: "s1"}
	for i := 0; i < maxQueuedToolStamps+5; i++ {
		s.ObserveMCPToolCall("jevons_agent_list", map[string]any{"i": i})
	}
	q := s.mux.stamps["jevons"]
	if len(q) != maxQueuedToolStamps {
		t.Fatalf("queued=%d want %d", len(q), maxQueuedToolStamps)
	}
	if got := q[0].Input["i"]; got != 5 {
		t.Fatalf("oldest kept=%v want 5", got)
	}
}
