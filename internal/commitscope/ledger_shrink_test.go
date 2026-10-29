// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/commitscope"
)

func TestLedgerShrinkRefusalBlocksMassDrop(t *testing.T) {
	head := []byte("targets:\n  T1: {name: a}\n  T2: {name: b}\n  T3: {name: c}\n  T4: {name: d}\n  T5: {name: e}\n  T6: {name: f}\n  T7: {name: g}\n")
	staged := []byte("targets:\n  T1: {name: a}\n")
	refuse, msg := commitscope.LedgerShrinkRefusal(head, staged)
	if !refuse || !strings.Contains(msg, "drop 6") {
		t.Fatalf("want refusal on 6-row drop, got refuse=%v msg=%q", refuse, msg)
	}
	if !strings.Contains(msg, "T2") || !strings.Contains(msg, "Lost target rows") {
		t.Fatalf("refusal should name lost rows: %q", msg)
	}
}

func TestLedgerShrinkMergeParentsRefusesSecondParent(t *testing.T) {
	parent1 := []byte("targets:\n  T1: {name: a}\n  T2: {name: b}\n  T3: {name: c}\n  T4: {name: d}\n  T5: {name: e}\n  T6: {name: f}\n  T7: {name: g}\n  T8: {name: h}\n")
	parent2 := parent1
	staged := []byte("targets:\n  T1: {name: a}\n")
	refuse, msg := commitscope.LedgerShrinkMergeParentsRefusal([][]byte{parent1, parent2}, staged)
	if !refuse || !strings.Contains(msg, "merge parent") {
		t.Fatalf("want merge-parent refusal, got refuse=%v msg=%q", refuse, msg)
	}
}

func TestLedgerShrinkRefusalAllowsSmallDrop(t *testing.T) {
	head := []byte("targets:\n  T1: {name: a}\n  T2: {name: b}\n  T3: {name: c}\n")
	staged := []byte("targets:\n  T1: {name: a}\n")
	refuse, _ := commitscope.LedgerShrinkRefusal(head, staged)
	if refuse {
		t.Fatal("small ledger shrink should be allowed")
	}
}
