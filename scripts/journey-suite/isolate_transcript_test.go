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

func TestT625ReconnectReadsActualOwnerExchange(t *testing.T) {
	const prompt = "Reply with exactly: fresh-reconnect-token"
	const reply = "fresh-reconnect-token"
	dir := t.TempDir()
	if err := assertStoredOwnerRoundTrip(dir, prompt, reply); err == nil {
		t.Fatal("missing database accepted")
	}
	if _, err := os.Stat(statedb.DefaultPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("oracle created missing database: %v", err)
	}
	store, err := statedb.Open(statedb.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	user := statedb.Event{Index: 2, ID: "owner", Type: "user", Body: `{"type":"user","turn_origin":"owner","message":{"content":[{"type":"text","text":"Reply with exactly: fresh-reconnect-token"}]}}`}
	response := statedb.Event{Index: 3, ID: "reply", Type: "assistant", Body: `{"type":"assistant","stream_id":"response","message":{"content":[{"type":"text","text":"fresh-reconnect-token"}],"stop_reason":"end_turn"}}`}
	oldError := statedb.Event{Index: 1, ID: "old-error", Type: "error", Body: `{"type":"error","error":"earlier request timed out"}`}
	if err := store.Upsert(overseerName, []statedb.Event{oldError}); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert("other", []statedb.Event{user, response}); err != nil {
		t.Fatal(err)
	}
	if err := assertStoredOwnerRoundTrip(dir, prompt, reply); err == nil {
		t.Fatal("another agent's exchange accepted")
	}
	if err := store.Upsert(overseerName, []statedb.Event{user}); err != nil {
		t.Fatal(err)
	}
	if err := assertStoredOwnerRoundTrip(dir, prompt, reply); err == nil {
		t.Fatal("owner echo without answer accepted")
	}
	stale := response
	stale.Body = `{"type":"assistant","stream_id":"response","message":{"content":[{"type":"text","text":"old-token"}],"stop_reason":"end_turn"}}`
	if err := store.Upsert(overseerName, []statedb.Event{stale}); err != nil {
		t.Fatal(err)
	}
	if err := assertStoredOwnerRoundTrip(dir, prompt, reply); err == nil {
		t.Fatal("stale answer accepted")
	}
	if err := store.Upsert(overseerName, []statedb.Event{response}); err != nil {
		t.Fatal(err)
	}
	if err := assertStoredOwnerRoundTrip(dir, prompt, reply); err != nil {
		t.Fatal(err)
	}
}
