// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/muxwin"
	"github.com/marcelocantos/jevons/internal/statedb"
)

const t825TallLine = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"TALL-CLIP-BODY"}],"stop_reason":"end_turn"}}`

// 🎯T825: the daemon imports the overseer journal once, at boot. A journey
// seed that lands afterwards must reach the mux window the React pane reads
// by going through the store; appending to the JSONL alone is never read.
func t825Server(t *testing.T) (*Server, *chatlog.Log, *statedb.Store) {
	t.Helper()
	dir := t.TempDir()
	clog, err := chatlog.Open(filepath.Join(dir, "chatlog", "jevons.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clog.Close() })
	db, err := statedb.Open(statedb.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := New("test", dir)
	s.overseerName = "jevons"
	s.SetChatLog(clog)
	s.SetStateDB(db)
	s.mux = newMuxHub()
	s.ImportTranscripts() // boot import of an empty journal: initialises the agent
	return s, clog, db
}

func TestT825LateJournalAppendIsNotServed(t *testing.T) {
	s, clog, _ := t825Server(t)
	if err := clog.Append(t825TallLine); err != nil {
		t.Fatal(err)
	}
	for _, ev := range s.muxCoalesced("jevons", true) {
		if strings.Contains(string(ev.Body), "TALL-CLIP-BODY") {
			t.Fatal("a journal appended after the boot import was served; the T825 premise no longer holds")
		}
	}
}

func TestT825StoreSeedAfterBootIsServedByMuxWindow(t *testing.T) {
	s, _, db := t825Server(t)
	evs := muxwin.EventsFromLines([]string{t825TallLine})
	if len(evs) != 1 {
		t.Fatalf("fold: %d events", len(evs))
	}
	n, err := db.N("jevons")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Upsert("jevons", []statedb.Event{{
		Index: n + 1, TS: evs[0].TS, Type: evs[0].Type, Kind: int(evs[0].Kind), Body: string(evs[0].Body),
	}}); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, ev := range s.muxCoalesced("jevons", true) {
		if strings.Contains(string(ev.Body), "TALL-CLIP-BODY") {
			found = true
		}
	}
	if !found {
		t.Fatal("store-seeded tall message not served by the mux window")
	}
}
