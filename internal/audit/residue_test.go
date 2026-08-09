// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"os"
	"testing"
	"time"
)

func writeFileRaw(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func testStore(t *testing.T) *ResidueStore {
	t.Helper()
	s, err := OpenResidueStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// finding builds a report finding with a computed fingerprint.
func finding(scope ScopeKind, path, title string, sev Severity) Finding {
	f := Finding{Scope: scope, Path: path, Title: title, Severity: sev}
	f.Fingerprint = Fingerprint(scope, path, title, "")
	return f
}

func reportWith(id string, fs ...Finding) Report {
	return Report{ID: id, Findings: fs}
}

func allScopes() []ScopeKind { return RequiredScopes }

func TestResidueMergeLifecycle(t *testing.T) {
	s := testStore(t)
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	bug := finding(ScopeCode, "internal/server/chat.go", "owner turn dropped on busy seat", SeverityHigh)

	// Pass 1: brand new finding.
	res, err := s.Merge(MergeArgs{Report: reportWith("r1", bug), CoveredScopes: allScopes(), Now: base})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.New) != 1 || len(res.Updated) != 0 || len(res.Resolved) != 0 {
		t.Fatalf("pass 1 delta wrong: new=%d updated=%d resolved=%d", len(res.New), len(res.Updated), len(res.Resolved))
	}
	if res.OpenTotal != 1 {
		t.Fatalf("open total = %d, want 1", res.OpenTotal)
	}
	if got := res.New[0].Disposition; got != DispositionPending {
		t.Fatalf("new entry disposition = %q, want pending", got)
	}

	// Pass 2: same finding again — updates in place, never duplicates.
	res, err = s.Merge(MergeArgs{Report: reportWith("r2", bug), CoveredScopes: allScopes(), Now: base.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.New) != 0 || len(res.Updated) != 1 {
		t.Fatalf("pass 2 should update, not append: new=%d updated=%d", len(res.New), len(res.Updated))
	}
	entries, err := s.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("residue grew to %d entries; the same finding must not duplicate", len(entries))
	}
	if entries[0].SeenCount != 2 {
		t.Fatalf("seen count = %d, want 2", entries[0].SeenCount)
	}

	// Pass 3: the defect is fixed — an empty covering pass resolves it.
	res, err = s.Merge(MergeArgs{Report: reportWith("r3"), CoveredScopes: allScopes(), Now: base.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Resolved) != 1 {
		t.Fatalf("pass 3 should resolve: %+v", res)
	}
	if res.OpenTotal != 0 {
		t.Fatalf("open total = %d, want 0", res.OpenTotal)
	}

	// Pass 4: it regresses — the entry reopens with its history intact.
	res, err = s.Merge(MergeArgs{Report: reportWith("r4", bug), CoveredScopes: allScopes(), Now: base.Add(3 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reopened) != 1 || len(res.New) != 0 {
		t.Fatalf("pass 4 should reopen, not mint new: %+v", res)
	}
	e := res.Reopened[0]
	if e.Reopens != 1 || e.SeenCount != 3 {
		t.Fatalf("history lost on reopen: reopens=%d seen=%d", e.Reopens, e.SeenCount)
	}
	if !e.FirstSeen.Equal(base) {
		t.Fatalf("first seen drifted to %s, want %s", e.FirstSeen, base)
	}
	if !e.ResolvedAt.IsZero() {
		t.Fatal("resolved_at not cleared on reopen")
	}
}

func TestResiduePartialScanNeverResolvesUnseenScopes(t *testing.T) {
	// A pass that could not see the skills tree must not conclude its
	// findings are fixed — that is how an audit silently loses residue.
	s := testStore(t)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	codeBug := finding(ScopeCode, "a.go", "code defect", SeverityHigh)
	skillBug := finding(ScopeSkills, "skills/release/SKILL.md", "stale skill reference", SeverityMedium)

	if _, err := s.Merge(MergeArgs{
		Report:        reportWith("r1", codeBug, skillBug),
		CoveredScopes: allScopes(),
		Now:           now,
	}); err != nil {
		t.Fatal(err)
	}

	// Second pass covers code only, and reports the code bug fixed.
	res, err := s.Merge(MergeArgs{
		Report:        reportWith("r2"),
		CoveredScopes: []ScopeKind{ScopeCode},
		Now:           now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Resolved) != 1 || res.Resolved[0].Scope != ScopeCode {
		t.Fatalf("expected only the code finding to resolve: %+v", res.Resolved)
	}
	if res.OpenTotal != 1 {
		t.Fatalf("open total = %d, want 1 (skills finding survives)", res.OpenTotal)
	}
	open, err := s.OpenEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].Scope != ScopeSkills {
		t.Fatalf("skills residue lost to a partial scan: %+v", open)
	}
}

func TestResidueNotifyThresholdAndRealertWindow(t *testing.T) {
	s := testStore(t)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	crit := finding(ScopeCode, "a.go", "credential written to the event log", SeverityCritical)
	low := finding(ScopeCode, "b.go", "comment typo", SeverityLow)

	res, err := s.Merge(MergeArgs{
		Report: reportWith("r1", crit, low), CoveredScopes: allScopes(),
		NotifySeverity: SeverityCritical, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notify) != 1 || res.Notify[0].Severity != SeverityCritical {
		t.Fatalf("notify set = %+v, want only the critical finding", res.Notify)
	}

	// Same criticals next pass, inside the re-alert window: standing
	// findings must not re-alert every cycle.
	res, err = s.Merge(MergeArgs{
		Report: reportWith("r2", crit, low), CoveredScopes: allScopes(),
		NotifySeverity: SeverityCritical, RenotifyAfter: 24 * time.Hour,
		Now: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notify) != 0 {
		t.Fatalf("standing critical re-alerted inside the window: %+v", res.Notify)
	}

	// Past the window it speaks up again.
	res, err = s.Merge(MergeArgs{
		Report: reportWith("r3", crit, low), CoveredScopes: allScopes(),
		NotifySeverity: SeverityCritical, RenotifyAfter: 24 * time.Hour,
		Now: now.Add(30 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notify) != 1 {
		t.Fatalf("notify after re-alert window = %d, want 1", len(res.Notify))
	}
}

func TestResidueNotifyRespectsMaxNotify(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC()
	var fs []Finding
	for _, name := range []string{"one", "two", "three", "four"} {
		fs = append(fs, finding(ScopeCode, name+".go", "critical defect "+name, SeverityCritical))
	}
	res, err := s.Merge(MergeArgs{
		Report: reportWith("r1", fs...), CoveredScopes: allScopes(),
		NotifySeverity: SeverityCritical, MaxNotify: 2, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notify) != 2 {
		t.Fatalf("notify = %d, want 2 (cap)", len(res.Notify))
	}
	if res.OpenTotal != 4 {
		t.Fatalf("capping notifications must not drop residue: open=%d", res.OpenTotal)
	}
}

func TestResidueDisposition(t *testing.T) {
	s := testStore(t)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	bug := finding(ScopeCode, "a.go", "defect", SeverityHigh)
	if _, err := s.Merge(MergeArgs{Report: reportWith("r1", bug), CoveredScopes: allScopes(), Now: now}); err != nil {
		t.Fatal(err)
	}

	e, err := s.SetDisposition(bug.Fingerprint, DispositionFiled, "tracked", "T999", now)
	if err != nil {
		t.Fatal(err)
	}
	if e.Disposition != DispositionFiled || e.TargetID != "T999" {
		t.Fatalf("disposition not recorded: %+v", e)
	}

	// ignore_with_reason demands a reason: a silent ignore is how findings
	// disappear without anyone deciding.
	if _, err := s.SetDisposition(bug.Fingerprint, DispositionIgnoreWithReason, "", "", now); err == nil {
		t.Fatal("expected error for ignore without reason")
	}
	if _, err := s.SetDisposition(bug.Fingerprint, "nonsense", "", "", now); err == nil {
		t.Fatal("expected error for unknown disposition")
	}
	if _, err := s.SetDisposition("no-such-fingerprint", DispositionFiled, "", "", now); err == nil {
		t.Fatal("expected error for unknown fingerprint")
	}
}

func TestResidueReopenClearsIgnoreDisposition(t *testing.T) {
	// An ignore verdict was made against evidence that no longer holds:
	// when the finding comes back it goes in front of the overseer again.
	s := testStore(t)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	bug := finding(ScopeCode, "a.go", "defect", SeverityHigh)

	if _, err := s.Merge(MergeArgs{Report: reportWith("r1", bug), CoveredScopes: allScopes(), Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDisposition(bug.Fingerprint, DispositionIgnoreWithReason, "not worth it", "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Merge(MergeArgs{Report: reportWith("r2"), CoveredScopes: allScopes(), Now: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	res, err := s.Merge(MergeArgs{Report: reportWith("r3", bug), CoveredScopes: allScopes(), Now: now.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reopened) != 1 {
		t.Fatalf("expected reopen: %+v", res)
	}
	if got := res.Reopened[0].Disposition; got != DispositionPending {
		t.Fatalf("reopened entry disposition = %q, want pending", got)
	}
}

func TestResidueMergeRemembersWorstSeverity(t *testing.T) {
	s := testStore(t)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	crit := finding(ScopeCode, "a.go", "same defect", SeverityCritical)
	downgraded := finding(ScopeCode, "a.go", "same defect", SeverityLow)

	if _, err := s.Merge(MergeArgs{Report: reportWith("r1", crit), CoveredScopes: allScopes(), Now: now}); err != nil {
		t.Fatal(err)
	}
	res, err := s.Merge(MergeArgs{Report: reportWith("r2", downgraded), CoveredScopes: allScopes(), Now: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	e := res.Updated[0]
	if e.Severity != SeverityLow {
		t.Fatalf("current severity = %q, want low", e.Severity)
	}
	if e.MaxSeverity != SeverityCritical {
		t.Fatalf("max severity = %q, want critical retained", e.MaxSeverity)
	}
}

func TestResidueStoreMalformedIsHardError(t *testing.T) {
	// Malformed durable state is a hard error, never a silent reset.
	dir := t.TempDir()
	s, err := OpenResidueStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileRaw(s.Path(), "{ this is not the ledger"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Entries(); err == nil {
		t.Fatal("expected hard error on malformed residue ledger")
	}
}
