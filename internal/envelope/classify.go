// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import "strings"

// Read is the field-read view existing prose classifiers consult. A nil
// Message means the caller must fall back to prose heuristics.
func Read(text string) *Message {
	m, _ := Parse(text)
	return m
}

// ClassifyProgress implements 🎯T176 field-first: envelope status wins;
// otherwise a coarse prose sniff. Empty means "no status claim".
func ClassifyProgress(text string) Progress {
	if m, err := Parse(text); m != nil && err == nil && m.Status != ProgressNone {
		return m.Status
	}
	body := text
	if m, _ := Parse(text); m != nil && m.Payload != "" {
		body = m.Payload
	}
	lower := strings.ToLower(body)
	if strings.Contains(lower, "in progress") || strings.Contains(lower, "in-progress") {
		return ProgressInProgress
	}
	return ProgressNone
}

// OracleBody is the text a prose oracle-marker heuristic should scan:
// envelope payload when a valid envelope is present (fields already
// captured), otherwise the original report.
func OracleBody(text string) string {
	m, err := Parse(text)
	if m == nil {
		return text
	}
	if err == nil && (m.HasOracle() || m.HasRisk()) {
		// Field path already decided; payload is rationale only.
		return m.Payload
	}
	if m.Payload != "" {
		return m.Payload
	}
	return text
}
