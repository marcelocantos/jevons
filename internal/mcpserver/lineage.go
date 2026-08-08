// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"

	"github.com/marcelocantos/claudia"
)

// canKill reports whether actor may kill target under the parent→descendant
// rule. Rules:
//   - never kill the overseer
//   - self-kill allowed (agent retires itself)
//   - actor may kill target if actor is a strict ancestor of target
//   - the overseer is treated as ancestor of every registered agent
//     (including legacy agents with empty parent)
//   - peers, reverse lineage, and cross-tree kills are denied
//
// Callers that want idempotent kill (desired state = not registered) must
// short-circuit when Def(target)==nil before calling canKill — see
// handleAgentKill (🎯T229). This helper still errors on missing target so
// pure authorization tests and non-idempotent call sites stay strict.
//
// Cross-tree requests are reified as an escalation toward the nearest
// common ancestor (🎯T100 thin): the deny message names the NCA so the
// actor can route justification upward instead of a bare refusal.
// Full multi-hop skeptical parent approval workflow remains later.
func canKill(reg *claudia.Registry, actor, target string, isOverseer func(string) bool) error {
	if reg == nil {
		return fmt.Errorf("agent registry not available")
	}
	if actor == "" {
		return fmt.Errorf("actor is required (pass your agent name; overseer uses the overseer name)")
	}
	if target == "" {
		return fmt.Errorf("name is required")
	}
	if isOverseer != nil && isOverseer(target) {
		return fmt.Errorf("refusing to kill %q — that is the overseer", target)
	}
	if reg.Def(target) == nil {
		return fmt.Errorf("agent %q is not registered", target)
	}
	if actor == target {
		return nil // self-kill
	}
	if isOverseer != nil && isOverseer(actor) {
		// Root of the fleet tree may kill any non-overseer agent.
		return nil
	}
	if reg.Def(actor) == nil {
		return fmt.Errorf("actor %q is not a registered agent", actor)
	}
	if reg.IsAncestor(actor, target) {
		return nil
	}
	nca := nearestCommonAncestor(reg, actor, target)
	if nca == "" {
		return fmt.Errorf(
			"denied: %q is not an ancestor of %q — only a parent (or overseer) may kill descendants; cross-tree kill is not direct (no common ancestor to escalate to)",
			actor, target,
		)
	}
	return fmt.Errorf(
		"denied: %q is not an ancestor of %q — cross-tree kill is not direct; escalate to nearest common ancestor %q with concrete justification (what/why/blast radius)",
		actor, target, nca,
	)
}

// nearestCommonAncestor returns the deepest shared ancestor of a and b,
// or "" if none. Self is not an ancestor of self in IsAncestor; if a is
// an ancestor of b (or vice versa), that ancestor is returned.
func nearestCommonAncestor(reg *claudia.Registry, a, b string) string {
	if reg == nil || a == "" || b == "" {
		return ""
	}
	if a == b {
		return a
	}
	if reg.IsAncestor(a, b) {
		return a
	}
	if reg.IsAncestor(b, a) {
		return b
	}
	// Walk parents of a; first that is ancestor of b (or is b) wins — walk
	// from a upward so the deepest match is found first.
	seen := map[string]bool{}
	for cur := a; cur != "" && !seen[cur]; {
		seen[cur] = true
		def := reg.Def(cur)
		if def == nil {
			break
		}
		p := def.Parent
		if p == "" {
			break
		}
		if p == b || reg.IsAncestor(p, b) {
			return p
		}
		cur = p
	}
	return ""
}

// killSubtree removes target and all its descendants (children first).
// Caller must have already authorized kill of target.
func killSubtree(reg *claudia.Registry, target string) error {
	desc := reg.Descendants(target)
	// Remove leaves-first-ish: reverse order from Descendants DFS so
	// children tend to precede parents; also remove any remaining child
	// before parent by iterating until stable if needed.
	for i := len(desc) - 1; i >= 0; i-- {
		if err := reg.Remove(desc[i]); err != nil {
			return fmt.Errorf("kill descendant %q: %w", desc[i], err)
		}
	}
	if err := reg.Remove(target); err != nil {
		return fmt.Errorf("kill %q: %w", target, err)
	}
	return nil
}

// killSubtreeAndClearTurns is handleAgentKill's remove path: registry
// subtree kill plus process-local turn-began flags (🎯T305).
func (s *Server) killSubtreeAndClearTurns(target string) error {
	if s == nil || s.registry == nil {
		return fmt.Errorf("agent registry not available")
	}
	names := append(s.registry.Descendants(target), target)
	if err := killSubtree(s.registry, target); err != nil {
		return err
	}
	for _, n := range names {
		s.clearAgentTurnBegan(n)
	}
	return nil
}
