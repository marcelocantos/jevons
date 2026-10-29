// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervisormem

import (
	"strings"
	"testing"
)

func TestT704AppendRollingCreatesFileAndReadsBack(t *testing.T) {
	state := t.TempDir()
	store, err := Open(state)
	if err != nil {
		t.Fatal(err)
	}
	block := "### 2026-10-22 07:01\nT704 hermetic: notes round-trip."
	if err := store.AppendRollingBlock(block); err != nil {
		t.Fatal(err)
	}
	body, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, block) {
		t.Fatalf("body missing block:\n%s", body)
	}
	if !strings.Contains(body, "## STANDING") || !strings.Contains(body, "## ROLLING") {
		t.Fatalf("skeleton sections missing:\n%s", body)
	}
}

func TestT704TrimDropsWholeBlocksFromTop(t *testing.T) {
	var rolling strings.Builder
	for i := 0; i < 40; i++ {
		rolling.WriteString("### 2026-01-01 block " + string(rune('A'+i%26)) + "\n")
		for j := 0; j < 4; j++ {
			rolling.WriteString("line\n")
		}
		rolling.WriteString("\n")
	}
	trimmed := trimRolling(rolling.String(), 20)
	if strings.Count(trimmed, "### ") == 0 {
		t.Fatal("trim removed everything")
	}
	if strings.Contains(trimmed, "block A") && strings.Count(trimmed, "\n") > 25 {
		t.Fatalf("trim did not shrink enough: lines=%d", strings.Count(trimmed, "\n"))
	}
}

func TestT704EscalateRequiresSubjectAndBody(t *testing.T) {
	_, err := Escalate("", "body", "info", "", nil)
	if err == nil {
		t.Fatal("expected error for empty subject")
	}
	_, err = Escalate("subj", "", "info", "", nil)
	if err == nil {
		t.Fatal("expected error for empty body")
	}
}
