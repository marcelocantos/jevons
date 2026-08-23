// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"strings"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/mcpattach"
)

// mintFromPendingHandover rebuilds a registry row from a durable rotation
// record when the named agent is absent (🎯T474). ok=false means there is
// no recoverable handover — the caller falls through to a genuine mint.
func (f *Claudia) mintFromPendingHandover(name string) (claudia.AgentDef, bool) {
	if f == nil || f.handovers == nil {
		return claudia.AgentDef{}, false
	}
	p, ok, err := f.handovers.Get(name)
	if err != nil || !ok || !p.HasMintIdentity() {
		return claudia.AgentDef{}, false
	}
	def := agentDefFromPending(p)
	def.MCPServers = f.SessionMCPServers(def.Provider, def.WorkDir)
	return def, true
}

// agentDefFromPending is the pure half of 🎯T474 mint recovery.
func agentDefFromPending(p handover.Pending) claudia.AgentDef {
	prov := claudia.Provider(strings.TrimSpace(p.To))
	if prov == "" {
		prov = claudia.Provider(strings.TrimSpace(p.From))
	}
	purpose := strings.TrimSpace(p.Purpose)
	if purpose == "" {
		// HasMintIdentity already required WorkDir; empty purpose reads as
		// work on /api/agents (same as pre-T114 rows — see 🎯T301).
		purpose = claudia.PurposeWork
	}
	model := cli.BindSessionModel(p.Model, prov)
	goal := strings.TrimSpace(p.Goal)
	if goal == "" {
		goal = WorkSessionGoal(purpose, p.TargetID, "", true)
	}
	return claudia.AgentDef{
		Name:         p.Agent,
		WorkDir:      p.WorkDir,
		Model:        model,
		Provider:     prov,
		SessionID:    p.NewSessionID,
		AutoStart:    true,
		Parent:       p.Parent,
		Purpose:      purpose,
		TargetID:     p.TargetID,
		SandboxMode:  CodexWorkSandbox(prov, purpose, ""),
		Goal:         goal,
		MCPExclusive: mcpattach.Exclusive,
		Materialized: false,
	}
}
