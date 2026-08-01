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
// Cross-tree escalation (request via nearest common ancestor) is deferred.
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
	return fmt.Errorf(
		"denied: %q is not an ancestor of %q — only a parent (or overseer) may kill descendants; cross-tree kill is not direct",
		actor, target,
	)
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
