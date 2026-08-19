// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package shaevidence

import (
	"strings"
	"testing"
)

func TestExtractEvidenceSHAsKeepsCommitCitations(t *testing.T) {
	text := strings.Join([]string{
		`SHA abcdef1 feat(T427): evidence is reachable`,
		`Landed by commit 1234567 "fix(ledger)"`,
		`Product SHA fedcba9 lands the oracle.`,
		`verified ancestor of HEAD: 0a1b2c3`,
		`git merge-base --is-ancestor deadbee HEAD`,
	}, "\n")
	got := ExtractEvidenceSHAs(text)
	want := map[string]bool{
		"abcdef1": true, "1234567": true, "fedcba9": true,
		"0a1b2c3": true, "deadbee": true,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d citations %#v, want %d %v", len(got), got, len(want), want)
	}
	for _, c := range got {
		if !want[c.SHA] {
			t.Errorf("unexpected citation %q", c.SHA)
		}
	}
}

func TestExtractEvidenceSHAsIgnoresThenCurrentHEAD(t *testing.T) {
	// T468 fixture shape: evidence SHA + merge-base proof naming then-HEAD.
	line := "**Landed (mine):** `9f93f16` — proven reachable: " +
		"`git merge-base --is-ancestor 9f93f16 HEAD` passes against current `c97d187`."
	got := ExtractEvidenceSHAs(line)
	if len(got) != 1 || got[0].SHA != "9f93f16" {
		t.Fatalf("got %#v, want only the evidence SHA 9f93f16 (not then-HEAD c97d187)", got)
	}
}

func TestExtractEvidenceSHAsIgnoresGateBookkeeping(t *testing.T) {
	text := `GATE t427 exit=0 GREEN id=9f13c0a2 out=6b1d9e4f2a01 dur=1.2s tree=clean@aabbccd`
	if got := ExtractEvidenceSHAs(text); len(got) != 0 {
		t.Fatalf("gate bookkeeping must not be evidence SHAs, got %#v", got)
	}
}

func TestExtractEvidenceSHAsIgnoresBareHexWithoutCue(t *testing.T) {
	text := "the colour code is deadbeef and the uuid is 0123456789abcdef"
	if got := ExtractEvidenceSHAs(text); len(got) != 0 {
		t.Fatalf("bare hex without evidence cue: %#v", got)
	}
}

func TestExtractEvidenceSHAsDedupes(t *testing.T) {
	text := "SHA abcdef1 landed\ncommit abcdef1 again"
	got := ExtractEvidenceSHAs(text)
	if len(got) != 1 || got[0].SHA != "abcdef1" {
		t.Fatalf("got %#v, want one abcdef1", got)
	}
}

func TestAttestationBlobKeepsOnlyEvidenceLines(t *testing.T) {
	yaml := strings.Join([]string{
		"  T1:",
		"    name: example",
		"    attestation: 'SHA aaa1111 lands it'",
		"    context: |-",
		"      Ignore SHA bbbb222 buried in context only.",
		"      Achieved 2026-08-10: SHA ccc3333 was the gate.",
		"    acceptance:",
		"    - cite SHA dddd444 in acceptance is not attestation",
	}, "\n")
	blob := AttestationBlob(yaml)
	if !strings.Contains(blob, "aaa1111") || !strings.Contains(blob, "ccc3333") {
		t.Fatalf("blob missing attestation/Achieved SHAs:\n%s", blob)
	}
	if strings.Contains(blob, "bbbb222") || strings.Contains(blob, "dddd444") {
		t.Fatalf("blob swallowed non-attestation prose:\n%s", blob)
	}
}

func TestTouchesOnlyLedgerPredicate(t *testing.T) {
	if !TouchesOnlyLedger([]string{"bullseye.yaml"}) {
		t.Fatal("bare bullseye.yaml must be amend-vulnerable shape")
	}
	if !TouchesOnlyLedger([]string{"path/to/bullseye.yaml"}) {
		t.Fatal("nested bullseye.yaml must match")
	}
	// Single-file CODE commit is safe — file count is not the predicate.
	if TouchesOnlyLedger([]string{"src/graph.rs"}) {
		t.Fatal("single-file code commit must not look ledger-only")
	}
	if TouchesOnlyLedger([]string{"bullseye.yaml", "README.md"}) {
		t.Fatal("multi-path write is not the yaml-only amend shape")
	}
	if TouchesOnlyLedger(nil) {
		t.Fatal("empty paths are not ledger-only")
	}
}
