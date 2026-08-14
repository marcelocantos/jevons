// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"strings"
	"testing"
)

// 🎯T443. The 🎯T386 checker fired a FALSE-GREEN banner on an honest report —
// jv-t380-auto's, which cited a deliberately RED gate as proof that its
// oracles fail against a tree with the wiring removed. That red is the thing
// 🎯T31 asks a worker to produce. A checker that banners the behaviour it
// demands is worse than a checker that banners nothing: it teaches the
// overseer to read past the banner, and the next real false green rides
// through behind that habit.
//
// The fixture is that report, byte for byte off the daemon's report store
// (~/.jevons/agent-reports/jv-t380-auto/20260814T224315Z-abd39af0.json), for
// the same reason 🎯T386's fixtures are written in a worker's register: a
// classifier tuned on tidy synthetic prose is tuned on the wrong distribution.

// theRedRowFraming is the prose that makes jv-t380-auto's RED citation a
// falsification result rather than a failure. The mutation tests rewrite it,
// so the two cases differ in exactly the thing under test — the role the
// report gives the citation — and in nothing else.
const theRedRowFraming = "same oracles with the wiring removed fail on their **own assertions**"

func t380Report(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/t443_t380_prefix_red_report.md")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	report := string(b)
	if !strings.Contains(report, "GATE t380-prefix-red exit=1 RED id=7ff7f677") {
		t.Fatal("fixture no longer carries the RED citation this target is about")
	}
	if !strings.Contains(report, theRedRowFraming) {
		t.Fatalf("fixture no longer carries the framing %q", theRedRowFraming)
	}
	return report
}

// The half that was broken: the honest report passes with no banner at all.
func TestT443FalsificationRedIsNotFlagged(t *testing.T) {
	report := t380Report(t)
	flags := FlagFalseGreen(report, nil)
	if len(flags) != 0 {
		t.Fatalf("honest falsification report flagged %v:\n%s", kinds(flags), Banner(flags))
	}
	if Banner(flags) != "" {
		t.Fatal("unflagged report produced a banner")
	}
}

// The half that must not break: the same RED gate, in the same report, cited
// as though it had passed. The exemption is for the role, not for the record —
// so rewriting only the framing brings the banner straight back.
func TestT443SameRedCitedAsAPassIsFlagged(t *testing.T) {
	report := t380Report(t)
	asPass := strings.Replace(report, theRedRowFraming,
		"the suite is green here too and every oracle passes", 1)
	if asPass == report {
		t.Fatal("mutation did not apply")
	}

	flags := FlagFalseGreen(asPass, nil)
	if !hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("flags = %v, want %s for a red cited as a pass", kinds(flags), FlagAttestationNotGreen)
	}
	// The quoted failure comes back with it: it is only exempt while it is the
	// content of a demonstration.
	if !hasKind(flags, FlagOutputContradicts) {
		t.Fatalf("flags = %v, want %s — the quoted FAIL is no longer a demonstration",
			kinds(flags), FlagOutputContradicts)
	}
	if !strings.Contains(Banner(flags), "t380-prefix-red") {
		t.Fatalf("banner does not name the gate it disbelieves:\n%s", Banner(flags))
	}
}

func TestClassifyCitationSeparatesRoles(t *testing.T) {
	const raw = "GATE t443-prefix-red exit=1 RED id=abc12345"
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
		{"the real row", "| `" + raw + "` | " + theRedRowFraming + " |", RoleFalsification},
		{"label above the citation", "Red against the pre-fix tree:\n    " + raw, RoleFalsification},
		{"fix reverted", raw + " — with the fix reverted the oracles fail", RoleFalsification},
		// The name is the worker's own label. A role a worker can assert by
		// choosing a name is not a check, so the name alone classifies nothing.
		{"name only, no prose", raw, RoleClaimedPass},
		{"red waved through", raw + " — unrelated flake, so I'm calling it passed", RoleClaimedPass},
		{"honest blockage, no falsification claim", raw + " — the suite is failing, investigating",
			RoleClaimedPass},
		// Both halves are required: failure language on its own is what every
		// report about a red says.
		{"failure language without removal framing", "The oracles fail here.\n" + raw, RoleClaimedPass},
		// A window that also calls this gate a pass is not a demonstration,
		// however it words the rest. Safe direction on purpose.
		{"falsification framing that also claims a pass",
			raw + " — with the wiring removed they fail, and the suite is green", RoleClaimedPass},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyCitation(tc.window, c); got != tc.want {
				t.Fatalf("ClassifyCitation(%q) = %s, want %s", tc.window, got, tc.want)
			}
		})
	}
}

// The exemption is local to the demonstration. A neighbouring row's evidence
// keeps its own verdict — otherwise one honest red in a table of gate results
// would silence the whole table, which is a false green by another route.
func TestT443ExemptionDoesNotCoverNeighbouringRows(t *testing.T) {
	report := strings.Join([]string{
		"🎯T444 done, tests green.",
		"",
		"| gate | verdict |",
		"|---|---|",
		"| `GATE t444-prefix-red exit=1 RED id=11111111` | with the wiring removed the oracles fail on their own assertions |",
		"| `GATE t444-gotest exit=0 GREEN id=22222222` | full run — `panic: test timed out after 10m0s` at the end, unrelated |",
		"",
		"Landed as abc1234.",
	}, "\n")

	flags := FlagFalseGreen(report, nil)
	if hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("the falsification row was flagged: %v", kinds(flags))
	}
	if !hasKind(flags, FlagOutputContradicts) {
		t.Fatalf("flags = %v, want %s — the panic quoted on the GREEN row is not a demonstration",
			kinds(flags), FlagOutputContradicts)
	}
}

// A failure quoted well away from any falsification framing is scanned exactly
// as it was before 🎯T443.
func TestT443OutputScanStillSeesUnframedFailures(t *testing.T) {
	report := strings.Join([]string{
		"🎯T445 done — make test-go is green.",
		"",
		"| gate | verdict |",
		"|---|---|",
		"| `GATE t445-prefix-red exit=1 RED id=33333333` | with the fix removed the oracles fail |",
		"",
		"Final run:",
		"",
		"    --- FAIL: TestSomethingElse (0.02s)",
		"",
		"Calling it a pass.",
	}, "\n")

	flags := FlagFalseGreen(report, nil)
	if hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("the falsification row was flagged: %v", kinds(flags))
	}
	if !hasKind(flags, FlagOutputContradicts) {
		t.Fatalf("flags = %v, want %s", kinds(flags), FlagOutputContradicts)
	}
}
