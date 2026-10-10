// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/agentreport"
)

// 🎯T690 — a worker can always reach its registry parent. Reporting upward
// must not depend on a per-seat approval policy for jevons_agent_send.
//
// Specimen (2026-09-20): jv-t679-born-stuck finished a scout, then could not
// tell jevons-po because Grok refused the MCP tool with "approval policy is
// never". The overseer relayed by hand. The parent can always reach the
// worker, so the channel looked fine from above.
//
// Product choice: the finish-report / stored-report path the daemon already
// captures on a terminal stop is also delivered to the registry parent.
// That call is deliverByName from the owner surface — not the MCP tool —
// so a seat whose policy denies jevons_agent_send still reports. Spawn
// result cites ParentReportChannelCite so the caller knows which.

const (
	// ParentReportChannel is the spawn-result token for the daemon path.
	ParentReportChannel = "daemon-delivered"
	// ParentReportChannelCite is the owner-visible spawn-result fragment.
	ParentReportChannelCite = "parent_report: daemon-delivered (not gated on jevons_agent_send approval)"
	// ErrApprovalPolicyNever is the Grok specimen denial (jv-t679-born-stuck).
	ErrApprovalPolicyNever = "MCP tool call requires approval, but approval policy is never"
)

// DenyAgentSend records that name's seat policy denies jevons_agent_send
// the way Grok "approval policy is never" denies it. Production mint does
// not enter names here: the parent-report channel does not consult this map.
// The hermetic mints a denied seat and still asserts parent delivery.
func (s *Server) DenyAgentSend(name string) {
	name = strings.TrimSpace(name)
	if s == nil || name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sendApprovalNever == nil {
		s.sendApprovalNever = map[string]bool{}
	}
	s.sendApprovalNever[name] = true
}

func (s *Server) agentSendDenied(actor string) bool {
	actor = strings.TrimSpace(actor)
	if s == nil || actor == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendApprovalNever[actor]
}

// registryParent is the named parent on the agent's registry row. Empty
// parent is not invented: T690 is "its registry parent", not a default PO.
func (s *Server) registryParent(name string) string {
	if s == nil || s.registry == nil {
		return ""
	}
	d := s.registry.Def(name)
	if d == nil {
		return ""
	}
	p := strings.TrimSpace(d.Parent)
	if p == "" || p == name {
		return ""
	}
	return p
}

// notifyParentReport delivers a terminal report to the registry parent on
// the daemon path (🎯T690). It does not go through jevons_agent_send.
// Failure is logged; the overseer notify still runs.
func (s *Server) notifyParentReport(agentName, msg string) {
	parent := s.registryParent(agentName)
	if parent == "" {
		return
	}
	if s.isOverseerAgent(parent) {
		return
	}
	if rec, err := agentreport.Latest(s.agentReportStateDir(), agentName); err == nil {
		msg = withAgentReportID(msg, rec.Handle(), s.deliveryNow())
	}
	res, err := s.deliverByName(parent, msg, OriginAgent, false)
	if err != nil {
		slog.Error("agent parent-report notify failed",
			"agent", agentName, "parent", parent, "channel", ParentReportChannel, "err", err)
		s.logLifecycle(compAgentLifecycle, "parent_report", "error", map[string]any{
			"agent": agentName, "parent": parent, "err": err.Error(),
		})
		return
	}
	slog.Info("notifying parent",
		"agent", agentName, "parent", parent, "status", res.Status,
		"channel", ParentReportChannel)
	s.logLifecycle(compAgentLifecycle, "parent_report", "ok", map[string]any{
		"agent": agentName, "parent": parent, "status": res.Status,
		"channel": ParentReportChannel,
	})
}
