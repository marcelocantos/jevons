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

	"github.com/marcelocantos/jevons/internal/envelope"
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
	body, decided, route := envelopeRoute(report)
	if decided {
		return route
	}
	s := strings.ToLower(strings.TrimSpace(body))
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

// envelopeRoute is the envelope arm shared by Classify and Reason. It returns
// the prose the keyword heuristics may read (never the fence's own slot
// lines), and whether the envelope alone decided the route.
//
// 🎯T658: two shapes used to fall through to the keyword scan over text the
// sender never wrote as a report. A fence that parses but fails validation
// (one malformed silent-decision slot) was scanned whole, so a scout-report
// whose fog lines mentioned "oracle" and whose prose said "Scout done" was
// oracle_done — the 2026-09-15 jv-t657-steer-ui reroute. And a fence that
// sits behind an unknown prefix (a first-send wrap the daemon composed) is
// no envelope to Parse, so the prefix's doctrine was scanned instead. Both
// are "cannot tell", and the package contract for that is the parent.
func envelopeRoute(report string) (body string, decided bool, route Route) {
	m, err := envelope.Parse(report)
	if m == nil {
		if fenceBeyondLine1(report) {
			return "", true, RouteParent
		}
		return report, false, RouteParent
	}
	switch m.Kind {
	case envelope.KindFinishReport:
		if err == nil && (m.HasOracle() || m.HasRisk()) {
			return "", true, RouteOverseer
		}
	case envelope.KindEscalation, envelope.KindTargetFileRequest:
		if err == nil {
			return "", true, RouteOverseer
		}
	case envelope.KindStatusPing, envelope.KindAck, envelope.KindSpawnBrief, envelope.KindScoutReport:
		// 🎯T536.3: scout handoff stays with the parent so they can
		// re-slice / spawn the implementer with an inherited ledger.
		return "", true, RouteParent
	}
	if err != nil {
		// Malformed envelope: the sender meant an envelope and the daemon
		// could not read it. The PO sees it first; nothing here is a
		// finish the keyword scan may infer from the fence's own words.
		return "", true, RouteParent
	}
	return m.Payload, false, RouteParent
}

// fenceBeyondLine1 reports whether text carries a ```jevons fence that
// Parse did not accept because prose precedes it. Parse treats such a fence
// as a quotation; for routing it means daemon framing this package does not
// know about sits ahead of the sender's envelope, and the words ahead of it
// are not the sender's report.
func fenceBeyondLine1(text string) bool {
	return strings.Contains(text, "\n```"+envelope.FenceInfo+"\n") ||
		strings.HasPrefix(strings.TrimLeft(text, " \t\r\n"), "```"+envelope.FenceInfo+"\n")
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
	if m, _ := envelope.Parse(report); m != nil {
		if p := strings.TrimSpace(m.Payload); p != "" {
			report = p
		}
	}
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
	body, decided, route := envelopeRoute(report)
	if decided {
		if route == RouteParent {
			return "parent"
		}
		if m, _ := envelope.Parse(report); m != nil && m.Kind == envelope.KindFinishReport {
			return "oracle_done"
		}
		return "needs_owner"
	}
	s := strings.ToLower(strings.TrimSpace(body))
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
