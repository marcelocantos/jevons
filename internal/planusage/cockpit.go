// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"fmt"
	"strings"

)

// ShowOnBar is the cockpit filter: idle Bedrock stays off the ticker
// (no subscription remaining). Everything else — including unpublished
// Grok and exhausted Claude — stays so the owner can see why.
func ShowOnBar(b Backend) bool {
	if strings.EqualFold(b.Provider, "bedrock") && !b.Available() && b.FleetAgents <= 0 {
		return false
	}
	return true
}

// CockpitSnapshot used to rewrite a rate-limited backend as available
// with session and weekly at 0%, so the ticker would paint spent bars
// rather than a collapsed icon. 🎯T677 removed that: the 429 comes from
// the usage endpoint, not the plan, and painting a failed reading as a
// spent allowance is how a live provider came to look empty and a live
// worker came to be parked. An unreadable backend now travels as what it
// is — unavailable, with its reason — and the cockpit paints it as no
// reading (🎯T681).
func CockpitSnapshot(snap Snapshot) Snapshot {
	return snap
}

func floatPtr(v float64) *float64 {
	p := v
	return &p
}

// FormatCockpit is the overseer-facing text for 🎯T390.1.4: the same
// per-provider remaining the header ticker paints, plus a one-line
// route hint (most weekly remaining / who is exhausted).
func FormatCockpit(snap Snapshot) string {
	view := CockpitSnapshot(snap)
	var b strings.Builder
	b.WriteString("Plan remaining (header ticker)\n")
	if view.Pending {
		b.WriteString("  pending first fetch — no backend has answered yet\n")
		return b.String()
	}
	if view.Error != "" {
		fmt.Fprintf(&b, "  query failed: %s\n", view.Error)
	}
	var (
		bestProv  string
		bestRem   = -1.0
		exhausted []string
	)
	shown := 0
	for _, be := range view.Backends {
		if !ShowOnBar(be) {
			continue
		}
		shown++
		fmt.Fprintf(&b, "%s\n", formatCockpitBackend(be))
		if backendRockBottom(be) {
			exhausted = append(exhausted, be.Provider)
		}
		if w, ok := be.PrimaryAllowanceWindow(); ok && w.RemainingPercent != nil {
			if *w.RemainingPercent > bestRem {
				bestRem = *w.RemainingPercent
				bestProv = be.Provider
			}
		}
	}
	if shown == 0 && view.Error == "" && !view.Pending {
		b.WriteString("  no backends on the bar\n")
	}
	b.WriteByte('\n')
	switch {
	case len(exhausted) > 0 && bestProv != "" && bestRem >= 0:
		fmt.Fprintf(&b, "Route: most weekly remaining = %s %.0f%%. Exhausted: %s.\n",
			bestProv, bestRem, strings.Join(exhausted, ", "))
	case len(exhausted) > 0:
		fmt.Fprintf(&b, "Route: exhausted: %s. Do not start new work on those providers.\n",
			strings.Join(exhausted, ", "))
	case bestProv != "" && bestRem >= 0:
		fmt.Fprintf(&b, "Route: most weekly remaining = %s %.0f%%.\n", bestProv, bestRem)
	default:
		b.WriteString("Route: no published weekly remaining.\n")
	}
	return b.String()
}

func formatCockpitBackend(be Backend) string {
	head := be.Provider
	if be.PlanType != "" {
		head += " (" + be.PlanType + ")"
	}
	if be.FleetAgents > 0 {
		head += fmt.Sprintf(" — %d agents", be.FleetAgents)
	}
	if be.Stale {
		head += " stale"
	}
	if !be.Available() {
		why := be.Reason
		if why == "" {
			why = "no plan-remaining published"
		}
		return fmt.Sprintf("  %s  unavailable — %s", head, why)
	}
	if backendRockBottom(be) {
		head += "  EXHAUSTED"
	}
	var parts []string
	for _, w := range be.Windows {
		label := w.Name
		if w.RemainingPercent != nil {
			label += fmt.Sprintf(" %.0f%%", *w.RemainingPercent)
		}
		if w.ResetsAt != nil {
			label += " rolls " + w.ResetsAt.Format("2 Jan 2006")
		}
		parts = append(parts, label)
	}
	line := fmt.Sprintf("  %s  %s", head, strings.Join(parts, "  "))
	return line
}

func backendRockBottom(be Backend) bool {
	if !be.Available() {
		return false
	}
	any := false
	for _, w := range be.Windows {
		if w.RemainingPercent == nil {
			continue
		}
		any = true
		if *w.RemainingPercent > 0 {
			return false
		}
	}
	return any
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		return s[:157] + "..."
	}
	return s
}
