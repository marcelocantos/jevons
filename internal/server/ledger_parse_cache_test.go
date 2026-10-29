// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"os"
	"path/filepath"
	"testing"
)

// The ledger parse is memoized by file identity: a repeat read returns the
// cached parse, and an atomic rewrite (bullseye writes by rename) is parsed
// afresh.
func TestReadBullseyeLedgerMemoizesUntilTheFileChanges(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ledger.yaml")
	write := func(body string) {
		t.Helper()
		tmp := p + ".tmp"
		if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, p); err != nil {
			t.Fatal(err)
		}
	}
	write("targets:\n  T1:\n    name: one\n    status: identified\n")
	for range 2 {
		doc, _, err := readBullseyeLedger(p)
		if err != nil {
			t.Fatal(err)
		}
		if doc.Targets["T1"].Name != "one" {
			t.Fatalf("read: %+v", doc.Targets["T1"])
		}
	}
	write("targets:\n  T1:\n    name: renamed\n    status: identified\n  T2:\n    name: two\n    status: identified\n")
	doc, _, err := readBullseyeLedger(p)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Targets["T1"].Name != "renamed" || doc.Targets["T2"].Name != "two" {
		t.Fatalf("rewrite not picked up: %+v", doc.Targets)
	}
}
