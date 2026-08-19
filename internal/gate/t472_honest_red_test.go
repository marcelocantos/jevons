// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"strings"
	"testing"
)

// 🎯T472. After 🎯T443 retired the falsification-red case, the FALSE-GREEN
// banner fired twice within the hour on honest reports that assign a RED a
// different non-pass role: inherited/pre-existing breakage (jv-t390-plan-usage)
// and an oracle that caught a real defect then fixed (jv-t391-guard-all-paths).
// A checker that banners disciplined reports teaches readers to skim past it —
// the exact failure 🎯T386 exists to prevent.
//
// Fixtures are the bannered reports, byte for byte off the daemon's store.

const (
	t390InheritedFraming = "Master is red right now and it is not T390's"
	t391DefectFraming    = "caught a real defect"
)

func t390InheritedReport(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/t472_t390_inherited_red_report.md")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	report := string(b)
	if !strings.Contains(report, "GATE go-build exit=1 RED id=8a0faca5") {
		t.Fatal("fixture no longer carries the RED citation this target is about")
	}
	if !strings.Contains(report, t390InheritedFraming) {
		t.Fatalf("fixture no longer carries the framing %q", t390InheritedFraming)
	}
	return report
}

func t391DefectCaughtReport(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/t472_t391_defect_caught_red_report.md")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	report := string(b)
	if !strings.Contains(report, "GATE go-test exit=1 RED id=2817ca36") {
		t.Fatal("fixture no longer carries the RED citation this target is about")
	}
	if !strings.Contains(report, t391DefectFraming) {
		t.Fatalf("fixture no longer carries the framing %q", t391DefectFraming)
	}
	return report
}

func TestT472InheritedRedIsNotFlagged(t *testing.T) {
	report := t390InheritedReport(t)
	flags := FlagFalseGreen(report, nil)
	if len(flags) != 0 {
		t.Fatalf("honest inherited-red report flagged %v:\n%s", kinds(flags), Banner(flags))
	}
	if Banner(flags) != "" {
		t.Fatal("unflagged report produced a banner")
	}
}

func TestT472DefectCaughtRedIsNotFlagged(t *testing.T) {
	report := t391DefectCaughtReport(t)
	flags := FlagFalseGreen(report, nil)
	if len(flags) != 0 {
		t.Fatalf("honest defect-caught report flagged %v:\n%s", kinds(flags), Banner(flags))
	}
	if Banner(flags) != "" {
		t.Fatal("unflagged report produced a banner")
	}
}

// The inverse still banners: rewrite only the role-giving framing so the same
// RED is offered as a pass.
func TestT472InheritedRedCitedAsAPassIsFlagged(t *testing.T) {
	report := t390InheritedReport(t)
	asPass := strings.Replace(report, t390InheritedFraming,
		"the suite is green here too and every oracle passes", 1)
	if asPass == report {
		t.Fatal("mutation did not apply")
	}
	// Also strip the other inherited cues on the same line so the role is only
	// the pass claim — otherwise "before my commit exists" would still exempt.
	asPass = strings.Replace(asPass, "before my commit exists", "on this tree", 1)
	asPass = strings.Replace(asPass, "red at my parent", "red in this run", 1)

	flags := FlagFalseGreen(asPass, nil)
	if !hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("flags = %v, want %s for a red cited as a pass", kinds(flags), FlagAttestationNotGreen)
	}
	if !strings.Contains(Banner(flags), "go-build") {
		t.Fatalf("banner does not name the gate it disbelieves:\n%s", Banner(flags))
	}
}

func TestT472DefectCaughtRedCitedAsAPassIsFlagged(t *testing.T) {
	report := t391DefectCaughtReport(t)
	asPass := strings.Replace(report,
		"**First gate run was RED and caught a real defect**, not a test gap",
		"First gate run was green and every oracle passes", 1)
	if asPass == report {
		t.Fatal("mutation did not apply")
	}

	flags := FlagFalseGreen(asPass, nil)
	if !hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("flags = %v, want %s for a red cited as a pass", kinds(flags), FlagAttestationNotGreen)
	}
	if !strings.Contains(Banner(flags), "go-test") {
		t.Fatalf("banner does not name the gate it disbelieves:\n%s", Banner(flags))
	}
}

func TestT472ClassifyCitationAddsHonestRoles(t *testing.T) {
	const raw = "GATE go-build exit=1 RED id=8a0faca5"
	cited := ParseAttestations(raw)
	if len(cited) != 1 {
		t.Fatalf("parsed %d attestations from %q, want 1", len(cited), raw)
	}
	c := cited[0]

	for _, tc := range []struct {
		name   string
		window string
		want   CitationRole
	}{
		{"acceptance phrase", raw + " — Pre-existing HEAD breakage in cmd/treeguard, not mine", RoleInherited},
		{"not this target", raw + " — Master is red right now and it is not T390's", RoleInherited},
		{"before my commit", raw + " — red at my parent, before my commit exists", RoleInherited},
		{"caught a real defect", "First gate run was RED and " + t391DefectFraming + ": `" + raw + "` — Fixed just now", RoleDefectCaught},
		{"oracle caught", raw + " — oracle caught the panic; fixed just now", RoleDefectCaught},
		// Pass claim in the same window still wins — safe direction.
		{"inherited framing that also claims a pass",
			raw + " — pre-existing breakage not mine, and the suite is green", RoleClaimedPass},
		{"defect framing that also claims a pass",
			raw + " — caught a real defect, and every oracle passes", RoleClaimedPass},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyCitation(tc.window, c); got != tc.want {
				t.Fatalf("ClassifyCitation(%q) = %s, want %s", tc.window, got, tc.want)
			}
		})
	}
}
