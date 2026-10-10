// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"strings"
	"testing"
)

// This is the T603 independent-review report's evidence structure, including
// the reported SIGKILL diagnostic and the unrelated full-package RED.
func t1048Review() string {
	return strings.Join([]string{
		"```jevons", "jevons: kind finish-report", "jevons: target T603", "jevons: status in-progress", "jevons: silent-ledger none", "```", "",
		"**Independent review complete; no achievement recorded.**", "",
		"- **Full owner-path package:** `GATE go-test exit=0 GREEN id=7b524b4c out=15c10dffad88 dur=1m26.8s tree=clean@22656c6e9ccd`. The command was `go test ./internal/mcpserver -count=1`.",
		"- **SIGKILL record:** `GATE sh-kill--9 exit=unknown KILLED id=b82a63a6 out=e3b0c44298fc dur=0s tree=clean@22656c6e9ccd`. The record confirms `process terminated by signal: killed`; it is not an absent or passing record.",
		"- **Focused regression:** `GATE go-test-TestT603TestT461 exit=0 GREEN id=728e86f7 out=6678adba10bf dur=41.1s tree=clean@22656c6e9ccd`.", "",
		"**RED disclosure:** The additional *full* `internal/gate` run was `GATE go-test exit=1 RED id=02c22a0d out=959129a1ad94 dur=59.6s tree=clean@22656c6e9ccd`. Its failure was `TestCheckAttestationAcceptance1/gate_on_a_commit_lacking_the_fix`. This is a T765 achievement-attestation test, not a T603 lease or kill test; it does not negate the focused GREEN. It does prevent claiming the entire `internal/gate` package is green.",
	}, "\n")
}

func TestT1048IndependentReviewObservationAndDisclosure(t *testing.T) {
	if flags := FlagFalseGreen(t1048Review(), nil); len(flags) != 0 {
		t.Fatalf("honest review flagged: %+v", flags)
	}
}

func TestT1048FramingMutationsRemainFlagged(t *testing.T) {
	base := t1048Review()
	cases := []struct {
		name, report string
		want         FlagKind
	}{
		{"killed-claimed-pass", strings.Replace(base, "it is not an absent or passing record", "I'm calling it passed", 1), FlagAttestationKilled},
		{"killed-called-green-with-disclaimer", strings.Replace(base, "it is not an absent or passing record", "it is not an absent or passing record, but it is green", 1), FlagAttestationKilled},
		{"killed-claimed-failing-test", strings.Replace(base, "it is not an absent or passing record", "it proves a failing test assertion", 1), FlagAttestationKilled},
		{"red-claimed-pass", strings.Replace(base, "It does prevent claiming the entire `internal/gate` package is green", "I'm calling it passed", 1), FlagAttestationNotGreen},
		{"real-failure-next-to-green", strings.Replace(base, "The command was `go test ./internal/mcpserver -count=1`.", "The command was `go test ./internal/mcpserver -count=1`.\n    --- FAIL: TestActual (0.00s)", 1), FlagOutputContradicts},
		{"unlabelled-killed", strings.Replace(base, "**SIGKILL record:**", "**Run:**", 1), FlagAttestationKilled},
		{"unlabelled-red", strings.Replace(base, "**RED disclosure:**", "**Run:**", 1), FlagAttestationNotGreen},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flags := FlagFalseGreen(tc.report, nil)
			if !hasKind(flags, tc.want) {
				t.Fatalf("flags=%v, want %s", kinds(flags), tc.want)
			}
		})
	}
}

// A disclaimer about one package must not turn an explicit pass claim for the
// cited RED into a disclosure. The T603 wording includes "package is green"
// under negation, so rejecting every green token would break the real report.
func TestT1048DisclosureCannotLaunderExplicitPass(t *testing.T) {
	base := t1048Review()
	for _, tc := range []struct{ name, addition string }{
		{"calling-it-green", " I am calling it green."},
		{"this-run-passed", " This run passed."},
		{"counts-as-pass", " This RED counts as a pass."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := strings.Replace(base, "It does prevent claiming", tc.addition+" It does prevent claiming", 1)
			if flags := FlagFalseGreen(report, nil); !hasKind(flags, FlagAttestationNotGreen) {
				t.Fatalf("RED falsely exempted: %v", kinds(flags))
			}
		})
	}
}

func TestT1048SameLineFailureNotHiddenByDisclosure(t *testing.T) {
	report := strings.Replace(t1048Review(), "**RED disclosure:**", "**RED disclosure:** `--- FAIL: TestGreenPath (0.00s)`", 1)
	if flags := FlagFalseGreen(report, nil); !hasKind(flags, FlagOutputContradicts) {
		t.Fatalf("same-line failure laundered: %v", kinds(flags))
	}
}

// Frozen verbatim from stored report 20261010T044824Z-3792be42, read via
// jevons_agent_report_read. The smaller fixture above isolates the classifier;
// this pins the actual report so surrounding prose cannot change its outcome.
func TestT1048StoredT603Report(t *testing.T) {
	body, err := os.ReadFile("testdata/t1048_t603_stored_report.txt")
	if err != nil {
		t.Fatal(err)
	}
	if flags := FlagFalseGreen(string(body), nil); len(flags) != 0 {
		t.Fatalf("stored honest review flagged: %v", flags)
	}
}
