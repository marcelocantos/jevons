// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package idea

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeDisposition(t *testing.T) {
	cases := map[string]Disposition{
		"file":             File,
		"product-shaped":   File,
		"park":             Park,
		"needs-owner":      Park,
		"design-discussion": Park,
		"hold":             Hold,
		"life-domain":      Hold,
		"drop":             Drop,
		"ignore":           Drop,
		"inbox":            Inbox,
		"":                 Inbox,
		"garbage":          Inbox,
	}
	for in, want := range cases {
		if got := NormalizeDisposition(in); got != want {
			t.Errorf("NormalizeDisposition(%q)=%q want %q", in, got, want)
		}
	}
}

func TestParkedLifeDomainHold(t *testing.T) {
	if !IsParkedLifeDomain("health") {
		t.Fatal("health should be parked")
	}
	if !IsParkedLifeDomain("SWOT") {
		t.Fatal("SWOT should be parked")
	}
	if IsParkedLifeDomain("jevons") {
		t.Fatal("jevons product should not be life-parked")
	}
	rec, err := NewRecord(CaptureArgs{
		Text:   "track blood pressure trends",
		Source: SourceIdea,
		Domain: "health",
		Now:    time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC),
		ID:     "idea-test-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Disposition != Hold {
		t.Fatalf("parked domain → hold, got %s", rec.Disposition)
	}
	if NextCeremony(Hold) == "" {
		t.Fatal("NextCeremony empty")
	}
}

func TestSuggestDispositionProductHints(t *testing.T) {
	if got := SuggestDisposition("fix daemon restart thrash", ""); got != File {
		t.Fatalf("product hint → file, got %s", got)
	}
	if got := SuggestDisposition("maybe a holiday", ""); got != Inbox {
		t.Fatalf("neutral → inbox, got %s", got)
	}
	if got := SuggestDisposition("cat-flap camera", "hardware"); got != Hold {
		t.Fatalf("hardware domain → hold, got %s", got)
	}
}

func TestCaptureListTriageOracle(t *testing.T) {
	// Oracle 🎯T325.3: captured idea appears in listable surface within ceremony.
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 15, 30, 0, 0, time.UTC)
	rec, err := st.Capture(CaptureArgs{
		Text:    "spark: add idea ledger so thoughts don't die in scrollback",
		Source:  SourceCapture,
		AsideID: "att-test-capture",
		Now:     now,
		ID:      "idea-oracle-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.ID != "idea-oracle-1" {
		t.Fatalf("id=%s", rec.ID)
	}
	if rec.Disposition != Inbox {
		t.Fatalf("disposition=%s", rec.Disposition)
	}
	if rec.Title == "" {
		t.Fatal("title empty")
	}

	list, err := st.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len=%d", len(list))
	}
	if list[0].ID != "idea-oracle-1" {
		t.Fatalf("listed id=%s", list[0].ID)
	}
	if list[0].Text != rec.Text {
		t.Fatal("text mismatch on list")
	}

	// Triage product-shaped → file
	updated, err := st.Triage(TriageArgs{
		ID:          "idea-oracle-1",
		Disposition: File,
		Note:        "product-shaped T325.3 path",
		TargetID:    "T999",
		Now:         now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Disposition != File {
		t.Fatalf("after triage: %s", updated.Disposition)
	}
	if updated.TargetID != "T999" {
		t.Fatalf("target_id=%s", updated.TargetID)
	}
	if updated.TriagedAt == "" {
		t.Fatal("triaged_at empty")
	}

	filed, err := st.List("file")
	if err != nil {
		t.Fatal(err)
	}
	if len(filed) != 1 {
		t.Fatalf("file filter len=%d", len(filed))
	}

	// Malformed state is hard error (no silent reset).
	badPath := filepath.Join(dir, "ideas.json")
	if err := writeRaw(badPath, "{not json"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.List(""); err == nil {
		t.Fatal("expected parse error on malformed ledger")
	}
}

func TestApplyTriageUnknownDisposition(t *testing.T) {
	rec := Record{ID: "x", Text: "y", Disposition: Inbox}
	if _, err := ApplyTriage(rec, TriageArgs{ID: "x", Disposition: "nope"}); err == nil {
		t.Fatal("expected unknown disposition error")
	}
}

func TestEmptyTextRejected(t *testing.T) {
	if _, err := NewRecord(CaptureArgs{Text: "  "}); err == nil {
		t.Fatal("expected empty text error")
	}
}

func writeRaw(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}
