// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"strings"
	"testing"
)

// 🎯T722. The 🎯T386 checker fired four times on ge-t191-ndk-discovery's
// honest finish report, flagging the red-before controls the brief demanded.
// A green alone says the tests pass now. A red on the parent plus a green
// on the commit says this commit is why. Treating that control as a
// contradiction teaches workers to stop producing controls.
//
// Hermetic: GREEN after + named RED before → clean; unlabelled RED →
// flagged; RED asserted as the pass → flagged; a checker that flags every
// report containing a RED citation fails the clean path (the mutation).

func t722NamedBeforePlusGreen() string {
	return strings.Join([]string{
		"🎯T722 done, make test-go is green.",
		"",
		"| gate | verdict |",
		"|---|---|",
		"| `GATE t722-cook-before exit=1 RED id=11111111` | site 1: NDK not found |",
		"| `GATE t722-cook-after exit=0 GREEN id=22222222` | cook succeeds |",
		"",
		"Landed as abc1234.",
	}, "\n")
}

func TestT722NamedBeforePlusGreenIsNotFlagged(t *testing.T) {
	flags := FlagFalseGreen(t722NamedBeforePlusGreen(), nil)
	if len(flags) != 0 {
		t.Fatalf("named before-gate + after GREEN flagged %v:\n%s", kinds(flags), Banner(flags))
	}
}

