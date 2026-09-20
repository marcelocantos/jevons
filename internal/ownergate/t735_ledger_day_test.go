// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ownergate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// aestT711Specimen is the live 🎯T711 instant: 2026-09-21 02:22 +1000, which
// is 2026-09-20 16:22 UTC. Stamping UTC names the 20th; bullseye's local
// stamp names the 21st.
func aestT711Specimen() (when time.Time, aest *time.Location) {
	aest = time.FixedZone("AEST", 10*3600)
	when = time.Date(2026, 9, 21, 2, 22, 0, 0, aest)
	return when, aest
}

func pinLocal(t *testing.T, loc *time.Location) {
	t.Helper()
	orig := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = orig })
}

// The 🎯T735 deliverable: a UTC-crossing local instant stamps the local
// calendar day, not the UTC day. Without this, one ledger write names two
// dates for the same instant.
func TestT735LedgerDayIsTheLocalCalendarNotUTC(t *testing.T) {
	when, aest := aestT711Specimen()
	pinLocal(t, aest)

	utcDay := when.UTC().Format("2006-01-02")
	if utcDay != "2026-09-20" {
		t.Fatalf("fixture is not the T711 specimen: UTC day = %s, want 2026-09-20", utcDay)
	}
	got := LedgerDay(when)
	if got != "2026-09-21 +1000" {
		t.Fatalf("LedgerDay = %q, want 2026-09-21 +1000 (AEST); UTC would stamp %s", got, utcDay)
	}
	if got == utcDay {
		t.Fatal("LedgerDay collapsed onto UTC — the T711 two-date write is back")
	}
	if bareDay.MatchString(got) {
		t.Fatalf("LedgerDay = %q is a bare date — a reader cannot tell local from the old UTC convention", got)
	}
}

var bareDay = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func TestT735ReasonStampsLocalDayOnUTCCrossingInstant(t *testing.T) {
	when, aest := aestT711Specimen()
	pinLocal(t, aest)

	rec := Record{
		Question:   "Does the live seat preempt an in-flight turn as intended?",
		Evidence:   "landed at 6e9da8f5; GATE id=45acfeb7 GREEN over TestT711",
		RecordedBy: "jevons-po",
		Now:        when,
	}
	reason, err := rec.Reason()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reason, "recorded 2026-09-20") {
		t.Fatalf("UTC date leaked into ledger prose: %q", reason)
	}
	if !strings.Contains(reason, "recorded 2026-09-21") {
		t.Fatalf("local date missing from ledger prose: %q", reason)
	}
}

// A reopen is one write that names both "recorded on" and "row achieved".
// Those two dates must agree when the achieve and the gate share an instant
// whose UTC day differs from the local day — the T711 specimen.
func TestT735ReopenReasonDoesNotNameTwoDaysForOneInstant(t *testing.T) {
	when, aest := aestT711Specimen()
	pinLocal(t, aest)

	r := Reopen{
		Record: Record{
			Question:   "Does the live seat preempt an in-flight turn as intended?",
			Evidence:   "landed at 6e9da8f5; GATE id=45acfeb7 GREEN over TestT711",
			RecordedBy: "jevons-po",
			Now:        when,
		},
		AchievedOn:  LedgerDay(when), // what bullseye stamps at that instant
		Attestation: "landed at 6e9da8f5",
	}
	reason, err := r.Reason()
	if err != nil {
		t.Fatal(err)
	}
	recorded := regexp.MustCompile(`recorded the 🎯T449 owner gate on (\d{4}-\d{2}-\d{2})`).FindStringSubmatch(reason)
	achieved := regexp.MustCompile(`against a row achieved (\d{4}-\d{2}-\d{2})`).FindStringSubmatch(reason)
	if recorded == nil || achieved == nil {
		t.Fatalf("reopen reason did not name both dates: %q", reason)
	}
	if recorded[1] != achieved[1] {
		t.Fatalf("one write, two dates: recorded %s vs achieved %s in %q", recorded[1], achieved[1], reason)
	}
	if recorded[1] != "2026-09-21" {
		t.Fatalf("shared date = %s, want 2026-09-21 (AEST); UTC would have been 2026-09-20", recorded[1])
	}

	gate, err := r.GateReason()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gate, "recorded 2026-09-20") || strings.Contains(gate, "achieved 2026-09-20") {
		t.Fatalf("UTC date leaked into the gate reason: %q", gate)
	}
	if !strings.Contains(gate, "recorded 2026-09-21") || !strings.Contains(gate, "was achieved 2026-09-21") {
		t.Fatalf("gate reason missing the local day: %q", gate)
	}
}

func TestT735RestoreAttestationStampsLocalDay(t *testing.T) {
	when, aest := aestT711Specimen()
	pinLocal(t, aest)

	att := RestoreAttestation(VerdictAccept, "looks right", "jevons-po", LedgerDay(when),
		"original attestation", when)
	if strings.Contains(att, "2026-09-20") {
		t.Fatalf("UTC date leaked into the restored attestation: %q", att)
	}
	if !strings.Contains(att, "answered 2026-09-21") {
		t.Fatalf("restored attestation missing the local answer day: %q", att)
	}
	answer := FormatAnswer(VerdictAccept, "", "jevons-po", when)
	if !strings.Contains(answer, "2026-09-21") || strings.Contains(answer, "2026-09-20") {
		t.Fatalf("FormatAnswer UTC-leaked: %q", answer)
	}
}

// Bullseye's `achieved` field is a bare local day. Copy it, do not restamp it:
// restamping would rewrite a historical field and is how a "normalise the
// ledger" pass would start.
func TestT735DoesNotRewriteBullseyeAchievedField(t *testing.T) {
	when, aest := aestT711Specimen()
	pinLocal(t, aest)

	r := Reopen{
		Record: Record{
			Question:   "Does the live seat preempt an in-flight turn as intended?",
			Evidence:   "landed at 6e9da8f5; GATE id=45acfeb7 GREEN over TestT711",
			RecordedBy: "jevons-po",
			Now:        when,
		},
		AchievedOn:  "2026-09-21", // bullseye field shape
		Attestation: "landed at 6e9da8f5",
	}
	reason, err := r.Reason()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reason, "on 2026-09-21 +1000") {
		t.Fatalf("jevons stamp missing offset: %q", reason)
	}
	if !strings.Contains(reason, "achieved 2026-09-21") {
		t.Fatalf("copied bullseye field missing: %q", reason)
	}
	if strings.Contains(reason, "achieved 2026-09-21 +1000") {
		t.Fatalf("rewrote the bullseye achieved field: %q", reason)
	}
}

var calendarDayFormatRe = regexp.MustCompile(`\.Format\("2006-01-02"\)`)

// The formatter family is one function. A new UTC().Format("2006-01-02") in
// production would re-introduce the T711 two-date write without touching
// ownergate tests.
func TestT735OnlyLedgerDayRendersLedgerCalendarDays(t *testing.T) {
	root := moduleRoot(t)
	var hits []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "ui", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		for i, line := range strings.Split(string(body), "\n") {
			if calendarDayFormatRe.MatchString(line) {
				hits = append(hits, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > 0 {
		t.Fatalf("production calendar-day Format must go through ownergate.LedgerDay; found:\n%s",
			strings.Join(hits, "\n"))
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
