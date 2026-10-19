// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"strings"

	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/shaevidence"
)

// 🎯T826 — A worker's older non-terminal status report is superseded by its
// newer one in the receiver's queue.
//
// 2026-09-22: jevons-po's sendq reached 21 messages, almost all worker progress
// ("waiting on the monitor", "gate still running"). The T774 digest of 7
// collapsed only 2 (unrecognised kinds), so the PO answered a turn per stale
// report. T810 covers identical fleet-health notices; T821 covers idle nudges.
// This is the same shape for worker progress: at most one supersedable
// non-terminal report per sending worker is pending. Terminal reports,
// refusals, and anything carrying a GATE id, SHA or ask are never dropped.

// AgentReportSender returns the worker name from a "[Agent X responded]"
// routing line, or ("", false) when text is not a worker report.
func AgentReportSender(text string) (string, bool) {
	for _, line := range firstLines(strings.TrimSpace(text), 6) {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "[Agent ")
		if !ok {
			// A reap banner may sit ahead of the routing line.
			continue
		}
		name, _, found := strings.Cut(rest, " responded]")
		name = strings.TrimSpace(name)
		if found && name != "" {
			return name, true
		}
	}
	return "", false
}

// IsSupersedableProgressReport is true when text is a worker status report that
// a newer one from the same worker may replace (🎯T826). False for non-reports
// and for anything that must survive: terminal finish/scout envelopes, GATE
// attestations, SHA evidence, owner asks, and explicit refusals.
func IsSupersedableProgressReport(text string) bool {
	if _, ok := AgentReportSender(text); !ok {
		return false
	}
	return progressKeepReason(text) == ""
}

// progressKeepReason names why a worker report must not be collapsed, or ""
// when it is ordinary supersedable progress.
func progressKeepReason(text string) string {
	if m, _ := envelope.Parse(text); m != nil {
		switch m.Kind {
		case envelope.KindFinishReport, envelope.KindScoutReport:
			return "terminal"
		}
		if m.HasOracle() {
			return "oracle"
		}
		if strings.TrimSpace(m.Question) != "" || strings.TrimSpace(m.QuestionID) != "" {
			return "ask"
		}
		if m.IsBlocked() {
			return "blocked"
		}
	}
	if len(gate.ParseAttestations(text)) > 0 {
		return "gate"
	}
	if len(shaevidence.ExtractEvidenceSHAs(text)) > 0 {
		return "sha"
	}
	if reportCarriesAsk(text) {
		return "ask"
	}
	if reportCarriesRefusal(text) {
		return "refusal"
	}
	return ""
}

// reportCarriesAsk is a lightweight ask detector for queue classification.
// Full ClassifyReportAsk lives in mcpserver; this catches the shapes that
// must never be dropped from a PO backlog (section heading, explicit owner
// decision request, question slots already handled above).
func reportCarriesAsk(text string) bool {
	lower := strings.ToLower(text)
	for _, m := range []string{
		"## 3. asks",
		"## asks",
		"owner decision requested",
		"needs an owner decision",
		"need a decision",
		"decision needed",
		"awaiting your",
		"awaiting a decision",
		"please advise",
		"please confirm",
		"which would you prefer",
		"question for you",
	} {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// reportCarriesRefusal keeps explicit refusal/not-delivered notices out of the
// supersede path — they are not "still running" status.
func reportCarriesRefusal(text string) bool {
	lower := strings.ToLower(text)
	for _, m := range []string{
		"not delivered",
		"refused",
		"refusal",
		"provider refused",
		"send refused",
	} {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// SameWorkerSupersedableProgress returns an AppendSuperseding predicate that
// drops a pending entry when it is supersedable progress from the same worker
// as incoming. Attempting/Uncertain and keep-worthy reports are never matched.
func SameWorkerSupersedableProgress(incoming string) func(Entry) bool {
	sender, ok := AgentReportSender(incoming)
	if !ok || !IsSupersedableProgressReport(incoming) {
		return func(Entry) bool { return false }
	}
	return func(old Entry) bool {
		oldSender, isReport := AgentReportSender(old.Text)
		return isReport && oldSender == sender && IsSupersedableProgressReport(old.Text)
	}
}
