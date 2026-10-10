// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// routineWorkerCompletion is deliberately a narrow allowlist, not a prose
// classifier: only a validated typed GREEN terminal completion qualifies.
// The caller additionally proves a registry parent (not the overseer), a
// durable store, and a successful parent delivery. Any uncertainty fails open.
func routineWorkerCompletion(text string) bool {
	m, err := envelope.Parse(text)
	if err != nil || m == nil || m.Kind != envelope.KindFinishReport ||
		m.Verdict != envelope.VerdictGreen || (m.Status != envelope.ProgressLanded && m.Status != envelope.ProgressShipped) ||
		m.HasRisk() || len(m.Decisions) != 0 || m.Question != "" ||
		m.Blocker != "" || !m.HasOracle() || ClassifyReportAsk(text) != AskNone {
		return false
	}
	// A typed GREEN alone is not authority to hide a material failure or
	// decision request in the free payload. These vetoes only *prevent*
	// suppression; they never authorize it by themselves.
	lower := strings.ToLower(m.Payload)
	for _, marker := range []string{"error", "fail", "panic", "anomal", "incident", "timeout", "unknown", "blocked", "needs-owner", "owner decision", "please decide", "security", "regression", "risk", "urgent", "question"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}
