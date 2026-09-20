// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ownergate

import (
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
	if got := LedgerDay(when); got != "2026-09-21" {
		t.Fatalf("LedgerDay = %q, want 2026-09-21 (AEST); UTC would stamp %s", got, utcDay)
	}
	if LedgerDay(when) == utcDay {
		t.Fatal("LedgerDay collapsed onto UTC — the T711 two-date write is back")
	}
}

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
