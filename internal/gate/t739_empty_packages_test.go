// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"reflect"
	"strings"
	"testing"
)

// 🎯T739: a partly-empty multi-package gate names the empty packages.

const t739Mixed = "ok  \texample.com/jevons/a\t0.15s\n" +
	"ok  \texample.com/jevons/b\t0.01s [no tests to run]\n" +
	"?   \texample.com/jevons/c\t[no test files]\n"

func TestT739EmptyPackagesParse(t *testing.T) {
	got := EmptyPackages(t739Mixed)
	want := []string{"example.com/jevons/b", "example.com/jevons/c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EmptyPackages = %v, want %v", got, want)
	}
	if got := EmptyPackages("ok\n"); got != nil {
		t.Fatalf("non-Go output yielded %v", got)
	}
}

func TestT739PartlyEmptyRunIsGreenButNamesEmptyPackage(t *testing.T) {
	rec, _ := runIn(t, "sh", "-c",
		`printf 'ok  \texample.com/a\t0.15s\nok  \texample.com/b\t0.01s [no tests to run]\n'; exit 0`)
	if rec.Verdict != VerdictGreen {
		t.Fatalf("verdict=%s, want GREEN overall", rec.Verdict)
	}
	if want := []string{"example.com/b"}; !reflect.DeepEqual(rec.EmptyPackages, want) {
		t.Fatalf("empty set = %v, want exactly %v", rec.EmptyPackages, want)
	}
	if line := rec.Attestation(); !strings.Contains(line, "exit=0 GREEN") || !strings.Contains(line, "empty=example.com/b") {
		t.Fatalf("verdict line does not name the empty package: %s", line)
	}
	if !strings.Contains(rec.Summary(), "example.com/b") {
		t.Fatalf("summary omits empty package: %s", rec.Summary())
	}
	if cited := ParseAttestations(rec.Attestation()); len(cited) != 1 || cited[0].ID != rec.ID {
		t.Fatalf("token broke attestation parsing: %+v", cited)
	}
}

func TestT739AllEmptyStillEmptyVerdict(t *testing.T) {
	rec, _ := runIn(t, "sh", "-c",
		`printf 'ok  \texample.com/a\t0.1s [no tests to run]\nok  \texample.com/b\t0.1s [no tests to run]\n'; exit 0`)
	if rec.Verdict != VerdictEmpty {
		t.Fatalf("verdict=%s, want EMPTY (🎯T719)", rec.Verdict)
	}
	if len(rec.EmptyPackages) != 2 {
		t.Fatalf("empty set = %v", rec.EmptyPackages)
	}
}

func TestT739CheckerFlagsCitationOfEmptyPackage(t *testing.T) {
	rec, store := runIn(t, "sh", "-c",
		`printf 'ok  \texample.com/jevons/internal/a\t0.15s\nok  \texample.com/jevons/internal/b\t0.01s [no tests to run]\n'; exit 0`)
	lookup := store.Lookup
	report := "Oracle for the b behaviour: internal/b is covered.\n" + rec.Attestation() + "\n"
	if !hasKind(FlagFalseGreen(report, lookup), FlagAttestationEmptyPackage) {
		t.Fatalf("citation of a green gate for an empty package was not flagged")
	}
	// Controls: honest statement, and a report about the package that did run.
	honest := "internal/b has no tests in this run.\n" + rec.Attestation() + "\n"
	if hasKind(FlagFalseGreen(honest, lookup), FlagAttestationEmptyPackage) {
		t.Fatalf("honest no-tests line flagged")
	}
	other := "Behaviour in internal/a proven.\n" + rec.Attestation() + "\n"
	if hasKind(FlagFalseGreen(other, lookup), FlagAttestationEmptyPackage) {
		t.Fatalf("report about the non-empty package flagged")
	}
}
