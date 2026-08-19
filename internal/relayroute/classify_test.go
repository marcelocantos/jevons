// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package relayroute

import (
	"strings"
	"testing"
)

func TestT3927OracleDoneGoesToOverseer(t *testing.T) {
	got := Classify("🎯T392.7 done. GATE abc GREEN tree=clean@deadbeef. SHA deadbeef.")
	if got != RouteOverseer {
		t.Fatalf("oracle-done classified %s, want overseer", got)
	}
	if Reason("GATE x GREEN, tests pass, done") != "oracle_done" {
		t.Fatalf("reason %s", Reason("GATE x GREEN, tests pass, done"))
	}
}

func TestT3927BlockedOnGoesToOverseer(t *testing.T) {
	if Classify("blocked on owner keypress for T383") != RouteOverseer {
		t.Fatal("blocked-on should skip the PO hop")
	}
}

func TestT3927NeedsOwnerGoesToOverseer(t *testing.T) {
	if Classify("needs-owner: class-3 taste verdict") != RouteOverseer {
		t.Fatal("needs-owner should skip the PO hop")
	}
}

func TestT3927ScopeChangeStaysWithParent(t *testing.T) {
	if Classify("we should split T10 into T10.1 and spawn two workers") != RouteParent {
		t.Fatal("spawn/scope decision must stay with the PO")
	}
	if Classify("") != RouteParent {
		t.Fatal("empty report must default to parent")
	}
	if Classify("[routed to overseer] jv-x skipped the hop") != RouteParent {
		t.Fatal("record line must not reroute")
	}
}

func TestT3927BareDoneStaysWithParent(t *testing.T) {
	// Bare "done" without oracle evidence is not a skip — T31 wants the
	// PO/overseer gate, and the safe default is the current hop.
	if Classify("done.") != RouteParent {
		t.Fatal("bare done must not skip the PO")
	}
}

func TestT3927RecordLine(t *testing.T) {
	report := "Needs owner verdict on provider spend.\n\nThe full report still goes to the overseer."
	line := RecordLine("jv-t392.7", "needs_owner", report)
	if Classify(line) != RouteParent {
		t.Fatalf("record line rerouted: %s", line)
	}
	for _, want := range []string{"jv-t392.7", "needs_owner", "Needs owner verdict on provider spend."} {
		if !strings.Contains(line, want) {
			t.Errorf("record line %q missing %q", line, want)
		}
	}
	if strings.Contains(line, "\n") {
		t.Fatalf("record line is not one line: %q", line)
	}
}

// 🎯T515: the PO record and durable route event need a bounded one-line
// excerpt, not the full multi-paragraph report (and not an empty placeholder).
func TestT515ReportSummary(t *testing.T) {
	if got := ReportSummary("  Needs   owner\nverdict.  "); got != "Needs owner verdict." {
		t.Fatalf("summary=%q", got)
	}
	if got := ReportSummary(" \n\t"); got != "empty report" {
		t.Fatalf("empty summary=%q", got)
	}
	long := strings.Repeat("word ", 80)
	got := ReportSummary(long)
	if strings.Contains(got, "\n") {
		t.Fatalf("summary not one line: %q", got)
	}
	runes := []rune(got)
	if len(runes) != recordSummaryRuneLimit {
		t.Fatalf("summary len=%d want %d (%q)", len(runes), recordSummaryRuneLimit, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated summary missing ellipsis: %q", got)
	}
}
