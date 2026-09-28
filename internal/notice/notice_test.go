// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package notice

import (
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
