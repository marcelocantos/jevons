// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package relayroute decides whether a worker report needs a product-owner
// turn or can reach the overseer directly (🎯T392.7).
//
// A PO that only restates a worker report costs two coordinator turns for
// information that did not change. A declared envelope decides the hop:
// an oracle or risk finish-report, a blocked finish-report, an escalation,
// or a target-file request skips to the overseer. A report that declares
// nothing stays with the parent. A sentence that says "blocked on" or
// "needs owner" is not that declaration (🎯T766.4).
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

// Classify reads the declared envelope, not the agent's name and not the
// sentence. An unenveloped report stays with the parent (🎯T766.4).
func Classify(report string) Route {
	_, decided, route := envelopeRoute(report)
	if decided {
		return route
	}
	return RouteParent
}

// envelopeRoute is the envelope arm shared by Classify and Reason. It returns
// the payload when the envelope did not decide, and whether the envelope
// alone decided the route.
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
		if err == nil && m.IsBlocked() {
			return "", true, RouteOverseer
		}
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

func (r Route) String() string { return string(r) }

// Reason is a stable token for logs and the PO record line.
func Reason(report string) string {
	_, decided, route := envelopeRoute(report)
	if !decided || route != RouteOverseer {
		return "parent"
	}
	if m, _ := envelope.Parse(report); m != nil && m.Kind == envelope.KindFinishReport {
		if m.IsBlocked() {
			return "blocked_on"
		}
		return "oracle_done"
	}
	return "needs_owner"
}
