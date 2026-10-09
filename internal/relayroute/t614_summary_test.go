// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package relayroute

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/roles"
)

// t614FinishReport is the 2026-09-01 jv-t611-month-trim shape: an
// oracle-backed finish-report whose PO record must name the work (🎯T614).
const t614FinishReport = "```jevons\n" +
	"jevons: kind finish-report\n" +
	"jevons: target T611\n" +
	"jevons: oracle sha=b695174d gate-id=8544c7b4\n" +
	"jevons: verdict GREEN\n" +
	"jevons: silent-ledger none\n" +
	"```\n\n" +
	"Month-trim landed."

const t614StandingBrief = `[Jevons fleet standing brief — apply for this whole assignment]

Refuse bare done without oracle evidence (GATE … GREEN).

---
`

const t614RoleBody = `# Product-owner role

You are a product owner. Spawn-only for Build work.
`

func t614Injected(report string) string {
	return roles.Assemble(t614StandingBrief, t614RoleBody, report)
}

func t614AssertRecord(t *testing.T, line string) {
	t.Helper()
	if !strings.Contains(line, "T611") {
		t.Errorf("RecordLine missing target id T611: %q", line)
	}
	if !strings.Contains(line, "b695174d") && !strings.Contains(line, "8544c7b4") {
		t.Errorf("RecordLine missing SHA or GATE from the report: %q", line)
	}
	if strings.Contains(line, "Product-owner role") || strings.Contains(line, roles.DoctrineMarker) {
		t.Errorf("RecordLine summarized the standing brief / role doctrine: %q", line)
	}
	if strings.HasPrefix(strings.TrimSpace(line), roles.DoctrineMarker) ||
		strings.Contains(line, "[Jevons fleet standing brief") {
		t.Errorf("RecordLine opens with fleet-brief / role-doctrine text: %q", line)
	}
	if strings.Contains(line, "\n") {
		t.Errorf("RecordLine is not one line: %q", line)
	}
}

// 🎯T614: a T392.7 PO record of a skipped hop summarises the worker
// finish-report, not the standing brief prepended to it.
func TestT614RecordLineCitesOracleNotDoctrine(t *testing.T) {
	injected := t614Injected(t614FinishReport)
	if !strings.Contains(injected, roles.DoctrineMarker) || !strings.Contains(injected, "Product-owner role") {
		t.Fatal("precondition: injected wrap must carry the doctrine the incident summarized")
	}
	if Classify(t614FinishReport) != RouteOverseer {
		t.Fatalf("bare finish-report classified %s, want overseer", Classify(t614FinishReport))
	}

	line := RecordLine("jv-t611-month-trim", "oracle_done", injected)
	t614AssertRecord(t, line)
	if !strings.Contains(line, "jv-t611-month-trim") || !strings.Contains(line, "oracle_done") {
		t.Errorf("RecordLine lost the hop identity: %q", line)
	}
}

func TestT614ReportSummaryCitesEnvelopeOracle(t *testing.T) {
	got := ReportSummary(t614FinishReport)
	if !strings.Contains(got, "T611") {
		t.Errorf("summary missing target: %q", got)
	}
	if !strings.Contains(got, "b695174d") && !strings.Contains(got, "8544c7b4") {
		t.Errorf("summary missing SHA or GATE: %q", got)
	}
	if strings.Contains(got, "jevons:") {
		t.Errorf("summary dumped slot lines: %q", got)
	}
	if !strings.Contains(got, "Month-trim landed") {
		t.Errorf("summary dropped payload: %q", got)
	}
}

func TestT614InjectedBriefDoesNotPolluteSummary(t *testing.T) {
	got := ReportSummary(t614Injected(t614FinishReport))
	if strings.Contains(got, "Product-owner role") || strings.Contains(got, roles.DoctrineMarker) {
		t.Fatalf("summary took the doctrine: %q", got)
	}
	if !strings.Contains(got, "T611") {
		t.Errorf("summary missing target behind the wrap: %q", got)
	}
	if !strings.Contains(got, "b695174d") && !strings.Contains(got, "8544c7b4") {
		t.Errorf("summary missing SHA or GATE behind the wrap: %q", got)
	}
}
