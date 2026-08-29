// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"

	"github.com/marcelocantos/claudia"
)

// ReportContextUnworkable delivers a 🎯T417 ceiling notice to the agent's
// parent and the overseer. Fire-and-forget: a failed notify is logged, never
// retried into a remint loop. parent may be empty — ResolveEventParent fills
// the lineage default.
func (s *Server) ReportContextUnworkable(agent, parent, text string) {
	if s == nil || strings.TrimSpace(text) == "" {
		return
	}
	agent = strings.TrimSpace(agent)
	overseer := s.overseerName()
	if overseer == "" {
		overseer = "jevons"
	}
	resolved := parent
	if s.registry != nil {
		if def := s.registry.Def(agent); def != nil {
			resolved = ResolveEventParent(*def, defaultProductPOName, overseer)
		} else {
			resolved = ResolveEventParent(claudia.AgentDef{Name: agent, Parent: parent}, defaultProductPOName, overseer)
		}
	} else if strings.TrimSpace(resolved) == "" {
		resolved = defaultProductPOName
	}

	// 🎯T561: say how to remint before the supervisor reaches for migrate.
	if plan, ok := s.contextRemintPlan(agent, false); ok {
		text = strings.TrimRight(text, "\n") + "\n" + plan.Advice(agent) + "\n"
	}

	targets := []string{}
	seen := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || name == agent || seen[name] {
			return
		}
		seen[name] = true
		targets = append(targets, name)
	}
	add(resolved)
	add(overseer)

	for _, name := range targets {
		if _, err := s.deliverByName(name, text, OriginAgent, false); err != nil {
			slog.Warn("🎯T417 unworkable notice failed",
				"component", "ctxcap",
				"agent", agent,
				"target", name,
				"err", err)
			continue
		}
		slog.Info("🎯T417 unworkable notice delivered",
			"component", "ctxcap",
			"agent", agent,
			"target", name)
	}
}
