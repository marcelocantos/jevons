// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/statedb"
)

// 🎯T625.10: the journey reads the store the daemon writes. The peer is the
// observed failure: the turn lives in statedb while the retired
// agent-chatlogs JSONL is a 0-byte file.
func TestAgentTranscriptReadsStatedbNotRetiredJSONL(t *testing.T) {
	dir := t.TempDir()
	db, err := statedb.Open(statedb.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Upsert("worker-a", []statedb.Event{
		{Index: 1, ID: "u1", TS: "2026-09-22T00:00:00Z", Type: "user", Body: `{"type":"user","text":"hi"}`},
		{Index: 2, ID: "a1", TS: "2026-09-22T00:00:01Z", Type: "assistant", Body: `{"type":"assistant","text":"JOURNEY-TX-1"}`},
		{Index: 3, ID: "t1", TS: "2026-09-22T00:00:02Z", Type: "tool_use", Body: `{"type":"tool_use"}`},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	logs := filepath.Join(dir, "agent-chatlogs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "worker-a.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	s := &suite{stateDir: dir}
	p, err := s.agentTranscriptHTTP("worker-a")
	if err != nil {
		t.Fatal(err)
	}
	if got := countTranscriptRole(p, "user"); got != 1 {
		t.Fatalf("user turns = %d, want 1 (store turn invisible)", got)
	}
	if turns, _ := p["turns"].([]any); len(turns) != 2 {
		t.Fatalf("turns = %d, want 2 (tool_use must not count)", len(turns))
	}

	// Control: an agent with no rows stays empty, so the check can fail.
	q, err := s.agentTranscriptHTTP("worker-b")
	if err != nil {
		t.Fatal(err)
	}
	if turns, _ := q["turns"].([]any); len(turns) != 0 || q["empty"] != true {
		t.Fatalf("unknown agent not empty: %v", q)
	}
}
