// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"strings"
	"testing"
)

// 🎯T759. The false-green checker was structurally opposed to the fleet's
// preferred instrument: deterministic differentials and mutation evidence
// necessarily emit REDs, and flagging every RED taught seats to stop citing
// them. T443/T472/T722 covered prose and control slots; this target closes
// the remaining cases — declared mutation / red-demo / differential roles,
// the two real claudia specimens that fired wrongly, and the inverse:
// a mutation arm that comes back GREEN is the thing that deserves a flag.

func TestT759CodexLoadSiblingHangIsNotFlagged(t *testing.T) {
	// cl-t33-codex-load specimen: cites a RED full-suite run whose only
	// failure was a sibling's cursor-ACP hang, with codex green inside the
	// same run. The RED is attribution, not a claim that the seat failed.
	report := strings.Join([]string{
		"```jevons",
		"jevons: kind finish-report",
		"jevons: target T33",
		"jevons: oracle sha=abc1234 gate-id=22222222",
		"jevons: gate-role 787c7e02 blocker",
		"jevons: verdict green",
		"jevons: status landed",
		"jevons: silent-ledger none",
		"```",
		"",
		"🎯T33 done, make test-go is green for the codex path.",
		"",
		"Sibling hang, not mine: `GATE t33-full-gate-clean-underload exit=2 RED id=787c7e02` —",
		"the failure was a sibling's cursor-ACP hang; codex was green inside the same run.",
		"",
		"`GATE t33-codex-path exit=0 GREEN id=22222222` is the acceptance.",
		"",
		"Landed as abc1234.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("sibling-hang RED flagged attestation_not_green: %v\n%s", kinds(flags), Banner(flags))
	}
	if len(flags) != 0 {
		t.Fatalf("honest cl-t33-shaped report flagged %v:\n%s", kinds(flags), Banner(flags))
	}
}

func TestT759PoolEventsInheritedParentIsNotFlagged(t *testing.T) {
	// cl-t78-pool-events specimen: cites two REDs deliberately to establish
	// that the failure came from the commit's PARENT, then a GREEN at the
	// first commit where its work and the fix coexist.
	report := strings.Join([]string{
		"```jevons",
		"jevons: kind finish-report",
		"jevons: target T78",
		"jevons: oracle sha=def5678 gate-id=cec59e91",
		"jevons: gate-role 7a9b40c1 blocker",
		"jevons: gate-role 7d3bbc1b blocker",
		"jevons: verdict green",
		"jevons: status landed",
		"jevons: silent-ledger none",
		"```",
		"",
		"🎯T78 done, make gate is green.",
		"",
		"Inherited from parent, not mine:",
		"`GATE make-gate exit=2 RED id=7a9b40c1` and `GATE parent-inherits-red exit=1 RED id=7d3bbc1b`",
		"— breakage was inherited from this commit's parent (TestHermeticCursorSessionMultiChunkReply).",
		"",
		"First commit where work and fix coexist: `GATE make-gate exit=0 GREEN id=cec59e91`.",
		"",
		"Landed as def5678.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("inherited-parent REDs flagged: %v\n%s", kinds(flags), Banner(flags))
	}
	if len(flags) != 0 {
		t.Fatalf("honest cl-t78-shaped report flagged %v:\n%s", kinds(flags), Banner(flags))
	}
}

func TestT759RedDemoAcceptanceIsNotFlagged(t *testing.T) {
	// cl-t100-live-gate-coverage: the RED *is* the acceptance — "the check
	// itself must fail when a new live test is added without wiring".
	report := strings.Join([]string{
		"```jevons",
		"jevons: kind finish-report",
		"jevons: target T100",
		"jevons: oracle sha=aaa1111 gate-id=f057d894",
		"jevons: gate-role f057d894 red-demo",
		"jevons: verdict green",
		"jevons: status landed",
		"jevons: silent-ledger none",
		"```",
		"",
		"🎯T100 done. The red-demo is the deliverable:",
		"`GATE livegate-red-demo exit=1 RED id=f057d894` — injected brand_new_backend_live_test.go,",
		"the check named both violated obligations. RED is success by construction.",
		"",
		"Landed as aaa1111.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("red-demo RED flagged: %v\n%s", kinds(flags), Banner(flags))
	}
	if len(flags) != 0 {
		t.Fatalf("honest red-demo report flagged %v:\n%s", kinds(flags), Banner(flags))
	}
}

func TestT759DifferentialBrokenLegIsNotFlagged(t *testing.T) {
	// cl-t93-gate-the-landed-fix: broken leg + mutation kill, both RED on
	// purpose, next to the green after-leg.
	report := strings.Join([]string{
		"```jevons",
		"jevons: kind finish-report",
		"jevons: target T93",
		"jevons: oracle sha=32d9fbd gate-id=bbbbbbbb",
		"jevons: gate-role aa0ad483 differential-broken-leg",
		"jevons: gate-role a2fb595c mutation",
		"jevons: verdict green",
		"jevons: status landed",
		"jevons: silent-ledger none",
		"```",
		"",
		"🎯T93 done, make test-go is green.",
		"",
		"| gate | role |",
		"|---|---|",
		"| `GATE t93-oneknob-red exit=1 RED id=aa0ad483` | broken leg of the differential |",
		"| `GATE t93-guard-kills-oneknob exit=1 RED id=a2fb595c` | mutation kill — guard fires |",
		"| `GATE t93-oneknob-green exit=0 GREEN id=bbbbbbbb` | after leg |",
		"",
		"Landed as 32d9fbd.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("differential/mutation REDs flagged: %v\n%s", kinds(flags), Banner(flags))
	}
	if len(flags) != 0 {
		t.Fatalf("honest differential report flagged %v:\n%s", kinds(flags), Banner(flags))
	}
}

func TestT759OwnAcceptanceRedStillFlagged(t *testing.T) {
	// The half that must not break: envelope claims status done / verdict
	// green while its OWN acceptance gate (no honest role) is RED.
	report := strings.Join([]string{
		"```jevons",
		"jevons: kind finish-report",
		"jevons: target T759",
		"jevons: oracle sha=abc1234 gate-id=deadbeef",
		"jevons: verdict green",
		"jevons: status landed",
		"jevons: silent-ledger none",
		"```",
		"",
		"🎯T759 done, make test-go is green.",
		"",
		"`GATE t759-acceptance exit=1 RED id=deadbeef` — calling it a pass.",
		"",
		"Landed as abc1234.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if !hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("flags = %v, want %s for own acceptance RED", kinds(flags), FlagAttestationNotGreen)
	}
}

func TestT759MutationGreenIsFlagged(t *testing.T) {
	// Inverse of the red-demo / mutation case: a declared mutation that
	// comes back GREEN is a guard with no teeth.
	report := strings.Join([]string{
		"```jevons",
		"jevons: kind finish-report",
		"jevons: target T759",
		"jevons: oracle sha=abc1234 gate-id=cccccccc",
		"jevons: gate-role a2fb595c mutation",
		"jevons: verdict green",
		"jevons: status landed",
		"jevons: silent-ledger none",
		"```",
		"",
		"🎯T759 done, make test-go is green.",
		"",
		"`GATE t93-guard-kills-oneknob exit=0 GREEN id=a2fb595c` — mutation arm.",
		"`GATE t759-after exit=0 GREEN id=cccccccc` — acceptance.",
		"",
		"Landed as abc1234.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if !hasKind(flags, FlagAttestationUnexpectedGreen) {
		t.Fatalf("flags = %v, want %s for a mutation that went GREEN",
			kinds(flags), FlagAttestationUnexpectedGreen)
	}
	if !strings.Contains(Banner(flags), "a2fb595c") && !strings.Contains(Banner(flags), "mutation") {
		t.Fatalf("banner does not name the mutation gate:\n%s", Banner(flags))
	}
}

func TestT759RedDemoGreenIsFlagged(t *testing.T) {
	report := strings.Join([]string{
		"```jevons",
		"jevons: kind finish-report",
		"jevons: target T100",
		"jevons: gate-role f057d894 red-demo",
		"jevons: silent-ledger none",
		"```",
		"",
		"🎯T100 done.",
		"`GATE livegate-red-demo exit=0 GREEN id=f057d894`",
		"Landed as aaa1111.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if !hasKind(flags, FlagAttestationUnexpectedGreen) {
		t.Fatalf("flags = %v, want %s for a red-demo that went GREEN",
			kinds(flags), FlagAttestationUnexpectedGreen)
	}
}

func TestT759UnlabelledRedStillFlagged(t *testing.T) {
	// Conservative default: no role, no honest prose → still flag.
	report := strings.Join([]string{
		"🎯T759 done, make test-go is green.",
		"",
		"`GATE t759-mystery exit=1 RED id=99999999`",
		"",
		"Landed as abc1234.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if !hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("flags = %v, want %s for unlabelled RED", kinds(flags), FlagAttestationNotGreen)
	}
}

func TestT759MutationProseWithoutSlotIsNotFlagged(t *testing.T) {
	// Prose path: "mutation check" / "guard fires" without a gate-role slot.
	report := strings.Join([]string{
		"🎯T759 done, make test-go is green.",
		"",
		"`GATE t759-mut exit=1 RED id=a2fb595c` — mutation check: the guard fires on both arms.",
		"`GATE t759-after exit=0 GREEN id=cccccccc`",
		"",
		"Landed as abc1234.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("mutation-prose RED flagged: %v\n%s", kinds(flags), Banner(flags))
	}
}

func TestT759ClassifyMutationAndDeclaredRoles(t *testing.T) {
	const raw = "GATE t759-mut exit=1 RED id=a2fb595c"
	cited := ParseAttestations(raw)
	if len(cited) != 1 {
		t.Fatalf("parsed %d", len(cited))
	}
	c := cited[0]
	for _, tc := range []struct {
		name   string
		window string
		want   CitationRole
	}{
		{"mutation check", raw + " — mutation check on the guard", RoleMutation},
		{"guard fires", raw + " — the guard fires on both arms", RoleMutation},
		{"red-demo", raw + " — red-demo acceptance deliverable", RoleMutation},
		{"differential", raw + " — broken leg of the differential", RoleControl},
		{"name only", raw, RoleClaimedPass},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyCitation(tc.window, c); got != tc.want {
				t.Fatalf("ClassifyCitation(%q) = %s, want %s", tc.window, got, tc.want)
			}
		})
	}
}

func TestT759MutationFlaggingEveryRedGoesRed(t *testing.T) {
	// The mutation this file exists to catch: treating any RED citation as
	// a contradiction. The red-demo report contains a RED and must stay
	// unflagged; if a future edit flags every RED, this fails.
	report := strings.Join([]string{
		"```jevons",
		"jevons: kind finish-report",
		"jevons: target T100",
		"jevons: gate-role f057d894 red-demo",
		"jevons: silent-ledger none",
		"```",
		"",
		"`GATE livegate-red-demo exit=1 RED id=f057d894`",
	}, "\n")
	if !strings.Contains(report, " RED ") {
		t.Fatal("fixture no longer contains a RED citation")
	}
	flags := FlagFalseGreen(report, nil)
	if len(flags) != 0 {
		t.Fatalf("mutation (flag every RED) would look like %v:\n%s", kinds(flags), Banner(flags))
	}
}
