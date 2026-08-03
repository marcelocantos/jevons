// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/claudia"
)

// looksLikePOOrBoss is a thin name heuristic for multi-slice coordinators
// that should fan out (🎯T111.4). Does not ban legitimate solo agents.
func looksLikePOOrBoss(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	if strings.HasSuffix(n, "-po") || strings.HasSuffix(n, "_po") {
		return true
	}
	if strings.Contains(n, "-boss") || strings.Contains(n, "_boss") || strings.HasSuffix(n, "boss") {
		return true
	}
	// Common product-owner handles.
	if strings.HasSuffix(n, "-owner") || n == "po" {
		return true
	}
	return false
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
		if !looksLikePOOrBoss(d.Name) {
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
