// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"strings"
	"testing"
)

// 🎯T737. The 🎯T386 false-green check fired three output_contradicts flags
// on jv-t719-empty-run's scout report. All three matched text in which the
// seat was describing the checker, not quoting a failure: a fog-known line
// listing ScanOutput's markers, and an acceptance clause listing execution
// evidence. Treating a name catalog as quoted output teaches workers to
// stop writing the reports that engage the verification machinery.
//
// Seam: a separate pass from 🎯T722 RoleControl. RoleControl classifies a
// cited RED. The specimen cited none — it named the markers in prose —
// so extending that role would leave the scout flagged. ScanOutput now
// requires output shape instead.
//
// Hermetic: the verbatim fog-known line below is clean; a report quoting
// an actual --- FAIL from its own run is flagged; a checker that flags
// any occurrence of a marker anywhere goes RED. Dirty-tree catches must
// still fire (discrimination, not permissiveness).

// t737FogKnownSpecimen is the fog-known line that produced two of the
// three flags, byte for byte from the T737 filing.
const t737FogKnownSpecimen = "verdictFor then applyTreeVerdict. DIRTY demotes only GREEN. ScanOutput markers are panic:/FAIL/DATA RACE/timeout. Empty is not scanned today."

// t737AcceptanceSpecimen is the acceptance clause that produced the
// third flag. The ellipsis is the filing's, not three ASCII dots.
const t737AcceptanceSpecimen = "output has no execution evidence (=== RUN, --- PASS:, --- SKIP:, --- FAIL:, an ok line without the empty suffix…)"

func t737MarkerProseReport() string {
	return strings.Join([]string{
		"🎯T737 done, make test-go is green.",
		"",
		t737FogKnownSpecimen,
		"",
		t737AcceptanceSpecimen,
		"",
		"Landed as abc1234.",
	}, "\n")
}

func TestT737LooksLikeMarkerProse(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want bool
	}{
		{"catalog", "ScanOutput markers are panic:/FAIL/DATA RACE/timeout", true},
		{"verbatim fog-known", t737FogKnownSpecimen, true},
		{"verbatim acceptance", t737AcceptanceSpecimen, true},
		{"runtime panic", "    panic: test timed out after 10m0s", false},
		{"go-test fail", "    --- FAIL: TestT737 (0.02s)", false},
		{"race warning", "WARNING: DATA RACE", false},
		{"no markers", "make test-go is green.", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := LooksLikeMarkerProse(tc.line); got != tc.want {
				t.Fatalf("LooksLikeMarkerProse(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestT737ScanOutputIgnoresNameCatalog(t *testing.T) {
	if got := ScanOutput(t737FogKnownSpecimen); len(got) != 0 {
		t.Fatalf("verbatim fog-known scanned as output: %v", got)
	}
	if got := ScanOutput(t737AcceptanceSpecimen); len(got) != 0 {
		t.Fatalf("verbatim acceptance scanned as output: %v", got)
	}
}

func TestT737ScanOutputStillSeesQuotedFailures(t *testing.T) {
	for _, tc := range []struct{ name, out, marker string }{
		{"panic", "panic: test timed out after 10m0s\n", "panic:"},
		{"fail", "--- FAIL: TestT737 (0.02s)\n", "--- FAIL"},
		{"race", "WARNING: DATA RACE\nRead at 0x1\n", "DATA RACE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanOutput(tc.out)
			if len(got) == 0 {
				t.Fatal("quoted output produced no anomaly")
			}
			hit := false
			for _, a := range got {
				if a.Marker == tc.marker {
					hit = true
				}
			}
			if !hit {
				t.Fatalf("anomalies %v missing marker %q", got, tc.marker)
			}
		})
	}
}

func TestT737CatalogReportIsNotFlagged(t *testing.T) {
	report := t737MarkerProseReport()
	if !strings.Contains(report, "panic:") || !strings.Contains(report, "DATA RACE") ||
		!strings.Contains(report, "--- FAIL") {
		t.Fatal("fixture no longer contains the markers this target is about")
	}
	flags := FlagFalseGreen(report, nil)
	if hasKind(flags, FlagOutputContradicts) {
		t.Fatalf("marker-prose report flagged output_contradicts: %v\n%s", kinds(flags), Banner(flags))
	}
	if len(flags) != 0 {
		t.Fatalf("marker-prose report flagged %v:\n%s", kinds(flags), Banner(flags))
	}
}

func TestT737QuotedFailFromOwnRunIsFlagged(t *testing.T) {
	report := strings.Join([]string{
		"🎯T737 done, make test-go is green.",
		"",
		"    --- FAIL: TestT737 (0.02s)",
		"",
		"Calling it a pass.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if !hasKind(flags, FlagOutputContradicts) {
		t.Fatalf("flags = %v, want %s for a quoted --- FAIL from the run", kinds(flags), FlagOutputContradicts)
	}
}

func TestT737ProseDoesNotHideARealFailOnTheSameReport(t *testing.T) {
	report := t737MarkerProseReport() + "\n\n    --- FAIL: TestT737 (0.02s)\n"
	flags := FlagFalseGreen(report, nil)
	if !hasKind(flags, FlagOutputContradicts) {
		t.Fatalf("flags = %v, want %s — specimen prose must not blanket-skip a real fail", kinds(flags), FlagOutputContradicts)
	}
}

func TestT737DirtyTreeCatchStillFires(t *testing.T) {
	// Discrimination, not permissiveness: five dirty-tree catches tonight
	// were correct. Naming ScanOutput's markers must not swallow T397.
	report := strings.Join([]string{
		"🎯T737 done, make test-go is green.",
		"",
		t737FogKnownSpecimen,
		"",
		"GATE t737-suite exit=0 DIRTY id=aaaaaaaa out=bbbbbbbbbbbb dur=1s",
		"",
		"Landed as abc1234.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if hasKind(flags, FlagOutputContradicts) {
		t.Fatalf("specimen flagged output_contradicts: %v\n%s", kinds(flags), Banner(flags))
	}
	if !hasKind(flags, FlagDirtyTreeGate) {
		t.Fatalf("flags = %v, want %s — T737 must not swallow dirty-tree catches", kinds(flags), FlagDirtyTreeGate)
	}
}

func TestT737QuotedPanicStillFlagged(t *testing.T) {
	flags := FlagFalseGreen(reportGreenOverPanic, nil)
	if !hasKind(flags, FlagOutputContradicts) {
		t.Fatalf("flags = %v, want %s — T386 green-over-panic must stay flagged", kinds(flags), FlagOutputContradicts)
	}
}

func TestT737MutationFlaggingAnyOccurrenceGoesRed(t *testing.T) {
	// The mutation this file exists to catch: treating any occurrence of
	// panic: / DATA RACE / --- FAIL as a contradiction. The catalog report
	// contains all three as substrings and must stay unflagged; if a future
	// edit flags every occurrence, this fails.
	report := t737MarkerProseReport()
	for _, m := range []string{"panic:", "DATA RACE", "--- FAIL"} {
		if !strings.Contains(report, m) {
			t.Fatalf("fixture no longer contains %q", m)
		}
	}
	flags := FlagFalseGreen(report, nil)
	if len(flags) != 0 {
		t.Fatalf("mutation (flag any marker occurrence) would look like %v:\n%s", kinds(flags), Banner(flags))
	}
}
