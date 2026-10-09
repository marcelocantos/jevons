// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package relayroute

import (
	"strings"

	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/roles"
)

// ReportSummary returns a bounded, single-line excerpt for the PO record and
// durable route event. It is deliberately mechanical: the full report still
// goes to the overseer, while the PO gets enough content to identify it.
//
// 🎯T614: a skipped-hop record must name the worker report (target and SHA
// or GATE), not the standing brief / role doctrine a first send prepends.
// Citations are taken from the envelope slots so they survive truncation
// and never dump `jevons:` lines (🎯T509).
func ReportSummary(report string) string {
	src := peelInjectedBrief(report)
	if m, _ := envelope.Parse(src); m != nil {
		if cited := citeMessage(m); cited != "" {
			src = cited
		}
	}
	return boundSummary(src)
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

func boundSummary(s string) string {
	summary := strings.Join(strings.Fields(s), " ")
	if summary == "" {
		return "empty report"
	}
	runes := []rune(summary)
	if len(runes) <= recordSummaryRuneLimit {
		return summary
	}
	return string(runes[:recordSummaryRuneLimit-1]) + "…"
}

// citeMessage prefers target / SHA / GATE from envelope slots, then the
// payload. Slot lines themselves stay off the record (🎯T509).
func citeMessage(m *envelope.Message) string {
	if m == nil {
		return ""
	}
	var parts []string
	if t := strings.TrimSpace(m.Target); t != "" {
		parts = append(parts, t)
	}
	if sha := strings.TrimSpace(m.SHA); sha != "" {
		parts = append(parts, "SHA "+sha)
	}
	if g := strings.TrimSpace(m.GateID); g != "" {
		parts = append(parts, "GATE "+g)
	}
	if p := strings.Join(strings.Fields(m.Payload), " "); p != "" {
		parts = append(parts, p)
	}
	return strings.Join(parts, " ")
}

// peelInjectedBrief drops daemon-authored prefixes so the summary is the
// sender's report. envelope.StripPrefixes already peels the standing brief;
// the role doctrine has no end marker of its own, so this takes the fence
// (or a GATE/SHA evidence line) that follows it. A doctrine that cannot be
// bounded yields empty — never the doctrine itself (🎯T614 / 🎯T658).
func peelInjectedBrief(report string) string {
	body, _ := envelope.StripPrefixes(report)
	body = strings.TrimLeft(body, " \t\r\n")
	if strings.HasPrefix(body, roles.DoctrineMarker) {
		if peeled := peelReportAfterDoctrine(body[len(roles.DoctrineMarker):]); peeled != "" {
			return peeled
		}
		return ""
	}
	if i := strings.Index(body, roles.DoctrineMarker); i >= 0 {
		if peeled := peelReportAfterDoctrine(body[i+len(roles.DoctrineMarker):]); peeled != "" {
			return peeled
		}
	}
	return body
}

func peelReportAfterDoctrine(rest string) string {
	fence := "```" + envelope.FenceInfo + "\n"
	if i := strings.Index(rest, fence); i >= 0 {
		return rest[i:]
	}
	if i := firstEvidenceIndex(rest); i >= 0 {
		return strings.TrimLeft(rest[i:], " \t\r\n")
	}
	return ""
}

// firstEvidenceIndex finds a GATE/SHA line after doctrine. 🎯T ids are not
// used as a start marker: the product-owner doctrine itself names 🎯T125.
func firstEvidenceIndex(s string) int {
	trimmed := strings.TrimLeft(s, " \t\r\n")
	if strings.HasPrefix(trimmed, "GATE ") || strings.HasPrefix(trimmed, "SHA ") {
		return len(s) - len(trimmed)
	}
	best := -1
	for _, m := range []string{"\nGATE ", "\nSHA "} {
		if i := strings.Index(s, m); i >= 0 {
			pos := i + 1
			if best < 0 || pos < best {
				best = pos
			}
		}
	}
	return best
}
