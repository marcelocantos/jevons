// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"strings"
	"testing"
)

// 🎯T765: a ledger achieve's cited gate is checked the way a finish report's
// is. The fixtures mirror the real specimens: 🎯T757's achieve (prose "clean
// gate", and its only real gate DIRTY on the commit before the fix) and
// 🎯T752's (two GREEN ids on clean 6efff2ce), which must keep verifying.

const (
	t765Parent = "cac727d3aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	t765Fix    = "3dda2240bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	t765Child  = "4ee1f00dcccccccccccccccccccccccccccccccc"
	t752Commit = "6efff2ce3255b070665e339fa6e270f6494b4dd7"
)

func t765Records() map[string]*Record {
	green := func(id, commit string, clean bool) *Record {
		dirty := 0
		if !clean {
			dirty = 7
		}
		verdict := VerdictGreen
		if !clean {
			verdict = VerdictDirty
		}
		return &Record{
			ID: id, Name: "t765", StatusKnown: true, ExitStatus: 0, Verdict: verdict,
			Tree: &TreeProvenance{Commit: commit, Clean: clean, DirtyFiles: dirty},
		}
	}
	return map[string]*Record{
		// 🎯T757's real record: exit 0, DIRTY, on the parent of the fix.
		"5921e618": green("5921e618", t765Parent, false),
		// A clean GREEN, but on the commit before the fix.
		"0ld0ld00": green("0ld0ld00", t765Parent, true),
		// A clean GREEN on the fix itself, and on a descendant containing it.
		"f1f1f1f1": green("f1f1f1f1", t765Fix, true),
		"c1c1c1c1": green("c1c1c1c1", t765Child, true),
		// GREEN verdict but an unclean tree (a pre-🎯T718 record shape), and a
		// GREEN with no tree provenance at all.
		"a0a0a0a0": {ID: "a0a0a0a0", StatusKnown: true, Verdict: VerdictGreen,
			Tree: &TreeProvenance{Commit: t765Fix, DirtyFiles: 3}},
		"b0b0b0b0": {ID: "b0b0b0b0", StatusKnown: true, Verdict: VerdictGreen},
		// 🎯T752's two real records.
		"aefad0ae": green("aefad0ae", t752Commit, true),
		"1b18363c": green("1b18363c", t752Commit, true),
	}
}

// t765Contains is a three-commit history: parent ← fix ← child.
func t765Contains(code, fix string) bool {
	order := []string{t765Parent, t765Fix, t765Child}
	pos := func(sha string) int {
		for i, c := range order {
			if sameCommit(c, sha) {
				return i
			}
		}
		return -1
	}
	if sameCommit(code, fix) {
		return true
	}
	c, f := pos(code), pos(fix)
	return c >= 0 && f >= 0 && f <= c
}

func t765Check(attestation string) AchieveResult {
	recs := t765Records()
	return CheckAchieve(&AchieveCheckArgs{
		Attestation: attestation,
		Lookup: func(id string) (*Record, bool) {
			r, ok := recs[id]
			return r, ok
		},
		Contains: t765Contains,
	})
}

func t765HasFlag(r AchieveResult, kind FlagKind) bool {
	for _, f := range r.Flags {
		if f.Kind == kind {
			return true
		}
	}
	return false
}

// (a) A gate named only in prose is refused: 🎯T757's achieve verbatim.
func TestT765ProseGateWithoutIDIsRefused(t *testing.T) {
	r := t765Check("3dda2240; bin/gate -clean go test ./internal/mcpserver/ -run T757 (GREEN). " +
		"Routing line now carries report_id and age= at parent delivery.")
	if r.Verdict != AchieveRefused || !t765HasFlag(r, FlagAchieveGateUncited) {
		t.Fatalf("prose-only gate: got %s", r)
	}
	// 🎯T760's shape: a GATE line with no id= is still prose.
	r = t765Check("Landed 3dda2240. GATE go-test-T760-T734 exit=0 GREEN (clean gate pending this turn).")
	if r.Verdict != AchieveRefused || !t765HasFlag(r, FlagAchieveGateUncited) {
		t.Fatalf("id-less GATE line: got %s", r)
	}
}

// (b) A cited DIRTY record is refused, whether the attestation calls it
// DIRTY or misquotes it as GREEN.
func TestT765DirtyRecordIsRefused(t *testing.T) {
	for _, att := range []string{
		"3dda2240; GATE t757 exit=0 DIRTY id=5921e618",
		"3dda2240; GATE t757 GREEN id=5921e618",
		"3dda2240; GATE t757 id=5921e618",
	} {
		r := t765Check(att)
		if r.Verdict != AchieveRefused || !t765HasFlag(r, FlagAttestationNotGreen) {
			t.Errorf("%q: got %s", att, r)
		}
	}
}

// GREEN is not enough: the record must also say it measured a clean tree.
func TestT765GreenWithoutCleanTreeIsRefused(t *testing.T) {
	r := t765Check("3dda2240; GATE t757 GREEN id=a0a0a0a0")
	if r.Verdict != AchieveRefused || !t765HasFlag(r, FlagDirtyTreeGate) {
		t.Fatalf("green on unclean tree: got %s", r)
	}
	r = t765Check("3dda2240; GATE t757 GREEN id=b0b0b0b0")
	if r.Verdict != AchieveRefused || !t765HasFlag(r, FlagAchieveTreeUnknown) {
		t.Fatalf("green with no tree: got %s", r)
	}
}

// (c) A clean GREEN that ran on the commit before the fix is refused.
func TestT765GreenOnAncestorOfFixIsRefused(t *testing.T) {
	r := t765Check("Fix 3dda2240; GATE t757 exit=0 GREEN id=0ld0ld00 tree=clean@cac727d3")
	if r.Verdict != AchieveRefused || !t765HasFlag(r, FlagAchieveGatePrecedesFix) {
		t.Fatalf("green on parent of fix: got %s", r)
	}
}

