// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"fmt"
	"strings"
)

// GateRole is a declared role for one cited gate id (🎯T722). Repeatable.
// The false-green checker reads this rather than inferring from the GATE
// name alone: a worker who names a failing run "foo-red" is not declaring
// a control, but `jevons: gate-role <id> control` is.
type GateRole struct {
	ID   string
	Role string
}

// GateRoleControl is the 🎯T722 role: a red-before / mutation / rebased
// control, not a pass. Alias "before" parses as this.
const GateRoleControl = "control"

// IsControl reports whether r names a red-before control.
func (r GateRole) IsControl() bool {
	return strings.EqualFold(strings.TrimSpace(r.Role), GateRoleControl)
}

// ControlIDs collects every declared control gate id in text, including
// slots that sit outside a line-1 fence. The checker uses this so a
// finish report that opens with prose (then a fence) still has its
// declarations read.
func ControlIDs(text string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := parseSlotLine(strings.TrimSpace(line))
		if !ok || (key != "gate-role" && key != "gaterole") {
			continue
		}
		r, err := parseGateRole(value)
		if err != nil || !r.IsControl() {
			continue
		}
		out[r.ID] = true
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
	switch strings.ToLower(role) {
	case GateRoleControl, "before":
		role = GateRoleControl
	default:
		role = strings.ToLower(role)
	}
	return GateRole{ID: id, Role: role}, nil
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
