// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package relayroute decides whether a worker report needs a product-owner
// turn or can reach the overseer directly (🎯T392.7).
//
// A PO that only restates a worker report costs two coordinator turns for
// information that did not change. Reports that need no product judgement
// — oracle-backed done, blocked-on-X, needs-owner-decision — skip that hop.
// Everything else stays on the parent. The safe default is the current
// behaviour: if the classifier cannot tell, the PO still sees it first.
package relayroute

import (
	"strings"
)

const recordSummaryRuneLimit = 160

// Route is where a worker report should land.
type Route string

const (
	// RouteParent: send to the PO (current default). Scope changes, spawn
	// decisions, and target lifecycle stay here.
	RouteParent Route = "parent"
	// RouteOverseer: skip the PO hop; the overseer is the independent gate
	// (🎯T31) and the PO is only a relay.
	RouteOverseer Route = "overseer"
)

// Classify reads the report, not the agent's name.
func Classify(report string) Route {
	s := strings.ToLower(strings.TrimSpace(report))
	if s == "" {
		return RouteParent
	}
	// A record of a prior route is itself not a report to reroute.
	if strings.HasPrefix(s, "[routed to overseer]") {
		return RouteParent
	}
	if needsOwner(s) || blockedOn(s) || oracleDone(s) {
		return RouteOverseer
	}
	return RouteParent
}

func needsOwner(s string) bool {
	for _, p := range []string{
		"needs-owner", "needs owner", "awaiting owner",
		"owner verdict", "class-3", "blocked-on-human",
	} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func blockedOn(s string) bool {
	return strings.Contains(s, "blocked on") || strings.Contains(s, "blocked-on")
}

func oracleDone(s string) bool {
	done := strings.Contains(s, "done") || strings.Contains(s, "achieved") ||
		strings.Contains(s, "complete") || strings.Contains(s, "finished")
	if !done {
		return false
	}
	oracle := strings.Contains(s, "gate") && strings.Contains(s, "green")
	oracle = oracle || strings.Contains(s, "oracle")
	oracle = oracle || (strings.Contains(s, "sha") && (strings.Contains(s, "test") || strings.Contains(s, "green")))
	return oracle
}

// ReportSummary returns a bounded, single-line excerpt for the PO record and
// durable route event. It is deliberately mechanical: the full report still
// goes to the overseer, while the PO gets enough content to identify it.
func ReportSummary(report string) string {
	summary := strings.Join(strings.Fields(report), " ")
	if summary == "" {
		return "empty report"
	}
	runes := []rune(summary)
	if len(runes) <= recordSummaryRuneLimit {
		return summary
	}
	return string(runes[:recordSummaryRuneLimit-1]) + "…"
}

// RecordLine is the one-line the PO still sees when the full report skipped
// the hop. It identifies the reporting worker and carries a report summary.
func RecordLine(agent, reason, summary string) string {
	if strings.TrimSpace(agent) == "" {
		agent = "worker"
	}
	if strings.TrimSpace(reason) == "" {
		reason = "unspecified"
	}
	summary = ReportSummary(summary)
	return "[routed to overseer] " + agent + " report skipped the PO hop (" + reason + "): " + summary + " (🎯T392.7)"
}

func (r Route) String() string { return string(r) }

// Reason is a stable token for logs and the PO record line.
func Reason(report string) string {
	s := strings.ToLower(strings.TrimSpace(report))
	switch {
	case needsOwner(s):
		return "needs_owner"
	case blockedOn(s):
		return "blocked_on"
	case oracleDone(s):
		return "oracle_done"
	default:
		return "parent"
	}
}
