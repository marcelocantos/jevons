// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package research

import (
	"strings"
	"testing"
	"time"
)

func at(min int) time.Time {
	return time.Date(2026, 8, 9, 10, min, 0, 0, time.UTC)
}

func TestApplyAddsFindingsOnFirstRevision(t *testing.T) {
	note, delta, err := Apply(Note{}, RevisionInput{
		Topic:   "repo/jevons",
		Title:   "Repository activity: jevons",
		Trigger: "test",
		At:      at(0),
		Findings: []Finding{
			{Key: "scope:internal/rsi", Claim: "steady activity in internal/rsi"},
			{Key: "scope:web", Claim: "light activity in web"},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !delta.Changed || delta.Revision != 1 {
		t.Fatalf("want changed rev 1, got changed=%v rev=%d", delta.Changed, delta.Revision)
	}
	if len(delta.Added) != 2 || len(note.Revisions) != 1 {
		t.Fatalf("want 2 added and 1 revision, got %d added, %d revisions", len(delta.Added), len(note.Revisions))
	}
	if note.ID != "repo-jevons" {
		t.Fatalf("want slug repo-jevons, got %q", note.ID)
	}
	if got := len(note.CurrentFindings()); got != 2 {
		t.Fatalf("want 2 current findings, got %d", got)
	}
}

// A cycle that re-observes the same context must not manufacture a revision:
// bounded cycles, not permanent monologue (🎯T356 acceptance #3).
func TestApplyUnchangedObservationWritesNoRevision(t *testing.T) {
	in := RevisionInput{
		Topic:    "repo/jevons",
		Trigger:  "test",
		At:       at(0),
		Findings: []Finding{{Key: "scope:web", Claim: "light activity in web", Evidence: []string{"2 commits"}}},
	}
	note, _, err := Apply(Note{}, in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	in.At = at(30)
	in.Findings[0].Evidence = []string{"2 commits", "head abc123"}
	note, delta, err := Apply(note, in)
	if err != nil {
		t.Fatalf("Apply repeat: %v", err)
	}
	if delta.Changed {
		t.Fatalf("re-observation must not count as change: %+v", delta)
	}
	if len(note.Revisions) != 1 {
		t.Fatalf("want 1 revision after re-observation, got %d", len(note.Revisions))
	}
	if len(delta.Confirmed) != 1 {
		t.Fatalf("want 1 confirmed, got %d", len(delta.Confirmed))
	}
	f := note.CurrentFindings()[0]
	if f.SeenCount != 2 {
		t.Fatalf("want seen_count 2, got %d", f.SeenCount)
	}
	if len(f.Evidence) != 2 {
		t.Fatalf("want merged evidence, got %v", f.Evidence)
	}
	if note.CheckedAt != stamp(at(30)) {
		t.Fatalf("checked_at must advance even when quiet, got %s", note.CheckedAt)
	}
}

// 🎯T356 acceptance #2: a changed claim supersedes its predecessor explicitly,
// with both conclusions retained and provenance intact.
func TestApplySupersedesPriorConclusionExplicitly(t *testing.T) {
	note, _, err := Apply(Note{}, RevisionInput{
		Topic:    "repo/jevons",
		Trigger:  "test",
		At:       at(0),
		Findings: []Finding{{Key: "scope:internal/rsi", Claim: "light activity in internal/rsi; head aaaa1111"}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	note, delta, err := Apply(note, RevisionInput{
		Topic:    "repo/jevons",
		Trigger:  "schedule",
		At:       at(90),
		Findings: []Finding{{Key: "scope:internal/rsi", Claim: "heavy activity in internal/rsi; head bbbb2222"}},
	})
	if err != nil {
		t.Fatalf("Apply supersede: %v", err)
	}
	if !delta.Changed || len(delta.Superseded) != 1 {
		t.Fatalf("want one supersession, got %+v", delta)
	}
	s := delta.Superseded[0]
	if !strings.Contains(s.PriorClaim, "aaaa1111") || !strings.Contains(s.NewClaim, "bbbb2222") {
		t.Fatalf("supersession must carry both claims: %+v", s)
	}
	if len(note.Findings) != 2 {
		t.Fatalf("prior conclusion must be retained, got %d findings", len(note.Findings))
	}
	current := note.CurrentFindings()
	if len(current) != 1 || !strings.Contains(current[0].Claim, "bbbb2222") {
		t.Fatalf("want single current claim on the new head, got %+v", current)
	}
	if current[0].FirstSeenRev != 1 {
		t.Fatalf("supersession must preserve first_seen_rev, got %d", current[0].FirstSeenRev)
	}
	var old Finding
	for _, f := range note.Findings {
		if f.Status == StatusSuperseded {
			old = f
		}
	}
	if old.SupersededBy != 2 || old.SupersededAt == "" {
		t.Fatalf("superseded finding must record by/at: %+v", old)
	}
	md := RenderNote(note)
	for _, want := range []string{"Superseded conclusions", "aaaa1111", "bbbb2222", "Revision history"} {
		if !strings.Contains(md, want) {
			t.Fatalf("render missing %q:\n%s", want, md)
		}
	}
}

func TestApplyTrimsRevisionsAndCountsDrops(t *testing.T) {
	note := NewNote("repo/churn", "", at(0))
	for i := range MaxRevisions + 5 {
		var err error
		note, _, err = Apply(note, RevisionInput{
			Topic:    "repo/churn",
			Trigger:  "test",
			At:       at(i),
			Findings: []Finding{{Key: "scope:x", Claim: "claim " + string(rune('a'+i%26)) + string(rune('0'+i/26))}},
		})
		if err != nil {
			t.Fatalf("Apply %d: %v", i, err)
		}
	}
	if len(note.Revisions) != MaxRevisions {
		t.Fatalf("want %d revisions retained, got %d", MaxRevisions, len(note.Revisions))
	}
	if note.DroppedRevisions != 5 {
		t.Fatalf("want 5 dropped revisions recorded, got %d", note.DroppedRevisions)
	}
}

func TestSlugIsStableAndSafe(t *testing.T) {
	cases := map[string]string{
		"repo/jevons":            "repo-jevons",
		"context/frontier":       "context-frontier",
		"feed/xAI Blog!!":        "feed-xai-blog",
		"   ":                    "note",
		strings.Repeat("x", 200): strings.Repeat("x", 80),
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Fatalf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}
