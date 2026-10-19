// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func dispositionFixtureStore(t *testing.T) *DispositionStore {
	t.Helper()
	s, err := OpenDispositionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDispositionRecordSetMetrics(t *testing.T) {
	s := dispositionFixtureStore(t)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	js := []Judgment{
		{Fingerprint: "fp-a", Name: "gap A", Observation: "obs A", Severity: "high"},
		{Fingerprint: "fp-b", Name: "gap B", Observation: "obs B", Severity: "medium"},
		{Fingerprint: "fp-c", Name: "gap C", Observation: "obs C", Severity: "low"},
	}
	if err := s.RecordDelivered(js, now); err != nil {
		t.Fatal(err)
	}

	if _, err := s.SetDisposition(SetDispositionArgs{
		Fingerprint: "fp-a", Disposition: DispositionFile,
		TargetID: "🎯T400", TargetCwd: "/repo", Evidence: "session-1", Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDisposition(SetDispositionArgs{
		Fingerprint: "fp-b", Disposition: DispositionIgnoreWithReason,
		Reason: "owner venting, one-off", Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	m, err := s.Metrics(now, 14*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if m.Total != 3 || m.Filed != 1 || m.Ignored != 1 || m.Pending != 1 {
		t.Fatalf("metrics = %+v, want total=3 filed=1 ignored=1 pending=1", m)
	}

	entries, err := s.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var filed *DispositionEntry
	for i := range entries {
		if entries[i].Fingerprint == "fp-a" {
			filed = &entries[i]
		}
	}
	if filed == nil || filed.TargetID != "T400" || filed.TargetCwd != "/repo" || filed.Evidence != "session-1" {
		t.Fatalf("filed entry wrong: %+v", filed)
	}
	if filed.DispositionAt.IsZero() || filed.DeliveredAt.IsZero() {
		t.Fatalf("timestamps missing: %+v", filed)
	}

	// Outside the window, nothing counts.
	m2, err := s.Metrics(now.Add(30*24*time.Hour), 14*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Total != 0 {
		t.Fatalf("expected empty window, got %+v", m2)
	}
}

func TestDispositionValidation(t *testing.T) {
	s := dispositionFixtureStore(t)
	if _, err := s.SetDisposition(SetDispositionArgs{Fingerprint: "fp", Disposition: "shrug"}); err == nil {
		t.Fatal("want error for unknown disposition")
	}
	if _, err := s.SetDisposition(SetDispositionArgs{Fingerprint: "fp", Disposition: DispositionIgnoreWithReason}); err == nil {
		t.Fatal("want error for ignore without reason")
	}
	if _, err := s.SetDisposition(SetDispositionArgs{Fingerprint: "fp", Disposition: DispositionFile}); err == nil {
		t.Fatal("want error for file without target_id")
	}
	if _, err := s.SetDisposition(SetDispositionArgs{Disposition: DispositionPark}); err == nil {
		t.Fatal("want error for missing fingerprint")
	}
	// Unknown fingerprint with valid args is accepted (judgment may predate store).
	if _, err := s.SetDisposition(SetDispositionArgs{Fingerprint: "fp-new", Disposition: DispositionPark}); err != nil {
		t.Fatal(err)
	}
}

func TestDispositionMalformedIsHardError(t *testing.T) {
	s := dispositionFixtureStore(t)
	if err := os.WriteFile(s.Path(), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Entries(); err == nil {
		t.Fatal("want hard error on malformed store, got nil")
	}
	if err := s.RecordDelivered([]Judgment{{Fingerprint: "x"}}, time.Now()); err == nil {
		t.Fatal("want hard error on malformed store during record")
	}
}

func TestSyncOutcomesAndSuppressions(t *testing.T) {
	s := dispositionFixtureStore(t)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	if err := s.RecordDelivered([]Judgment{
		{Fingerprint: "fp-open", Name: "open gap"},
		{Fingerprint: "fp-done", Name: "done gap"},
	}, now); err != nil {
		t.Fatal(err)
	}
	for fp, id := range map[string]string{"fp-open": "T410", "fp-done": "T411"} {
		if _, err := s.SetDisposition(SetDispositionArgs{
			Fingerprint: fp, Disposition: DispositionFile,
			TargetID: id, TargetCwd: "/repo", Now: now,
		}); err != nil {
			t.Fatal(err)
		}
	}

	read := func(cwd, id string) (string, error) {
		if id == "T411" {
			return "achieved", nil
		}
		return "converging", nil
	}
	outcomeAt := now.Add(time.Hour)
	n, err := s.SyncOutcomes(read, outcomeAt)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("SyncOutcomes updated %d, want 1", n)
	}
	// Second sync is a no-op (outcome already recorded).
	if n, err := s.SyncOutcomes(read, outcomeAt.Add(time.Hour)); err != nil || n != 0 {
		t.Fatalf("resync = (%d, %v), want (0, nil)", n, err)
	}

	sup, err := s.Suppressions(outcomeAt)
	if err != nil {
		t.Fatal(err)
	}
	if got := sup["fp-open"]; !got.Always {
		t.Fatalf("fp-open should be always-suppressed (filed open), got %+v", got)
	}
	if got := sup["fp-done"]; got.Always || !got.After.Equal(outcomeAt) {
		t.Fatalf("fp-done should suppress by outcome time %v, got %+v", outcomeAt, got)
	}
	m, err := s.Metrics(outcomeAt, 14*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if m.Achieved != 1 {
		t.Fatalf("metrics achieved = %d, want 1: %+v", m.Achieved, m)
	}
}

// TestCoachCycleOutcomeSuppression proves acceptance #4/#5: an achieved
// outcome suppresses re-propose of the same fingerprint unless the evidence
// cluster is newer than the outcome.
func TestCoachCycleOutcomeSuppression(t *testing.T) {
	oldTS := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	evidence := func(ts time.Time) []Evidence {
		var out []Evidence
		for i := 0; i < 3; i++ {
			out = append(out, Evidence{
				Kind: "lifecycle_error", Component: "mcp", Decision: "tool",
				Outcome: "error", Message: "call failed", SourceID: "c1", TS: ts,
			})
		}
		return out
	}

	// Baseline: discover the fingerprint this cluster produces.
	base, err := RunCoachCycle(CoachCycleArgs{Evidence: evidence(oldTS), DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(base.Judgments) != 1 {
		t.Fatalf("want 1 judgment, got %d", len(base.Judgments))
	}
	fp := base.Judgments[0].Fingerprint

	outcomeAt := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	sup := map[string]Suppression{fp: {After: outcomeAt}}

	// Evidence older than the outcome: suppressed.
	res, err := RunCoachCycle(CoachCycleArgs{Evidence: evidence(oldTS), OutcomeSuppressions: sup, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Judgments) != 0 {
		t.Fatalf("want suppression, got judgments %+v", res.Judgments)
	}
	found := false
	for _, sk := range res.Skipped {
		if sk.Fingerprint == fp && sk.Reason == "outcome_suppressed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want outcome_suppressed skip for %s, got %+v", fp, res.Skipped)
	}

	// Newer evidence than the outcome: re-propose allowed.
	res2, err := RunCoachCycle(CoachCycleArgs{
		Evidence: evidence(outcomeAt.Add(24 * time.Hour)), OutcomeSuppressions: sup, DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Judgments) != 1 {
		t.Fatalf("want re-propose with new evidence, got %+v", res2.Skipped)
	}

	// Filed-and-open: suppressed regardless of evidence age.
	res3, err := RunCoachCycle(CoachCycleArgs{
		Evidence:            evidence(outcomeAt.Add(24 * time.Hour)),
		OutcomeSuppressions: map[string]Suppression{fp: {Always: true}},
		DryRun:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res3.Judgments) != 0 {
		t.Fatalf("want filed-open suppression, got judgments %+v", res3.Judgments)
	}
}

func TestRecordDeliveredReopensAfterOutcome(t *testing.T) {
	s := dispositionFixtureStore(t)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	if err := s.RecordDelivered([]Judgment{{Fingerprint: "fp", Name: "gap"}}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDisposition(SetDispositionArgs{
		Fingerprint: "fp", Disposition: DispositionFile, TargetID: "T5", TargetCwd: "/r", Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncOutcomes(func(cwd, id string) (string, error) { return "achieved", nil }, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Coach re-delivers (new evidence beat suppression): entry reopens as pending.
	if err := s.RecordDelivered([]Judgment{{Fingerprint: "fp", Name: "gap again"}}, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Disposition != DispositionPending || e.Outcome != "" {
		t.Fatalf("want reopened pending entry, got %+v", e)
	}
}

func TestReadBullseyeTargetStatus(t *testing.T) {
	repo := t.TempDir()
	yaml := `schema_version: 5
targets:
  T7:
    name: Example
    status: achieved
  T8:
    name: Example open
    status: converging
`
	if err := os.WriteFile(filepath.Join(repo, "bullseye.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "internal", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Discovery walks up from a subdirectory; 🎯 prefix is stripped.
	status, err := ReadBullseyeTargetStatus(sub, "🎯T7")
	if err != nil {
		t.Fatal(err)
	}
	if status != "achieved" {
		t.Fatalf("status = %q, want achieved", status)
	}
	if s2, err := ReadBullseyeTargetStatus(repo, "T8"); err != nil || s2 != "converging" {
		t.Fatalf("T8 = (%q, %v)", s2, err)
	}
	if _, err := ReadBullseyeTargetStatus(repo, "T999"); err == nil || !strings.Contains(err.Error(), "not in") {
		t.Fatalf("want missing-target error, got %v", err)
	}
}


// 🎯T333.1 — usefulness is distinct from disposition rates, and a judgment
// that produced no compression is visible as performative.
func TestUsefulnessMetricsDistinguishUsefulVsPerformative(t *testing.T) {
	s := dispositionFixtureStore(t)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	if err := s.RecordDelivered([]Judgment{
		{Fingerprint: "fp-useful", Name: "real gap"},
		{Fingerprint: "fp-performative", Name: "ticket theatre"},
		{Fingerprint: "fp-acted", Name: "ops fix"},
		{Fingerprint: "fp-pending", Name: "untouched"},
		{Fingerprint: "fp-ignored", Name: "noise"},
	}, now); err != nil {
		t.Fatal(err)
	}
	// Two filed judgments + one act_other + one ignore + one pending.
	for _, fp := range []string{"fp-useful", "fp-performative"} {
		if _, err := s.SetDisposition(SetDispositionArgs{
			Fingerprint: fp, Disposition: DispositionFile,
			TargetID: "T" + fp, TargetCwd: "/repo", Now: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SetDisposition(SetDispositionArgs{
		Fingerprint: "fp-acted", Disposition: DispositionActOther,
		Reason: "fixed in place", Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDisposition(SetDispositionArgs{
		Fingerprint: "fp-ignored", Disposition: DispositionIgnoreWithReason,
		Reason: "one-off", Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	// Ledger outcomes drive crude usefulness inference on SyncOutcomes.
	read := func(_ string, id string) (string, error) {
		switch id {
		case "Tfp-useful":
			return OutcomeAchieved, nil
		case "Tfp-performative":
			return OutcomeSetAside, nil
		default:
			return "converging", nil
		}
	}
	if n, err := s.SyncOutcomes(read, now.Add(time.Hour)); err != nil || n != 2 {
		t.Fatalf("SyncOutcomes = (%d, %v), want (2, nil)", n, err)
	}

	// Explicit escalation_down for the acted judgment (not ledger-inferred).
	if _, err := s.SetUsefulness(SetUsefulnessArgs{
		Fingerprint: "fp-acted",
		Usefulness:  UsefulnessEscalationDown,
		Note:        "same error cluster count fell after the ops fix",
		Now:         now.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	m, err := s.Metrics(now.Add(2*time.Hour), 14*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Disposition rates still independent.
	if m.Filed != 2 || m.ActOther != 1 || m.Ignored != 1 || m.Pending != 1 {
		t.Fatalf("disposition rates wrong: %+v", m)
	}
	if m.Achieved != 1 || m.SetAside != 1 {
		t.Fatalf("outcome rates wrong: %+v", m)
	}
	// Usefulness rates distinct: useful (implemented + escalation) vs performative.
	if m.ImplementedStayedUseful != 1 {
		t.Fatalf("implemented_stayed_useful=%d want 1: %+v", m.ImplementedStayedUseful, m)
	}
	if m.EscalationDown != 1 {
		t.Fatalf("escalation_down=%d want 1: %+v", m.EscalationDown, m)
	}
	if m.NoEffect != 1 {
		t.Fatalf("no_effect=%d want 1 (performative visible): %+v", m.NoEffect, m)
	}
	if m.UsefulnessUnscored != 0 {
		// All three candidates (2 file + 1 act_other) are scored.
		t.Fatalf("unscored=%d want 0: %+v", m.UsefulnessUnscored, m)
	}
	if m.UsefulnessUseful() != 2 {
		t.Fatalf("UsefulnessUseful()=%d want 2", m.UsefulnessUseful())
	}
	if m.UsefulnessScored() != 3 {
		t.Fatalf("UsefulnessScored()=%d want 3", m.UsefulnessScored())
	}

	// Ignore is not a usefulness candidate — its absence from unscored is the point.
	entries, err := s.Entries()
	if err != nil {
		t.Fatal(err)
	}
	byFP := map[string]DispositionEntry{}
	for _, e := range entries {
		byFP[e.Fingerprint] = e
	}
	if byFP["fp-useful"].Usefulness != UsefulnessImplementedStayedUseful {
		t.Fatalf("fp-useful usefulness=%q", byFP["fp-useful"].Usefulness)
	}
	if byFP["fp-performative"].Usefulness != UsefulnessNoEffect {
		t.Fatalf("fp-performative usefulness=%q (must be visible as no compression)", byFP["fp-performative"].Usefulness)
	}
	if byFP["fp-acted"].Usefulness != UsefulnessEscalationDown {
		t.Fatalf("fp-acted usefulness=%q", byFP["fp-acted"].Usefulness)
	}
	if byFP["fp-ignored"].Usefulness != "" {
		t.Fatalf("ignored judgment should not carry usefulness, got %q", byFP["fp-ignored"].Usefulness)
	}
}

func TestInferUsefulnessAndNormalize(t *testing.T) {
	if got := InferUsefulness(OutcomeAchieved); got != UsefulnessImplementedStayedUseful {
		t.Fatalf("achieved → %q", got)
	}
	if got := InferUsefulness(OutcomeSetAside); got != UsefulnessNoEffect {
		t.Fatalf("set_aside → %q", got)
	}
	if got := InferUsefulness("converging"); got != "" {
		t.Fatalf("open → %q", got)
	}
	if !isUsefulnessCandidate(DispositionFile) || !isUsefulnessCandidate(DispositionActOther) {
		t.Fatal("file/act_other must be candidates")
	}
	if isUsefulnessCandidate(DispositionPark) || isUsefulnessCandidate(DispositionIgnoreWithReason) || isUsefulnessCandidate(DispositionPending) {
		t.Fatal("park/ignore/pending must not be candidates")
	}
	if _, err := NormalizeUsefulness("shrug"); err == nil {
		t.Fatal("want error for unknown usefulness")
	}
	if v, err := NormalizeUsefulness(UsefulnessNoEffect); err != nil || v != UsefulnessNoEffect {
		t.Fatalf("normalize no_effect = (%q, %v)", v, err)
	}
}

func TestSetUsefulnessValidationAndClear(t *testing.T) {
	s := dispositionFixtureStore(t)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	if _, err := s.SetUsefulness(SetUsefulnessArgs{Fingerprint: "fp", Usefulness: "nope"}); err == nil {
		t.Fatal("want error for bad usefulness")
	}
	if _, err := s.SetUsefulness(SetUsefulnessArgs{Usefulness: UsefulnessNoEffect}); err == nil {
		t.Fatal("want error for missing fingerprint")
	}
	e, err := s.SetUsefulness(SetUsefulnessArgs{
		Fingerprint: "fp-new", Usefulness: UsefulnessNoEffect, Note: "no later effect", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.Usefulness != UsefulnessNoEffect || e.UsefulnessNote != "no later effect" || e.UsefulnessAt.IsZero() {
		t.Fatalf("entry wrong: %+v", e)
	}
	// Clear.
	cleared, err := s.SetUsefulness(SetUsefulnessArgs{Fingerprint: "fp-new", Usefulness: "", Now: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Usefulness != "" || !cleared.UsefulnessAt.IsZero() || cleared.UsefulnessNote != "" {
		t.Fatalf("clear failed: %+v", cleared)
	}
}

func TestRecordDeliveredClearsUsefulnessOnReopen(t *testing.T) {
	s := dispositionFixtureStore(t)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	if err := s.RecordDelivered([]Judgment{{Fingerprint: "fp", Name: "gap"}}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDisposition(SetDispositionArgs{
		Fingerprint: "fp", Disposition: DispositionFile, TargetID: "T5", TargetCwd: "/r", Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncOutcomes(func(string, string) (string, error) { return OutcomeAchieved, nil }, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Entries()
	if err != nil || len(entries) != 1 || entries[0].Usefulness != UsefulnessImplementedStayedUseful {
		t.Fatalf("pre-reopen usefulness missing: %+v err=%v", entries, err)
	}
	// Re-delivery with prior outcome: reopen clears usefulness.
	if err := s.RecordDelivered([]Judgment{{Fingerprint: "fp", Name: "gap again"}}, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	entries, err = s.Entries()
	if err != nil {
		t.Fatal(err)
	}
	e := entries[0]
	if e.Disposition != DispositionPending || e.Outcome != "" || e.Usefulness != "" {
		t.Fatalf("want cleared usefulness on reopen, got %+v", e)
	}
}

func TestUsefulnessUnscoredUntilOutcome(t *testing.T) {
	s := dispositionFixtureStore(t)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	if err := s.RecordDelivered([]Judgment{{Fingerprint: "fp", Name: "open"}}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDisposition(SetDispositionArgs{
		Fingerprint: "fp", Disposition: DispositionFile, TargetID: "T1", TargetCwd: "/r", Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	m, err := s.Metrics(now, 14*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if m.Filed != 1 || m.UsefulnessUnscored != 1 || m.UsefulnessScored() != 0 {
		t.Fatalf("want unscored filed judgment visible: %+v", m)
	}
}
