// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/statedb"
)

// 🎯T825: the seed lands after rows the live daemon already stored, and only
// the appended journal bytes are folded.
func TestSeedThroughStoreAppendsAfterLiveRows(t *testing.T) {
	dir := t.TempDir()
	db, err := statedb.Open(statedb.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Upsert("jevons", []statedb.Event{{Index: 1, Type: "user", Body: `{"type":"user"}`}}); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	journal := filepath.Join(dir, "chatlog", "jevons.jsonl")
	if err := os.MkdirAll(filepath.Dir(journal), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, []byte(`{"type":"user","message":{"role":"user","content":"OLD-LIVE"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = seedThroughStore(dir, "jevons", journal, func() error {
		f, err := os.OpenFile(journal, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.WriteString(`{"type":"user","message":{"role":"user","content":"SEEDED"}}` + "\n")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err = statedb.Open(statedb.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	evs, err := db.Range("jevons", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Index != 1 || evs[1].Index != 2 {
		t.Fatalf("rows: %+v", evs)
	}
	if !strings.Contains(evs[1].Body, "SEEDED") || strings.Contains(evs[1].Body, "OLD-LIVE") {
		t.Fatalf("seed row wrong: %s", evs[1].Body)
	}
}