func TestT722UnlabelledRedPlusGreenIsFlagged(t *testing.T) {
	report := strings.Join([]string{
		"🎯T722 done, make test-go is green.",
		"",
		"| gate | verdict |",
		"|---|---|",
		"| `GATE t722-cook exit=1 RED id=11111111` | site 1: NDK not found |",
		"| `GATE t722-cook-after exit=0 GREEN id=22222222` | cook succeeds |",
		"",
		"Landed as abc1234.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if !hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("flags = %v, want %s for an unlabelled RED", kinds(flags), FlagAttestationNotGreen)
	}
}

func TestT722RedAssertedAsThePassIsFlagged(t *testing.T) {
	report := strings.Join([]string{
		"🎯T722 done, make test-go is green.",
		"",
		"| gate | verdict |",
		"|---|---|",
		"| `GATE t722-cook-before exit=1 RED id=11111111` | unrelated flake, so I'm calling it passed |",
		"| `GATE t722-cook-after exit=0 GREEN id=22222222` | cook succeeds |",
		"",
		"Landed as abc1234.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if !hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("flags = %v, want %s for a RED claimed as the pass", kinds(flags), FlagAttestationNotGreen)
	}
}

func TestT722OnlyRedStillFlagged(t *testing.T) {
	report := strings.Join([]string{
		"🎯T722 done.",
		"",
		"| gate | verdict |",
		"|---|---|",
		"| `GATE t722-cook-before exit=1 RED id=11111111` | site 1: NDK not found |",
		"",
		"Landed as abc1234.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if !hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("flags = %v, want %s for only-RED", kinds(flags), FlagAttestationNotGreen)
	}
}

func TestT722SoleCauseOfTheGreenIsNotFlagged(t *testing.T) {
	// The load-bearing row: greenClaimMarkers used to fire first on
	// "sole cause of the green" and call the control a failed pass.
	report := strings.Join([]string{
		"🎯T722 done, make test-go is green.",
		"",
		"| gate | verdict |",
		"|---|---|",
		"| `GATE t722-cook-before-rebased exit=1 RED id=453cc1d9` | same red re-taken at the rebase base with only the three scripts reverted — isolates my commit as the sole cause of the green |",
		"| `GATE t722-cook-after exit=0 GREEN id=59dc1780` | cook succeeds |",
		"",
		"Landed as abc1234.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if len(flags) != 0 {
		t.Fatalf("rebased control flagged %v:\n%s", kinds(flags), Banner(flags))
	}
}

func TestT722SameLineBeforeAfterIsNotFlagged(t *testing.T) {
	report := strings.Join([]string{
		"🎯T722 done, make test-go is green.",
		"",
		"| `GATE t722-vkprobe-before exit=1 RED id=c1df720e` → `GATE t722-vkprobe-after exit=0 GREEN id=6841a237` | second entry point |",
		"",
		"Landed as abc1234.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("same-line before→after flagged: %v\n%s", kinds(flags), Banner(flags))
	}
}

func TestT722EnvelopeSlotWithoutProseIsNotFlagged(t *testing.T) {
	report := strings.Join([]string{
		"```jevons",
		"jevons: kind finish-report",
		"jevons: target T722",
		"jevons: oracle sha=abc1234 gate-id=22222222",
		"jevons: gate-role 33333333 control",
		"jevons: silent-ledger none",
		"```",
		"",
		"🎯T722 done, make test-go is green.",
		"",
		"GATE t722-parent exit=1 RED id=33333333",
		"GATE t722-after exit=0 GREEN id=22222222",
		"",
		"Landed as abc1234.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if len(flags) != 0 {
		t.Fatalf("declared control slot flagged %v:\n%s", kinds(flags), Banner(flags))
	}
}

func TestT722EnvelopeSlotCannotLaunderAPassClaim(t *testing.T) {
	report := strings.Join([]string{
		"```jevons",
		"jevons: kind finish-report",
		"jevons: target T722",
		"jevons: oracle sha=abc1234 gate-id=22222222",
		"jevons: gate-role 33333333 control",
		"jevons: silent-ledger none",
		"```",
		"",
		"🎯T722 done, make test-go is green.",
		"",
		"GATE t722-parent exit=1 RED id=33333333 — unrelated flake, so I'm calling it passed",
		"GATE t722-after exit=0 GREEN id=22222222",
		"",
		"Landed as abc1234.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if !hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("flags = %v, want %s — a slot cannot launder a pass claim", kinds(flags), FlagAttestationNotGreen)
	}
}

func TestT722NameOnlyRedSuffixStaysClaimedPass(t *testing.T) {
	// 🎯T443 invariant: a role a worker can assert by choosing a name
	// ending in -red is not a check. -before is the T722 convention;
	// -red is not.
	const raw = "GATE t443-prefix-red exit=1 RED id=abc12345"
	cited := ParseAttestations(raw)
	if len(cited) != 1 {
		t.Fatalf("parsed %d", len(cited))
	}
	if got := ClassifyCitation(raw, cited[0]); got != RoleClaimedPass {
		t.Fatalf("ClassifyCitation(name-only -red) = %s, want %s", got, RoleClaimedPass)
	}
}

func TestT722ClassifyControlProseBeatsNearbyGreen(t *testing.T) {
	const raw = "GATE t722-cook-before-rebased exit=1 RED id=453cc1d9"
	cited := ParseAttestations(raw)
	if len(cited) != 1 {
		t.Fatalf("parsed %d", len(cited))
	}
	window := raw + " — isolates my commit as the sole cause of the green"
	if got := ClassifyCitation(window, cited[0]); got != RoleControl {
		t.Fatalf("ClassifyCitation = %s, want %s", got, RoleControl)
	}
}

func TestT722SpecimenTableIsNotFlagged(t *testing.T) {
	// Shape of ge-t191-ndk-discovery 20260920T144931Z-047c9b39: a markdown
	// table of *-before REDs next to *-after GREENs. Four of those REDs
	// were the four firings.
	report := strings.Join([]string{
		"🎯T191 done, tests green.",
		"",
		"| Gate | Verdict |",
		"|---|---|",
		"| `GATE t191-cook-before exit=1 RED id=1ba897f8` | site 1: NDK r27 not found |",
		"| `GATE t191-host-triple-before exit=1 RED id=307906ee` | site 2: colour-escape in CC |",
		"| `GATE t191-cook-before-rebased exit=1 RED id=453cc1d9` | same red re-taken at the rebase base — isolates my commit as the sole cause of the green |",
		"| `GATE t191-cook-after exit=0 GREEN id=59dc1780` | acceptance #1 |",
		"| `GATE t191-vkprobe-before exit=1 RED id=c1df720e` → `GATE t191-vkprobe-after exit=0 GREEN id=6841a237` | second entry point |",
		"| `GATE t191-ndk-discovery-test exit=0 GREEN id=412e244e` | standing oracle |",
		"",
		"**Pre-existing red, not mine:** `GATE t191-make-bullseye exit=2 RED id=9a09de34` fails on missing macOS archives.",
		"",
		"Landed as 7d68797.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if len(flags) != 0 {
		t.Fatalf("specimen-shaped report flagged %v:\n%s", kinds(flags), Banner(flags))
	}
}

func TestT722MutationFlaggingEveryRedGoesRed(t *testing.T) {
	// The mutation this file exists to catch: treating any RED citation
	// as a contradiction. The named-before+green report contains a RED
	// and must stay unflagged; if a future edit flags every RED, this
	// fails.
	report := t722NamedBeforePlusGreen()
	if !strings.Contains(report, " RED ") {
		t.Fatal("fixture no longer contains a RED citation")
	}
	flags := FlagFalseGreen(report, nil)
	if len(flags) != 0 {
		t.Fatalf("mutation (flag every RED) would look like %v:\n%s", kinds(flags), Banner(flags))
	}
}
