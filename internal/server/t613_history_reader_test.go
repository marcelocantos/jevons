// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/statedb"
)

// 🎯T613: /api/history must read statedb once import-once JSONL froze, not
// progress frames appended to the archive file after the last real turn.

func t613UserLine(text string, ts time.Time) string {
	return `{"type":"user","timestamp":"` + ts.Format(time.RFC3339) +
		`","message":{"role":"user","content":[{"type":"text","text":"` + text + `"}]}}`
}

func t613ProgressLine(ts time.Time) string {
	return `{"type":"progress","timestamp":"` + ts.Format(time.RFC3339) +
		`","progress_type":"tool_use"}`
}

func TestT613HandleHistoryReadsStatedbNotFrozenJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog", "jevons.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 8, 26, 8, 44, 0, 0, time.UTC)
	now := time.Now().UTC().Truncate(time.Second)
	frozen := t613UserLine("frozen-in-jsonl-only", old) + "\n" +
		t613ProgressLine(now) + "\n"
	if err := os.WriteFile(path, []byte(frozen), 0o644); err != nil {
		t.Fatal(err)
	}
	clog, err := chatlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clog.Close() })

	db, err := statedb.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	live := t613UserLine("live-in-statedb", now)
	if err := db.Upsert("jevons", []statedb.Event{
		{Index: 1, TS: now.Format(time.RFC3339), Type: "user", Body: live},
	}); err != nil {
		t.Fatal(err)
	}

	s := New("test", dir)
	s.overseerName = "jevons"
	s.SetChatLog(clog)
	s.SetStateDB(db)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/history", s.handleHistory)
	req := httptest.NewRequest(http.MethodGet, "/api/history?end=1&limit=10", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Lines []json.RawMessage `json:"lines"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 1 {
		t.Fatalf("total=%d want 1 coalesced turn in statedb", body.Total)
	}
	raw := string(body.Lines[0])
	if strings.Contains(raw, "frozen-in-jsonl-only") {
		t.Fatalf("served frozen JSONL turn: %s", raw)
	}
	if !strings.Contains(raw, "live-in-statedb") {
		t.Fatalf("missing live statedb turn: %s", raw)
	}
}

// Drift oracle: overseer /api/history and /ws/chat replay must go through the
// canonical-history gate — not chatlog.ReadRange on the archive while statedb
// has rows.
func TestT613OverseerHistoryReadersUseCanonicalGate(t *testing.T) {
	src, err := os.ReadFile("chat.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if !strings.Contains(text, "overseerCanonicalHistory()") {
		t.Fatal("chat.go must call overseerCanonicalHistory for history reads")
	}
	if !strings.Contains(text, "overseerHistoryReadRange") || !strings.Contains(text, "overseerReplayTailSealed") {
		t.Fatal("chat.go must route statedb history through overseer_history helpers")
	}
}
