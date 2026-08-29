package mcpserver

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/jevons/internal/fleetlog"
)

// 🎯T560: a kill takes one seat, not the frontier.
//
// On 2026-08-29 the overseer reminted jevons-po at its context ceiling with
// jevons_agent_kill, and the kill's implicit subtree semantics took four
// in-progress work seats with it (T540.3 pockets, T555.1, T557). A PO remint
// re-registers the same name, so the children's Parent link is valid again
// the moment the PO is back; removing them served nothing. Subtree removal
// is still available, but only as an explicit `subtree=true` decision.

// KillPlan is which registry rows a jevons_agent_kill removes and which it
// leaves registered under the killed name (their Parent link untouched, so
// a same-name remint restores lineage).
type KillPlan struct {
	Removed   []string // descendants that leave with the target (target excluded)
	Preserved []string // descendants that stay registered
}

// PlanKill decides the scope of a kill of target. Default (subtree=false)
// preserves every descendant; subtree=true removes them all. Pure.
func PlanKill(target string, descendants []string, subtree bool) KillPlan {
	var plan KillPlan
	target = strings.TrimSpace(target)
	for _, d := range descendants {
		d = strings.TrimSpace(d)
		if d == "" || d == target {
			continue
		}
		if subtree {
			plan.Removed = append(plan.Removed, d)
		} else {
			plan.Preserved = append(plan.Preserved, d)
		}
	}
	return plan
}

// killRootAndClearTurns removes only target from the registry, leaving its
// descendants registered with their Parent link intact, and clears the
// process-local turn-began flag (🎯T305) for the one seat that left.
func (s *Server) killRootAndClearTurns(target string) error {
	if s == nil || s.registry == nil {
		return fmt.Errorf("agent registry not available")
	}
	if _, err := s.RemovalAccount().Remove(s.registry, target, fleetlog.Removal{
		Reason: fleetlog.ReasonKill,
		Detail: "killed by explicit request (descendants preserved; 🎯T560)",
	}); err != nil {
		return fmt.Errorf("kill %q: %w", target, err)
	}
	s.clearAgentTurnBegan(target)
	return nil
}
