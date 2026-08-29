// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"fmt"
	"strings"
	"time"
)

// 🎯T561 — a context blow is not a provider problem.
//
// When a seat blows the context ceiling, the cheapest recovery is a fresh
// session on the same provider with a thin continue brief: kill+start the
// same name, provider unchanged. jevons_agent_migrate refuses same-provider
// moves ("that is a resume, not a migrate"), and on 2026-08-29 that refusal
// pushed a Claude remint onto cursor merely because migrate wanted a
// different provider — while Claude's weekly window still had plenty left.
// Cross-provider migrate is the answer only when the seat's own provider is
// exhausted or blocked (MigrateOff) or the owner asks for it.

// RemintMode is how a context-blown seat is reminted.
type RemintMode string

const (
	// RemintSameProvider — kill+start the same name on the same provider
	// with a thin continue brief.
	RemintSameProvider RemintMode = "kill+start"
	// RemintMigrate — rotate onto another provider (jevons_agent_migrate).
	RemintMigrate RemintMode = "migrate"
)

// RemintPlan is the decision for one context-blown seat.
type RemintPlan struct {
	Mode RemintMode
	// Provider is the backend the successor runs on: the seat's own
	// provider for RemintSameProvider, empty (dest chosen by plan policy /
	// the caller) for RemintMigrate.
	Provider string
	Reason   string
}

// RemintArgs is the input to ContextRemintPlan.
type RemintArgs struct {
	// SeatProvider is the blown seat's current backend ("claude", …).
	SeatProvider string
	// Backend is that provider's plan-usage row; ignored when !Known.
	Backend Backend
	// Known is false when no plan feed covers SeatProvider. Unknown is
	// not exhausted — the seat stays on its provider.
	Known bool
	// OwnerAsked is true when the owner explicitly asked for a move.
	OwnerAsked bool
	Now        time.Time
	Thresholds Thresholds
}

// ContextRemintPlan decides whether a context-blown seat stays on its
// provider (kill+start, thin brief) or leaves it (migrate). Pure.
func ContextRemintPlan(a RemintArgs) RemintPlan {
	prov := strings.ToLower(strings.TrimSpace(a.SeatProvider))
	if a.OwnerAsked {
		return RemintPlan{Mode: RemintMigrate, Reason: "owner asked for a cross-provider move"}
	}
	if !a.Known {
		return RemintPlan{Mode: RemintSameProvider, Provider: prov,
			Reason: fmt.Sprintf("%s has no plan-usage reading; unknown is not exhausted — stay on %s", titleProvider(prov), prov)}
	}
	if MigrateOff(a.Backend, a.Now, a.Thresholds) {
		return RemintPlan{Mode: RemintMigrate,
			Reason: fmt.Sprintf("%s is exhausted/blocked: %s", titleProvider(prov), migrateOffReason(a.Backend, a.Now, a.Thresholds))}
	}
	detail := WeeklyBandDetail(a.Backend, a.Now, a.Thresholds)
	return RemintPlan{Mode: RemintSameProvider, Provider: prov,
		Reason: fmt.Sprintf("%s weekly is %s (%s) — context blow is not a provider problem", titleProvider(prov), detail.Band, detail.Reason)}
}

// Advice renders the plan as the one-line instruction a supervisor acts on
// (the 🎯T417 unworkable notice carries it).
func (p RemintPlan) Advice(agent string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		agent = "<name>"
	}
	switch p.Mode {
	case RemintMigrate:
		return fmt.Sprintf("Remint (🎯T561): %s — cross-provider jevons_agent_migrate is allowed for %s.", p.Reason, agent)
	default:
		return fmt.Sprintf("Remint (🎯T561): %s. Use jevons_agent_kill(%s) then jevons_agent_start(name=%s, provider=%q) with a thin continue brief — do NOT jevons_agent_migrate to another provider just because migrate needs one.",
			p.Reason, agent, agent, p.Provider)
	}
}
