// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"fmt"
	"strings"
)

// IsExhaustedReason reports whether an unavailable reason is the
// provider saying the allowance is gone (HTTP 429 / rate_limit), not
// "we publish no remaining number". Matches the cockpit JS
// (🎯T390.1.3): that case paints 0% session+weekly bars, not a collapsed
// icon.
func IsExhaustedReason(reason string) bool {
	s := strings.ToLower(reason)
	if s == "" {
		return false
	}
	return strings.Contains(s, "429") ||
		strings.Contains(s, "rate_limit") ||
		strings.Contains(s, "rate-limit") ||
		strings.Contains(s, "rate limited")
}

// ShowOnBar is the cockpit filter: idle Bedrock stays off the ticker
// (no subscription remaining). Everything else — including unpublished
// Grok and exhausted Claude — stays so the owner can see why.
func ShowOnBar(b Backend) bool {
	if strings.EqualFold(b.Provider, "bedrock") && !b.Available() && b.FleetAgents <= 0 {
		return false
	}
	return true
}

// CockpitSnapshot copies snap and rewrites 429/rate_limit backends the
// way the header ticker paints them: available, session+weekly at 0%.
// Unpublished Grok is left unavailable with its reason. The reader's
// stored snapshot is not mutated.
func CockpitSnapshot(snap Snapshot) Snapshot {
	out := snap
	out.Backends = make([]Backend, len(snap.Backends))
	copy(out.Backends, snap.Backends)
	for i := range out.Backends {
		b := &out.Backends[i]
		if b.Available() || !IsExhaustedReason(b.Reason) || len(b.Windows) > 0 {
			continue
		}
		zero, used := 0.0, 100.0
		b.Status = StatusAvailable
		b.Windows = []Window{
			{Name: WindowSession, RemainingPercent: floatPtr(zero), UsedPercent: floatPtr(used)},
			{Name: WindowWeekly, RemainingPercent: floatPtr(zero), UsedPercent: floatPtr(used)},
		}
	}
	return out
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
		if IsExhaustedReason(be.Reason) || backendRockBottom(be) {
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
	rock := backendRockBottom(be) || IsExhaustedReason(be.Reason)
	if rock {
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
	if IsExhaustedReason(be.Reason) && be.Reason != "" {
		line += "\n    " + oneLine(be.Reason)
	}
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
