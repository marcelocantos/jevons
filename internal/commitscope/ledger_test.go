// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"strings"
	"testing"
)

const seedLedger = `schema_version: 1
targets:
  T1:
    name: seed
    status: identified
    context: 'same text that bullseye may re-quote'
`

const restyledLedger = `schema_version: 1
targets:
  T1:
    name: seed
    status: identified
    context: |-
      same text that bullseye may re-quote
`

const foreignLedger = `schema_version: 1
targets:
  T1:
    name: seed
    status: identified
    context: 'same text that bullseye may re-quote'
  T999:
    name: other worker's target
    status: identified
`

const claimedAndForeign = `schema_version: 1
targets:
  T1:
    name: seed
    status: identified
    context: 'same text that bullseye may re-quote'
  T888:
    name: this actor's target
    status: identified
  T999:
    name: other worker's target
    status: identified
`

const onlyClaimed = `schema_version: 1
targets:
  T1:
    name: seed
    status: identified
    context: 'same text that bullseye may re-quote'
  T888:
    name: this actor's target
    status: identified
`

// TestYAMLRestyleIsNotATargetChange is the T742 false alarm: bullseye
// re-serialized a single-quoted scalar as a block scalar and treeguard's
// raw-line sweep reported the context as "gone". Semantic comparison
// must not.
func TestYAMLRestyleIsNotATargetChange(t *testing.T) {
	changed, err := ChangedTargets([]byte(seedLedger), []byte(restyledLedger))
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Errorf("scalar-style rewrite reported as changed targets %v", changed)
	}
}

func TestAddedTargetIsAChange(t *testing.T) {
	changed, err := ChangedTargets([]byte(seedLedger), []byte(foreignLedger))
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] != "T999" {
		t.Errorf("changed = %v, want [T999]", changed)
	}
}

func TestStatusChangeIsAChange(t *testing.T) {
	head := []byte(seedLedger)
	staged := []byte(strings.Replace(seedLedger, "status: identified", "status: achieved", 1))
	changed, err := ChangedTargets(head, staged)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] != "T1" {
		t.Errorf("changed = %v, want [T1]", changed)
	}
}

func TestForeignTargetsNamesUnclaimedRows(t *testing.T) {
	changed := []string{"T888", "T999"}
	if got := ForeignTargets(changed, []string{"T888"}); len(got) != 1 || got[0] != "T999" {
		t.Errorf("claimed T888: got %v, want [T999]", got)
	}
	if got := ForeignTargets(changed, nil); len(got) != 2 {
		t.Errorf("empty claim: got %v, want both", got)
	}
	if got := ForeignTargets(changed, []string{"🎯t888", "T999"}); len(got) != 0 {
		t.Errorf("all claimed: got %v, want none", got)
	}
}

func TestParseClaimedSplitsAndNormalizes(t *testing.T) {
	got := ParseClaimed(" t888, 🎯T999\nT1 ")
	want := []string{"T888", "T999", "T1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ParseClaimed = %v, want %v", got, want)
	}
}

// TestScopedLedgerCommitIsToldAboutForeignRows is the 🎯T748 acceptance:
// a path-scoped commit of a ledger already dirty with another worker's
// target is not silently allowed. Warn-and-name, not refuse: the PO must
// still be able to close targets.
func TestScopedLedgerCommitIsToldAboutForeignRows(t *testing.T) {
	req := Request{
		IndexFile: "/r/.git/next-index-9.lock",
		Staged:    []string{"bullseye.yaml"},
		Contents: []FileContent{{
			Path:   "bullseye.yaml",
			Head:   []byte(seedLedger),
			Staged: []byte(claimedAndForeign),
		}},
		Claimed: []string{"T888"},
	}
	v := Decide(&req)
	if v.Refused {
		t.Fatal("scoped ledger commit was refused; the PO must be able to land it")
	}
	if v.Message == "" {
		t.Fatal("scoped ledger commit with a foreign target was silently allowed")
	}
	for _, want := range []string{"T999", "🎯T748", "DIFF", "bullseye.yaml"} {
		if !strings.Contains(v.Message, want) {
			t.Errorf("warning omits %q:\n%s", want, v.Message)
		}
	}
	foreignBlock := v.Message
	if i := strings.Index(foreignBlock, "Claimed as yours"); i >= 0 {
		foreignBlock = foreignBlock[:i]
	}
	if strings.Contains(foreignBlock, "T888") {
		t.Errorf("claimed T888 listed as foreign:\n%s", v.Message)
	}
}

func TestScopedLedgerCommitOfOnlyClaimedRowsIsSilent(t *testing.T) {
	v := Decide(&Request{
		IndexFile: "/r/.git/next-index-9.lock",
		Staged:    []string{"bullseye.yaml"},
		Contents: []FileContent{{
			Path:   "bullseye.yaml",
			Head:   []byte(seedLedger),
			Staged: []byte(onlyClaimed),
		}},
		Claimed: []string{"T888"},
	})
	if v.Refused {
		t.Fatal("claimed-only ledger commit was refused")
	}
	if v.Message != "" {
		t.Errorf("claimed-only ledger commit still warned:\n%s", v.Message)
	}
}

func TestScopedLedgerRestyleIsSilent(t *testing.T) {
	v := Decide(&Request{
		IndexFile: "/r/.git/next-index-9.lock",
		Staged:    []string{"bullseye.yaml"},
		Contents: []FileContent{{
			Path:   "bullseye.yaml",
			Head:   []byte(seedLedger),
			Staged: []byte(restyledLedger),
		}},
	})
	if v.Refused || v.Message != "" {
		t.Errorf("YAML restyle of the same text was not silent: refused=%v\n%s", v.Refused, v.Message)
	}
}

func TestEmptyClaimNamesEveryChangedRow(t *testing.T) {
	v := Decide(&Request{
		IndexFile: "/r/.git/next-index-9.lock",
		Staged:    []string{"bullseye.yaml"},
		Contents: []FileContent{{
			Path:   "bullseye.yaml",
			Head:   []byte(seedLedger),
			Staged: []byte(foreignLedger),
		}},
	})
	if v.Refused {
		t.Fatal("empty-claim ledger commit was refused")
	}
	if !strings.Contains(v.Message, "T999") {
		t.Errorf("empty claim did not name the other worker's target:\n%s", v.Message)
	}
}
