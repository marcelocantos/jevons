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
// Hermetic: ScanOutput markers are panic:/FAIL/DATA RACE/timeout is clean;
// a report quoting an actual --- FAIL from its own run is flagged; a
// checker that flags any occurrence of a marker anywhere goes RED.

const t737CatalogLine = "ScanOutput markers are panic:/FAIL/DATA RACE/timeout"

func t737MarkerProseReport() string {
	return strings.Join([]string{
		"🎯T737 done, make test-go is green.",
		"",
		"jevons: fog-known \"verdictFor then applyTreeVerdict. DIRTY demotes only GREEN. " +
			t737CatalogLine + ". Empty is not scanned today.\"",
		"",
		"Acceptance: output has no execution evidence (=== RUN, --- PASS:, --- SKIP:, " +
			"--- FAIL:, an ok line without the empty suffix).",
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
		{"catalog", t737CatalogLine, true},
		{"fog-known specimen", "jevons: fog-known \"" + t737CatalogLine + "\"", true},
		{"acceptance list", "output has no execution evidence (=== RUN, --- PASS:, --- SKIP:, --- FAIL:, an ok line)", true},
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
	if got := ScanOutput(t737CatalogLine); len(got) != 0 {
		t.Fatalf("catalog scanned as output: %v", got)
	}
	list := "output has no execution evidence (=== RUN, --- PASS:, --- SKIP:, --- FAIL:, an ok line)"
	if got := ScanOutput(list); len(got) != 0 {
		t.Fatalf("acceptance list scanned as output: %v", got)
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
