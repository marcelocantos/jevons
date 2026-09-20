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
//
// 🎯T727, why this path carries no occurrence. It does not go through
// notifyFleetHealth, so nothing forces one on it; that is deliberate rather
// than an omission. The emitter is once-per-spell — contextCeilingPass holds a
// sticky latch per agent and clears it only when the seat drops back under the
// ceiling — so a second spell is a genuinely new incident that must reach the
// overseer. It already does, because FormatUnworkableNotice puts two moving
// quantities in the text: the measured context size and the rotation/compaction
// age. A seat that fell below the ceiling and climbed back is re-measured, so
// the second notice differs and 🎯T428 passes it through.
//
// THE ACCEPTED LOSS, stated so it is not discovered as a bug: two spells whose
// notices render byte-identical — the same token count to the digit AND a
// rotation age that formats the same, which in practice means a parked seat
// with "none recorded" both times — collapse as a replay and the second spell
// is never announced. Closing that would mean stamping a discriminator into a
// notice whose wording is deliberately pinned pure, to cover a case that needs
// an exact token-count collision. The fleet knowingly accepts losing it.
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
