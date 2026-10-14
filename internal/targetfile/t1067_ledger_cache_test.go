// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"fmt"
	"strings"
	"testing"
)

// 🎯T1067: a ledger is parsed once per distinct content. Repeat lookups on
// the same bytes do not re-parse, and an edited ledger is seen at once.
func TestT1067LedgerLookupsShareOneParse(t *testing.T) {
	var b strings.Builder
	b.WriteString("schema_version: 5\ntargets:\n")
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&b, "  T%d:\n    name: target %d with a long enough name to cost something\n    status: identified\n", i, i)
	}
	b.WriteString("  T9999:\n    name: parked\n    status: identified\n    owned_by:\n      owner: marcelo\n      reason: mine\n")
	data := []byte(b.String())

	if st, ok := LookupTargetStatus(data, "T42"); !ok || st != "identified" {
		t.Fatalf("status T42 = %q %v", st, ok)
	}
	if owner, ok := LookupTargetOwner(data, "T9999"); !ok || owner != "marcelo" {
		t.Fatalf("owner T9999 = %q %v", owner, ok)
	}
	allocs := testing.AllocsPerRun(50, func() {
		LookupTargetStatus(data, "T1500")
		LookupTargetOwner(data, "T9999")
	})
	if allocs > 10 {
		t.Fatalf("a repeat lookup made %.0f allocations; the ledger is being re-parsed", allocs)
	}

	edited := []byte(strings.Replace(string(data), "  T42:\n    name: target 42 with a long enough name to cost something\n    status: identified\n",
		"  T42:\n    name: target 42 with a long enough name to cost something\n    status: achieved\n", 1))
	if st, ok := LookupTargetStatus(edited, "T42"); !ok || st != "achieved" {
		t.Fatalf("after an edit, status T42 = %q %v; want achieved", st, ok)
	}
	if st, _ := LookupTargetStatus([]byte("not: [valid"), "T42"); st != "" {
		t.Fatalf("an unparsable ledger answered %q", st)
	}
}
