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

func TestJ32SeedsTallHistoryThroughCanonicalStore(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "chatlog", "jevons.jsonl")
	if err := seedThroughStore(dir, overseerName, journal, func() error { return seedJ32Journal(journal) }); err != nil {
		t.Fatal(err)
	}
	db, err := statedb.Open(statedb.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Range(overseerName, 1, 2*j32SeedTurns+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2*j32SeedTurns {
		t.Fatalf("seeded %d rows, want %d", len(rows), 2*j32SeedTurns)
	}
	if rows[0].Type != "user" || rows[1].Type != "assistant" || !strings.Contains(rows[len(rows)-1].Body, "J32 history 27 line 31") {
		t.Fatalf("seed mix/tail: first=%s second=%s tail=%s", rows[0].Type, rows[1].Type, rows[len(rows)-1].Body)
	}
	raw, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 100000 {
		t.Fatalf("seed has only %d bytes of painted ink", len(raw))
	}
}
