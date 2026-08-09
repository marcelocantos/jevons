// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agentreport

import (
	"strings"
	"testing"
	"time"
)

// incidentReport builds a fixture in the shape of the three reports that were
// silently cut on 2026-08-09: a long body of working notes, with the
// conclusions and the asks at the END. The tail is what the overseer needed
// and what the pre-fix head-only cut destroyed, so every assertion below is
// about the tail surviving or its loss being visible.
func incidentReport(bodyKB int) string {
	var b strings.Builder
	b.WriteString("# jv-t372-auto — EC-6 harness inventory\n\n")
	b.WriteString("## 1. What I did\n\n")
	for b.Len() < bodyKB*1024 {
		b.WriteString("Walked the fork inventory entry by entry, checking each claim ")
		b.WriteString("against the source rather than against the summary table.\n")
	}
	b.WriteString("\n## 2. Correction — this changes the EC-6 ruling\n\n")
	b.WriteString("The inventory doc (§2.3 F-HARNESS-1) tells the opposite story ")
	b.WriteString("to the one I put in the summary: the harness fork is load-bearing.\n")
	b.WriteString("\n## 3. Asks\n\n")
	b.WriteString("- Do not let the owner rule on EC-6 using my earlier premise.\n")
	b.WriteString("- MatchTurn needs an owner decision before I go further.\n")
	return b.String()
}

// The load-bearing assertion of 🎯T388 acceptance 1 and 4: an over-bound
// report is never silently cut. It either arrives whole or arrives with an
// explicit marker AND a usable retrieval handle.
//
// RED against the pre-fix tree: the old path produced text[:1997]+"..." — no
// marker prefix, no handle, and the tail (asks, correction) absent. Each of
// the three checks below fails on that output independently.
func TestOverBoundReportIsNeverSilentlyCut(t *testing.T) {
	report := incidentReport(8)
	h := Handle{Agent: "jv-t372-auto", ReportID: "20260809T101500Z-deadbeef"}

	got := Elide(report, DeliveryBound, h)

	if !got.Truncated {
		t.Fatalf("an %d-byte report must not claim to fit a %d-byte bound", len(report), DeliveryBound)
	}
	if !IsTruncatedDelivery(got.Text) {
		t.Errorf("delivered text carries no truncation marker — this is the silent cut 🎯T388 exists to eliminate:\n%s", got.Text)
	}
	// The handle must be present and pasteable: visibility without
	// recoverability still loses the content.
	if !strings.Contains(got.Text, "jevons_agent_report_read") ||
		!strings.Contains(got.Text, h.Agent) ||
		!strings.Contains(got.Text, h.ReportID) {
		t.Errorf("marker names no retrieval handle; overseer cannot fetch the rest:\n%s", got.Text)
	}
	// The structural point: conclusions and asks live at the END, so the tail
	// must survive an elision that removes the middle.
	for _, want := range []string{
		"MatchTurn needs an owner decision",
		"Do not let the owner rule on EC-6",
		"## 3. Asks",
	} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("tail lost from delivery (%q missing) — a head-only cut eats exactly the region worth sending", want)
		}
	}
	// And the head is still there, so the reader has both ends.
	if !strings.Contains(got.Text, "# jv-t372-auto — EC-6 harness inventory") {
		t.Errorf("head lost from delivery")
	}
	if got.ElidedBytes <= 0 || got.ElidedBytes >= got.TotalBytes {
		t.Errorf("elided byte count %d is not a sane fraction of %d", got.ElidedBytes, got.TotalBytes)
	}
}

// Over-broadness mutation guard: a normal short report must come through
// byte-identical, with no marker and no retrieval handle. A "fix" that marks
// everything as truncated, or that attaches a handle to every delivery, fails
// here. This is the counterweight to the test above — together they pin the
// bound rather than just demanding chrome.
func TestShortReportAcquiresNoMarkerAndNoHandle(t *testing.T) {
	short := "Landed 🎯T999 at abc1234.\n\n`go test ./internal/foo/` — ok.\n\nNo asks.\n"
	h := Handle{Agent: "jv-t999-x", ReportID: "20260809T101500Z-cafebabe"}

	got := Elide(short, DeliveryBound, h)

	if got.Truncated {
		t.Fatalf("a %d-byte report must not be truncated at a %d-byte bound", len(short), DeliveryBound)
	}
	if got.Text != short {
		t.Errorf("short report was modified in delivery:\n got: %q\nwant: %q", got.Text, short)
	}
	if IsTruncatedDelivery(got.Text) {
		t.Errorf("short report acquired a truncation marker it does not need")
	}
	if strings.Contains(got.Text, "jevons_agent_report_read") {
		t.Errorf("short report acquired a retrieval handle it does not need")
	}
	if got.ElidedBytes != 0 {
		t.Errorf("ElidedBytes = %d, want 0", got.ElidedBytes)
	}
}

// A report exactly at the bound is whole; one byte over is marked. Pins the
// boundary so a later edit cannot drift it silently in either direction.
func TestBoundaryExactFitVersusOneOver(t *testing.T) {
	h := Handle{Agent: "a", ReportID: "b"}
	exact := strings.Repeat("x", DeliveryBound)
	if e := Elide(exact, DeliveryBound, h); e.Truncated {
		t.Errorf("report of exactly %d bytes must not be truncated", DeliveryBound)
	}
	over := strings.Repeat("x", DeliveryBound+1)
	if e := Elide(over, DeliveryBound, h); !e.Truncated {
		t.Errorf("report of %d bytes must be truncated at a %d-byte bound", DeliveryBound+1, DeliveryBound)
	}
}

