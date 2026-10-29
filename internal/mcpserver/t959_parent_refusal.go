// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/agentreport"
)

// ParentReportStatusRefused is the lifecycle / delivery label for a turn the
// provider refused. The raw refusal text is not forwarded as worker progress.
const ParentReportStatusRefused = "refused"

// providerTurnRefused reports whether the just-finished turn was a transport
// refusal rather than authored worker output.
func providerTurnRefused(errored bool, class agenterr.Class, text string) bool {
	if errored && class.IsFailure() {
		return true
	}
	return agenterr.RefusalOnlyTurn(text)
}

// notifyProviderTurnRefused tells the registry parent the turn was refused,
// without delivering the provider's 401 prose as an ordinary worker report.
func (s *Server) notifyProviderTurnRefused(agentName string, class agenterr.Class) {
	parent := s.registryParent(agentName)
	if parent == "" || s.isOverseerAgent(parent) {
		return
	}
	body := fmt.Sprintf(
		"[provider refused the turn (%s) — not delivered as worker progress; re-drive follows repaired plan login]",
		class.String(),
	)
	msg := formatAgentResponded(agentName, agentreport.Handle{}, body, s.deliveryNow())
	res, err := s.deliverByName(parent, msg, OriginAgent, false)
	if err != nil {
		slog.Error("agent parent-report refused notify failed",
			"agent", agentName, "parent", parent, "channel", ParentReportChannel, "err", err)
		s.logLifecycle(compAgentLifecycle, "parent_report", "error", map[string]any{
			"agent": agentName, "parent": parent, "status": ParentReportStatusRefused, "err": err.Error(),
		})
		return
	}
	slog.Info("notifying parent",
		"agent", agentName, "parent", parent, "status", ParentReportStatusRefused,
		"delivery", res.Status, "channel", ParentReportChannel)
	s.logLifecycle(compAgentLifecycle, "parent_report", "ok", map[string]any{
		"agent": agentName, "parent": parent, "status": ParentReportStatusRefused,
		"delivery": res.Status, "channel": ParentReportChannel,
	})
}
