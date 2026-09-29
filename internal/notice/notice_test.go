// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package notice

import (
	"strings"
	"testing"
	"time"
)

func doneReport() string {
	return "```jevons\n" +
		"jevons: kind finish-report\n" +
		"jevons: target T254.4\n" +
		"jevons: sha abc123\n" +
		"jevons: gate id=deadbeef\n" +
		"jevons: verdict GREEN\n" +
		"jevons: silent-ledger none\n" +
		"```\n\nLanded the inbox surface. GATE make-test-go exit=0 GREEN id=deadbeef.\n"
}

func blockedReport() string {
	return "```jevons\n" +
		"jevons: kind finish-report\n" +
		"jevons: target T99\n" +
		"jevons: silent-ledger none\n" +
		"```\n\nBlocked: the dependency target is not filed yet.\n"
}

func TestFromReportExtractsDoneOutcome(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	n, ok := FromReport("jv-t254.4-fleet-ops-inbox", "jevons-po", doneReport(), at)
	if !ok {
		t.Fatalf("expected ok=true for a typed finish-report")
	}
	if n.Kind != "finish-report" {
		t.Fatalf("kind = %q", n.Kind)
	}
	if n.Outcome != OutcomeDone {
		t.Fatalf("outcome = %q, want done", n.Outcome)
	}
	if n.SHA != "abc123" || n.GateID != "id=deadbeef" || n.Verdict != "GREEN" {
		t.Fatalf("slots not carried through: %+v", n)
	}
	if !n.HasOracle {
		t.Fatalf("HasOracle should be true (sha+gate present)")
	}
	if n.Agent != "jv-t254.4-fleet-ops-inbox" || n.Parent != "jevons-po" {
		t.Fatalf("agent/parent not recorded: %+v", n)
	}
	if n.ID == "" {
		t.Fatalf("expected a non-empty id")
	}
}

func TestFromReportBlockedOutcome(t *testing.T) {
	n, ok := FromReport("jv-worker", "jevons-po", blockedReport(), time.Now())
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if n.Outcome != OutcomeBlocked {
		t.Fatalf("outcome = %q, want blocked", n.Outcome)
	}
}

func TestFromReportNonTerminalIsNotRecorded(t *testing.T) {
	if _, ok := FromReport("jv-worker", "jevons-po", "just a plain status update, nothing structured", time.Now()); ok {
		t.Fatalf("plain prose must not be treated as a terminal notice")
	}
	statusPing := "```jevons\njevons: kind status-ping\n```\n\nstill working"
	if _, ok := FromReport("jv-worker", "jevons-po", statusPing, time.Now()); ok {
		t.Fatalf("status-ping is not terminal and must not be recorded")
	}
}

func TestAppendAndListRoundTrip(t *testing.T) {
	dir := t.TempDir()
	n1, ok := FromReport("worker-a", "jevons-po", doneReport(), time.Now())
	if !ok {
		t.Fatalf("setup: expected ok")
	}
	if err := Append(dir, n1); err != nil {
		t.Fatalf("Append: %v", err)
	}
	n2, ok := FromReport("worker-b", "jevons-po", blockedReport(), time.Now().Add(time.Second))
	if !ok {
		t.Fatalf("setup: expected ok")
	}
	if err := Append(dir, n2); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := List(dir, "jevons-po")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List returned %d notices, want 2", len(got))
	}
	if got[0].Agent != "worker-a" || got[1].Agent != "worker-b" {
		t.Fatalf("unexpected order/content: %+v", got)
	}
	if got[0].Outcome != OutcomeDone || got[1].Outcome != OutcomeBlocked {
		t.Fatalf("unexpected outcomes: %+v", got)
	}
}

func TestListMissingParentIsEmptyNotError(t *testing.T) {
	dir := t.TempDir()
	got, err := List(dir, "no-such-po")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

func TestListIsolatesParents(t *testing.T) {
	dir := t.TempDir()
	n, _ := FromReport("worker-a", "po-1", doneReport(), time.Now())
	if err := Append(dir, n); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := List(dir, "po-2")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("po-2's inbox must not see po-1's notice, got %v", got)
	}
}

func envelopeReport(slots []string, payload string) string {
	s := "```jevons\n"
	for _, slot := range slots {
		s += "jevons: " + slot + "\n"
	}
	return s + "```\n\n" + payload
}

// 🎯T254.4 regression from the live inbox (2026-09-29, jv-t840-merge-reconcile):
// a finish-report citing sha + gate with verdict RED was recorded as done.
func TestFromReportRedVerdictIsNotDone(t *testing.T) {
	text := envelopeReport([]string{
		"kind finish-report", "target T840", "sha c1df1dec", "gate 88cd5aba",
		"verdict RED", "risk residual", "silent-ledger none",
	}, "T840 should not be achieved yet. The merge onto mainline is clean, but J3 fails 3 of 3 runs.\n")
	n, ok := FromReport("jv-t840-merge-reconcile", "jevons-po", text, time.Now())
	if !ok {
		t.Fatal("expected ok")
	}
	if n.Outcome != OutcomeBlocked {
		t.Fatalf("RED verdict outcome = %q, want blocked", n.Outcome)
	}
}