// Multi-byte runes must never be cut mid-character: the fleet writes 🎯 and —
// constantly, and a mangled byte at the seam is a second kind of corruption.
func TestElisionCutsOnRuneBoundaries(t *testing.T) {
	report := strings.Repeat("🎯T388 — report — ", 400)
	got := Elide(report, DeliveryBound, Handle{Agent: "a", ReportID: "b"})
	if !got.Truncated {
		t.Fatalf("fixture should exceed the bound")
	}
	if !utf8Valid(got.Text) {
		t.Errorf("delivered text is not valid UTF-8 — a rune was cut in half")
	}
}

func utf8Valid(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

// Acceptance 2: the full terminal report outlives the agent. The store is
// keyed by name on disk and consults no registry, so a read after
// deregistration returns the whole text — the guarantee that was previously
// supplied by luck (jv-t372-auto had happened to commit its reasoning to a
// design doc).
func TestStoredReportSurvivesDeregistration(t *testing.T) {
	dir := t.TempDir()
	report := incidentReport(8)

	rec, err := Save(dir, "jv-t372-auto", report, time.Date(2026, 8, 9, 10, 15, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Nothing here knows whether the agent is running; that is the point.
	got, err := Latest(dir, "jv-t372-auto")
	if err != nil {
		t.Fatalf("Latest after deregistration: %v", err)
	}
	if got.Text != report {
		t.Errorf("stored report differs from what was written (%d vs %d bytes)", len(got.Text), len(report))
	}
	byID, err := Load(dir, "jv-t372-auto", rec.ID)
	if err != nil {
		t.Fatalf("Load by id: %v", err)
	}
	if byID.Text != report {
		t.Errorf("load-by-id returned different bytes")
	}
	// The elided delivery must point at a handle that actually resolves.
	e := Elide(report, DeliveryBound, rec.Handle())
	if !strings.Contains(e.Text, rec.ID) {
		t.Errorf("delivery handle does not name the stored record id")
	}
}

// Acceptance 3: a section comes back cut from the stored bytes, not
// re-derived. Re-derivation is the failure mode — an agent asked to "resend
// the last bit" runs its model again and can rewrite the very correction the
// overseer was chasing.
func TestSectionRetrievalReturnsStoredBytes(t *testing.T) {
	dir := t.TempDir()
	report := incidentReport(8)
	if _, err := Save(dir, "jv-t372-auto", report, time.Now()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	rec, err := Latest(dir, "jv-t372-auto")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}

	sec, err := FindSection(rec.Text, "asks")
	if err != nil {
		t.Fatalf("FindSection: %v", err)
	}
	if !strings.Contains(sec.Text, "MatchTurn needs an owner decision") {
		t.Errorf("asks section did not carry its content:\n%s", sec.Text)
	}
	if !strings.Contains(report, sec.Text) {
		t.Errorf("returned section is not a verbatim slice of the stored report")
	}

	corr, err := FindSection(rec.Text, "Correction")
	if err != nil {
		t.Fatalf("FindSection(Correction): %v", err)
	}
	if !strings.Contains(corr.Text, "§2.3 F-HARNESS-1") {
		t.Errorf("correction section missing its load-bearing sentence")
	}

	if _, err := FindSection(rec.Text, "no such heading"); err == nil {
		t.Errorf("a missing section must be an error naming what is available, not empty text")
	}
}

func TestSectionsSplitOnHeadings(t *testing.T) {
	secs := Sections("intro text\n\n## One\nbody one\n\n### Two\nbody two\n")
	if len(secs) != 3 {
		t.Fatalf("got %d sections, want 3: %v", len(secs), HeadingList(secs))
	}
	if secs[0].Heading != preambleHeading || secs[1].Heading != "One" || secs[2].Heading != "Two" {
		t.Errorf("headings = %v", HeadingList(secs))
	}
	if secs[2].Level != 3 {
		t.Errorf("level = %d, want 3", secs[2].Level)
	}
	// A report with no headings is still one addressable section.
	if got := Sections("just prose"); len(got) != 1 || got[0].Heading != preambleHeading {
		t.Errorf("headingless report = %v", HeadingList(got))
	}
}

// An agent name is free-form and arrives from the fleet layer, so the store
// must refuse anything that could escape its root.
func TestStoreRefusesUnsafeAgentNames(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"", "..", "../../etc", "a/b", `a\b`} {
		if _, err := Save(dir, bad, "text", time.Now()); err == nil {
			t.Errorf("Save accepted unsafe agent name %q", bad)
		}
	}
	// 🎯T197 names with literal dots are legitimate and must work.
	if _, err := Save(dir, "jv-t27.2-config", "text", time.Now()); err != nil {
		t.Errorf("Save rejected a legitimate dotted worker name: %v", err)
	}
}

// A never-reported agent is a normal state, not a fault.
func TestListMissingAgentIsEmptyNotError(t *testing.T) {
	got, err := List(t.TempDir(), "never-reported")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d records, want 0", len(got))
	}
}
