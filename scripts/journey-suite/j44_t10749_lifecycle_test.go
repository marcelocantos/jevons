// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestT10749ControlFactsNeedATerminalReceipt(t *testing.T) {
	open := controlFacts{usage: 1, receipts: []string{"req started"}}
	if open.terminal() {
		t.Fatal("a started receipt is not a finished turn")
	}
	unknown := controlFacts{usage: 1, receipts: []string{"req unknown"}}
	if unknown.terminal() {
		t.Fatal("an unknown receipt is not a finished turn")
	}
	done := controlFacts{usage: 2, receipts: []string{"req completed"}}
	if !done.terminal() {
		t.Fatal("a completed receipt is a finished turn")
	}
	if !done.equal(done) {
		t.Fatal("a fact set must equal itself")
	}
	grew := done
	grew.usage = 3
	if done.equal(grew) {
		t.Fatal("a larger usage count must not compare equal")
	}
}

func TestJ44CanonicalUsageWaitsForPositiveReplicatedCount(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE usage_calls (session_id TEXT)`); err != nil {
		t.Fatal(err)
	}
	// Model the production collector's poll arriving after an initial zero
	// has already held for a settling interval.
	go func() {
		time.Sleep(80 * time.Millisecond)
		_, _ = db.Exec(`INSERT INTO usage_calls (session_id) VALUES ('seat-generation')`)
	}()
	got, err := settledUsageCalls(dir, "seat-generation", 25*time.Millisecond, time.Second)
	if err != nil || got != 1 {
		t.Fatalf("settled count = %d, %v; want 1", got, err)
	}
}

func TestJ44CanonicalUsageNeverTreatsZeroAsSettled(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE usage_calls (session_id TEXT)`); err != nil {
		t.Fatal(err)
	}
	_, err = settledUsageCalls(dir, "missing-generation", 10*time.Millisecond, 70*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "missing-generation") {
		t.Fatalf("missing usage must fail with session: %v", err)
	}
}
