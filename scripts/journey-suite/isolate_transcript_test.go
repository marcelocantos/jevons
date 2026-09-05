// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"testing"

	"github.com/marcelocantos/jevons/internal/statedb"
)

func TestIsolationReadsProductStoreWithoutCreatingEvidence(t *testing.T) {
	dir := t.TempDir()
	if err := assertIsolateTranscript(dir); err == nil {
		t.Fatal("missing database accepted")
	}
	if _, err := os.Stat(statedb.DefaultPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("check created a database: %v", err)
	}
	store, err := statedb.Open(statedb.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := assertIsolateTranscript(dir); err == nil {
		t.Fatal("empty store accepted")
	}
	if err := store.Upsert("other", []statedb.Event{{Index: 1, ID: "e:1", Type: "user", Body: `{"type":"user"}`}}); err != nil {
		t.Fatal(err)
	}
	if err := assertIsolateTranscript(dir); err == nil {
		t.Fatal("unrelated agent history accepted")
	}
	if err := store.Upsert(overseerName, []statedb.Event{{Index: 1, ID: "e:1", Type: "user", Body: `{"type":"user"}`}}); err != nil {
		t.Fatal(err)
	}
	if err := assertIsolateTranscript(dir); err != nil {
		t.Fatal(err)
	}
}
