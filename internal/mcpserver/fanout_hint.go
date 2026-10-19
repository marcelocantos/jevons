// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/claudia"
)

// purposeCoordinator reports a declared product-owner or boss purpose
// (🎯T111.4). The seat name does not decide.
func purposeCoordinator(purpose string) bool {
	switch strings.ToLower(strings.TrimSpace(purpose)) {
	case purposeProductOwner, "boss":
		return true
	default:
		return false
	}
}

// FormatFanOutHints returns an oversight note when PO/boss agents have
// zero children in the registry (detectable without only RHS eyeballing).
// Empty string when nothing to report.
func FormatFanOutHints(reg *claudia.Registry, overseerName string) string {
	if reg == nil {
		return ""
	}
	var lines []string
	for _, d := range reg.List() {
		if d.Name == overseerName {
			continue
		}
		if !purposeCoordinator(d.Purpose) {
			continue
		}
		// Children = agents whose Parent is this name.
		nChild := 0
		for _, c := range reg.List() {
			if c.Parent == d.Name {
				nChild++
			}
		}
		if nChild == 0 {
			lines = append(lines, fmt.Sprintf(
				"[Fan-out check] %q has zero children — multi-slice missions should jevons_agent_start workers (🎯T111.4); solo tasks are fine",
				d.Name))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