func TestFromReportNonPassVerdictIsOther(t *testing.T) {
	text := envelopeReport([]string{
		"kind finish-report", "target T1", "sha abc", "verdict DIRTY", "silent-ledger none",
	}, "Measured in a dirty tree.\n")
	n, _ := FromReport("w", "po", text, time.Now())
	if n.Outcome != OutcomeOther {
		t.Fatalf("DIRTY verdict outcome = %q, want other", n.Outcome)
	}
}

// A done report routinely names a design-gated follow-up or an unblocked
// dependency below its headline; whole-text substring matching filed those as
// needs-design / blocked.
func TestFromReportDoneWithDesignGatedFollowUpStaysDone(t *testing.T) {
	text := envelopeReport([]string{
		"kind finish-report", "target T254.4", "sha abc123", "gate deadbeef",
		"verdict GREEN", "silent-ledger none",
	}, "Landed the fleet-wide inbox view.\n\nResidual: dedup is T847, design-gated. T12 is unblocked now; nothing blocked here.\n")
	n, _ := FromReport("w", "po", text, time.Now())
	if n.Outcome != OutcomeDone {
		t.Fatalf("outcome = %q, want done", n.Outcome)
	}
}

func TestFromReportNegatedHeadlineIsNotBlocked(t *testing.T) {
	text := envelopeReport([]string{
		"kind finish-report", "target T5", "sha abc", "silent-ledger none",
	}, "Not blocked: the dependency landed and this is done.\n")
	n, _ := FromReport("w", "po", text, time.Now())
	if n.Outcome != OutcomeDone {
		t.Fatalf("outcome = %q, want done", n.Outcome)
	}
}

func TestFromReportNeedsDesignHeadline(t *testing.T) {
	text := envelopeReport([]string{
		"kind scout-report", "target T5", "silent-ledger none",
	}, "Needs design: the retention contract is owner taste.\n")
	n, _ := FromReport("w", "po", text, time.Now())
	if n.Outcome != OutcomeNeedsDesign {
		t.Fatalf("outcome = %q, want needs-design", n.Outcome)
	}
}

func TestFromReportExplicitOutcomeSlotWins(t *testing.T) {
	text := envelopeReport([]string{
		"kind finish-report", "target T5", "sha abc", "verdict GREEN",
		"outcome needs design", "silent-ledger none",
	}, "Partial slice landed; the rest waits on the owner.\n")
	n, _ := FromReport("w", "po", text, time.Now())
	if n.Outcome != OutcomeNeedsDesign {
		t.Fatalf("outcome = %q, want needs-design from the explicit slot", n.Outcome)
	}
}

func TestFromReportEscalationIsBlocked(t *testing.T) {
	text := envelopeReport([]string{"kind escalation", "target T5"}, "The provider refuses every turn.\n")
	n, ok := FromReport("w", "po", text, time.Now())
	if !ok {
		t.Fatal("escalation is a terminal raise and must be recorded")
	}
	if n.Outcome != OutcomeBlocked {
		t.Fatalf("outcome = %q, want blocked", n.Outcome)
	}
	text = envelopeReport([]string{"kind escalation", "target T5"}, "Design-gated: retention policy is the owner's call.\n")
	if n, _ := FromReport("w", "po", text, time.Now()); n.Outcome != OutcomeNeedsDesign {
		t.Fatalf("design escalation outcome = %q, want needs-design", n.Outcome)
	}
}

// The overseer reads every parent's inbox, including "unowned" notices whose
// reporting agent's parent could not be resolved.
func TestListAllMergesParentsOldestFirst(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	for i, p := range []struct{ agent, parent string }{
		{"w1", "jevons-po"}, {"w2", ""}, {"w3", "other-po"}, {"w4", "jevons-po"},
	} {
		n, _ := FromReport(p.agent, p.parent, doneReport(), base.Add(time.Duration(i)*time.Minute))
		if err := Append(dir, n); err != nil {
			t.Fatal(err)
		}
	}
	all, err := ListAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	var agents []string
	for _, n := range all {
		agents = append(agents, n.Agent)
	}
	if got := strings.Join(agents, ","); got != "w1,w2,w3,w4" {
		t.Fatalf("fleet-wide order = %s, want w1,w2,w3,w4", got)
	}
}

func TestSelectFiltersOutcomeAndLimit(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	reports := []string{doneReport(), blockedReport(), doneReport(), blockedReport(), blockedReport()}
	for i, r := range reports {
		n, _ := FromReport("w"+string(rune('a'+i)), "jevons-po", r, base.Add(time.Duration(i)*time.Minute))
		if err := Append(dir, n); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Select(dir, Query{Outcome: OutcomeBlocked, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Agent != "wd" || got[1].Agent != "we" {
		t.Fatalf("Select blocked limit 2 = %+v, want wd,we", got)
	}
	got, err = Select(dir, Query{Parent: "jevons-po", Outcome: OutcomeDone})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Select done = %d, want 2", len(got))
	}
}

func TestParseOutcome(t *testing.T) {
	for raw, want := range map[string]Outcome{
		"done": OutcomeDone, "Blocked": OutcomeBlocked, "needs design": OutcomeNeedsDesign,
		"needs_design": OutcomeNeedsDesign, "other": OutcomeOther,
	} {
		if got, ok := ParseOutcome(raw); !ok || got != want {
			t.Fatalf("ParseOutcome(%q) = %q,%v want %q", raw, got, ok, want)
		}
	}
	if _, ok := ParseOutcome("finished"); ok {
		t.Fatal("unknown outcome must not parse")
	}
}
