// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"strings"
	"testing"
)

func TestUnionPreservingWorkingDivergenceKeepsUncommittedAchieve(t *testing.T) {
	head := `schema_version: 1
targets:
  T1: {name: a, status: identified}
`
	working := `schema_version: 1
targets:
  T1: {name: a, status: identified}
  T1147: {name: x, status: achieved}
`
	candidate := head

	out, err := UnionPreservingWorkingDivergence([]byte(head), []byte(working), []byte(candidate))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "T1147:") {
		t.Fatalf("preserve dropped uncommitted achieve:\n%s", out)
	}
	if !strings.Contains(string(out), "achieved") {
		t.Fatalf("achieved status lost:\n%s", out)
	}
}

func TestWouldDropWorkingDivergenceDetectsHeadRestore(t *testing.T) {
	head := `schema_version: 1
targets:
  T1: {name: a, status: identified}
`
	working := `schema_version: 1
targets:
  T1: {name: a, status: identified}
  T1147: {name: x, status: achieved}
`
	drop, ids, err := WouldDropWorkingDivergence([]byte(head), []byte(working), []byte(head))
	if err != nil {
		t.Fatal(err)
	}
	if !drop || len(ids) != 1 || ids[0] != "T1147" {
		t.Fatalf("drop=%v ids=%v", drop, ids)
	}
}

func TestWouldDropStaleWorkingTreeMissingHeadRows(t *testing.T) {
	head := `schema_version: 1
targets:
  T1: {name: a, status: identified}
`
	working := `targets: {}
`
	drop, ids, err := WouldDropWorkingDivergence([]byte(head), []byte(working), []byte(head))
	if err != nil {
		t.Fatal(err)
	}
	if drop {
		t.Fatalf("stale mirror should not count as drop; ids=%v", ids)
	}
}
