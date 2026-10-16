// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"fmt"
	"strings"
)

// GateRole is a declared role for one cited gate id (🎯T722 / 🎯T759).
// Repeatable. The false-green checker reads this rather than inferring from
// the GATE name alone: a worker who names a failing run "foo-red" is not
// declaring a control, but `jevons: gate-role <id> control` is.
type GateRole struct {
	ID   string
	Role string
}

// Declared roles. A role whose ExpectsRed is true is evidence when the cited
// gate is RED and a failure when it is GREEN (🎯T759). A role that expects a
// pass is the report's own acceptance evidence — RED there is still a
// contradiction.
const (
	// GateRoleControl is a red-before / rebased control, not a pass (🎯T722).
	// Alias "before" parses as this.
	GateRoleControl = "control"
	// GateRoleMutation is a deliberate RED that proves a guard has teeth
	// (🎯T759). GREEN here is the failure.
	GateRoleMutation = "mutation"
	// GateRoleRedDemo is a RED that is the target's acceptance deliverable —
	// "the check itself must fail when … is demonstrated by adding one"
	// (🎯T759 / claudia T100).
	GateRoleRedDemo = "red-demo"
	// GateRoleDifferential is the broken leg of a deterministic differential
	// pair (🎯T759 / claudia T93). Aliases: differential, broken-leg.
	GateRoleDifferential = "differential-broken-leg"
	// GateRoleBlocker is a RED cited as someone else's pre-existing breakage
	// or as the inherited failure that blocked the seat (🎯T759). Aliases:
	// inherited, blocker.
	GateRoleBlocker = "blocker"
	// GateRoleFalsification is a RED offered as proof that oracles have power
	// (🎯T443). Alias: falsification.
	GateRoleFalsification = "falsification"
)

// IsControl reports whether r names a red-before control.
func (r GateRole) IsControl() bool {
	return r.Role == GateRoleControl
}

// ExpectsRed reports whether a RED citation under this role is the result
// being demonstrated, not a failure being glossed over (🎯T759). A GREEN
// under such a role is the thing that deserves a flag.
func (r GateRole) ExpectsRed() bool {
	switch r.Role {
	case GateRoleControl, GateRoleMutation, GateRoleRedDemo,
		GateRoleDifferential, GateRoleBlocker, GateRoleFalsification:
		return true
	default:
		return false
	}
}

// NeedsCompanionGreen reports whether a RED under this role is only honest
// when the same report also cites a GREEN (🎯T722 control / differential).
// Mutation and red-demo are the deliverable on their own.
func (r GateRole) NeedsCompanionGreen() bool {
	switch r.Role {
	case GateRoleControl, GateRoleDifferential:
		return true
	default:
		return false
	}
}

// ControlIDs collects every declared control gate id in text, including
// slots that sit outside a line-1 fence. Kept for 🎯T722 callers; prefer
// HonestNonPassRoles when the full 🎯T759 set is needed.
func ControlIDs(text string) map[string]bool {
	out := map[string]bool{}
	for id, role := range HonestNonPassRoles(text) {
		if role.IsControl() {
			out[id] = true
		}
	}
	return out
}

// HonestNonPassRoles collects every declared gate id whose role is evidence
// when RED (🎯T759), including slots outside a line-1 fence. The checker uses
// this so a finish report that opens with prose (then a fence) still has its
// declarations read. Map value is the normalised role.
func HonestNonPassRoles(text string) map[string]GateRole {
	out := map[string]GateRole{}
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := parseSlotLine(strings.TrimSpace(line))
		if !ok || (key != "gate-role" && key != "gaterole") {
			continue
		}
		r, err := parseGateRole(value)
		if err != nil || !r.ExpectsRed() {
			continue
		}
		out[r.ID] = r
	}
	return out
}

func parseGateRole(value string) (GateRole, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return GateRole{}, fmt.Errorf("gate-role requires id and role")
	}
	var id, role string
	for _, tok := range strings.Fields(value) {
		k, v, cut := strings.Cut(tok, "=")
		if cut {
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "id", "gate-id", "gateid":
				id = strings.TrimSpace(v)
			case "role":
				role = strings.TrimSpace(v)
			}
			continue
		}
		if id == "" {
			id = tok
			continue
		}
		if role == "" {
			role = tok
		}
	}
	if id == "" || role == "" {
		return GateRole{}, fmt.Errorf("gate-role requires id and role")
	}
	role = normalizeGateRole(role)
	return GateRole{ID: id, Role: role}, nil
}

func normalizeGateRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case GateRoleControl, "before":
		return GateRoleControl
	case GateRoleMutation, "mutation-kill", "mutation-evidence":
		return GateRoleMutation
	case GateRoleRedDemo, "reddemo", "red_demo":
		return GateRoleRedDemo
	case GateRoleDifferential, "differential", "broken-leg", "broken_leg", "brokenleg":
		return GateRoleDifferential
	case GateRoleBlocker, "inherited", "blocker-inherited":
		return GateRoleBlocker
	case GateRoleFalsification, "falsify", "falsification-proof":
		return GateRoleFalsification
	default:
		return strings.ToLower(strings.TrimSpace(role))
	}
}

func gateRoleFingerprint(m *Message) string {
	if m == nil || len(m.GateRoles) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("gate-role=")
	for _, r := range m.GateRoles {
		b.WriteByte('|')
		b.WriteString(r.ID)
		b.WriteByte(':')
		b.WriteString(r.Role)
	}
	return b.String()
}