// A hex token that is not a commit — a gate id quoted bare, a report id —
// is not a fix the gate must contain, once the caller can tell commits apart.
func TestT765NonCommitTokensAreNotFixes(t *testing.T) {
	att := "Fix 3dda2240 (report 20260920T141435Z-a4e18bdc, earlier run 5921e618); " +
		"GATE t757 exit=0 GREEN id=f1f1f1f1"
	recs := t765Records()
	r := CheckAchieve(&AchieveCheckArgs{
		Attestation: att,
		Lookup:      func(id string) (*Record, bool) { r, ok := recs[id]; return r, ok },
		Contains:    t765Contains,
		IsCommit:    func(sha string) bool { return sameCommit(sha, t765Fix) },
	})
	if r.Verdict != AchieveVerified {
		t.Fatalf("non-commit tokens must not be demanded: got %s", r)
	}
	// Without the filter the same text is refused: the filter is what passes it.
	if r := t765Check(att); r.Verdict != AchieveRefused {
		t.Fatalf("control without IsCommit: got %s", r)
	}
}

// A gate id with no record behind it is refused and never marked, even with
// an accepted-risk sentence: that is a run that did not happen.
func TestT765UnknownIDIsRefusedEvenWithRisk(t *testing.T) {
	r := t765Check("3dda2240; GATE t757 GREEN id=deadd00d. Accepted-risk: gate could not run clean.")
	if r.Verdict != AchieveRefused || !t765HasFlag(r, FlagAttestationUnknown) {
		t.Fatalf("unknown id: got %s", r)
	}
}

// (d) A clean GREEN on the fix, or on a descendant that contains it, is
// accepted.
func TestT765CleanGreenOnFixIsAccepted(t *testing.T) {
	for _, att := range []string{
		"3dda2240; GATE t757 exit=0 GREEN id=f1f1f1f1 tree=clean@3dda2240",
		"3dda2240; GATE t757 exit=0 GREEN id=c1c1c1c1 tree=clean@4ee1f00d",
	} {
		if r := t765Check(att); r.Verdict != AchieveVerified {
			t.Errorf("%q: got %s", att, r)
		}
	}
}

// 🎯T752's real attestation is the control: a verifiable achieve must verify,
// including its accepted risk about something other than the gate.
func TestT765T752AttestationVerifies(t *testing.T) {
	att := "Overseer verify: GATE t752-head GREEN id=aefad0ae tree=clean@6efff2ce; " +
		"GATE t752-named GREEN id=1b18363c; activation buildsnap 6efff2ce. " +
		"Accepted-risk T552: no Grok seat for live observe."
	if r := t765Check(att); r.Verdict != AchieveVerified {
		t.Fatalf("T752 control: got %s", r)
	}
}

// The escape for repos where -clean cannot pass: an accepted risk about the
// gate evidence is recorded with a visible marker — not verified, not silent.
func TestT765AcceptedRiskOnDirtyIsMarked(t *testing.T) {
	r := t765Check("3dda2240; GATE t757 exit=0 DIRTY id=5921e618. " +
		"Accepted-risk: -clean cannot pass until ge ships SDL3 libs (ge T203).")
	if r.Verdict != AchieveMarked || !t765HasFlag(r, FlagAttestationNotGreen) {
		t.Fatalf("accepted-risk dirty: got %s", r)
	}
	if !strings.HasPrefix(r.String(), AchieveMarker) {
		t.Fatalf("marked result must lead with %q: %s", AchieveMarker, r)
	}
	// An accepted risk about something else does not launder the gate.
	r = t765Check("3dda2240; GATE t757 exit=0 DIRTY id=5921e618. " +
		"Accepted-risk T552: no Grok seat for live observe.")
	if r.Verdict != AchieveRefused {
		t.Fatalf("unrelated risk must not mark a dirty gate: got %s", r)
	}
}

// An attestation that claims no gate is outside this check.
func TestT765UngatedAttestationIsNotJudged(t *testing.T) {
	if r := t765Check("Docs-only: persona text landed in 3dda2240."); r.Verdict != AchieveUngated {
		t.Fatalf("ungated: got %s", r)
	}
}

// The ledger walk reads the live attestation and the reverted achieves kept in
// context, without double-counting bullseye's mirror of the live one.
func TestT765LedgerAchievesReadsBothSources(t *testing.T) {
	ledger := []byte(`schema_version: 5
targets:
  T752:
    status: achieved
    achieved: 2026-09-21
    attestation: 'GATE t752-head GREEN id=aefad0ae'
    context: |-
      Found by a worker.

      Achieved 2026-09-21: GATE t752-head GREEN id=aefad0ae
  T757:
    status: converging
    context: |-
      Split from T747.

      Achieved 2026-09-21: 3dda2240; bin/gate -clean go test (GREEN).

      Reverted 2026-09-21: false green.
`)
	got, err := LedgerAchieves(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 achieves, got %+v", got)
	}
	if got[0].ID != "T752" || got[0].Source != "attestation" || got[0].Date != "2026-09-21" {
		t.Errorf("T752: %+v", got[0])
	}
	if got[1].ID != "T757" || got[1].Source != "context" ||
		got[1].Attestation != "3dda2240; bin/gate -clean go test (GREEN)." {
		t.Errorf("T757: %+v", got[1])
	}
	if r := t765Check(got[1].Attestation); r.Verdict != AchieveRefused {
		t.Errorf("T757 from ledger: got %s", r)
	}
	if _, err := LedgerAchieves([]byte("targets: [")); err == nil {
		t.Error("malformed ledger must be an error")
	}
}
