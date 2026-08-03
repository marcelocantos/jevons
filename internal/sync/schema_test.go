// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"strings"
	"testing"
)

func TestSyncSchemaDDLCoversOwnershipPartition(t *testing.T) {
	joined := strings.Join(SyncSchemaDDL, "\n")
	for _, table := range append(append([]string{}, ServerOwnedTables...), ClientOwnedTables...) {
		if !strings.Contains(joined, "TABLE IF NOT EXISTS "+table) &&
			!strings.Contains(joined, "TABLE IF NOT EXISTS "+table+" ") &&
			!strings.Contains(joined, table+" (") {
			// CREATE TABLE lines embed the name after IF NOT EXISTS.
			needle := "EXISTS " + table
			if !strings.Contains(joined, needle) {
				t.Fatalf("schema missing table %q", table)
			}
		}
	}
	// server_state seed required for singleton state writes.
	if !strings.Contains(joined, "INSERT OR IGNORE INTO server_state") {
		t.Fatal("schema missing server_state seed")
	}
}

func TestOwnershipDisjoint(t *testing.T) {
	server := map[string]bool{}
	for _, tname := range ServerOwnedTables {
		server[tname] = true
	}
	for _, tname := range ClientOwnedTables {
		if server[tname] {
			t.Fatalf("table %q claimed by both server and client", tname)
		}
	}
}
