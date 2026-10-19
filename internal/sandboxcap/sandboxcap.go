// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package sandboxcap is the placement gate for work that needs a local
// listener or a gate-log write (🎯T596).
//
// The 2026-08-31 incident: jv-t557-chat-ui-test was moved onto Codex and
// could not run its gate at all — "this Codex sandbox forbids localhost
// listeners and gate-log writes (listen EPERM 127.0.0.1)". The seat's whole
// job was the chat-UI suite. No files changed and no evidence was produced.
//
// This package records per-backend sandbox capabilities in an explicit
// table rather than inferring them from the backend name at each call
// site. Spawn and migrate refuse a destination that lacks a capability the
// target needs, naming the capability and the backends that still grant it.
// A seat that discovers the block mid-mission reports capability_blocked.
package sandboxcap

import (
	"fmt"
	"sort"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cli"
)

// Capability is a host-side ability a seat may need from its backend.
type Capability string

const (
	// LocalhostListener is binding a loopback port (UI/journey oracles,
	// make test-ui, journey-suite).
	LocalhostListener Capability = "localhost_listener"
	// GateLogWrite is writing under the gate store root (~/.jevons/gates),
	// which lives outside the workspace so a gate cannot be edited by the
	// work it judges (🎯T386 / 🎯T598).
	GateLogWrite Capability = "gate_log_write"
)

// Target tags that declare a need. Exact match, case-insensitive.
const (
	TagNeedsListener = "needs-listener"
	TagNeedsGateLog  = "needs-gate-log"
)

// StatusCapabilityBlocked is the finish-report status a seat writes when
// it discovers mid-mission that its backend cannot grant a required
// capability. The parent reads it the same way as other blocked statuses
// (🎯T938); the blocker names the capability.
const StatusCapabilityBlocked = "capability_blocked"

// backendCaps is the registry. Keyed by plan id (cli.PlanProvider), so
// openai-codex and codex share one row. A call site never switches on the
// provider name string — it looks up here.
//
// Codex: gate-log writes are granted by 🎯T598's writable-roots tuning, but
// loopback listen remains blocked (the original EPERM). Claude / Anthropic /
// Grok / Cursor seats run without a Codex-style sandbox and grant both.
var backendCaps = map[claudia.Provider][]Capability{
	claudia.ProviderClaude: {LocalhostListener, GateLogWrite},
	claudia.ProviderGrok:   {LocalhostListener, GateLogWrite},
	claudia.ProviderCursor: {LocalhostListener, GateLogWrite},
	claudia.ProviderCodex:  {GateLogWrite},
}

// Caps returns the sandbox capabilities a backend grants. Empty for an
// unknown plan (fail closed on placement when needs are non-empty).
func Caps(provider claudia.Provider) []Capability {
	plan := cli.PlanProvider(provider)
	if plan == "" {
		plan = claudia.Provider(strings.ToLower(strings.TrimSpace(string(provider))))
	}
	out := append([]Capability(nil), backendCaps[plan]...)
	return out
}

// Has reports whether the backend grants cap.
func Has(provider claudia.Provider, cap Capability) bool {
	for _, c := range Caps(provider) {
		if c == cap {
			return true
		}
	}
	return false
}

// NeedsFromTags returns the capabilities a target's tags declare. Only the
// exact tags needs-listener / needs-gate-log count — inference from the
// target name or acceptance is deliberately out of scope so a silent
// mis-tag cannot strand every UI leaf.
func NeedsFromTags(tags []string) []Capability {
	var out []Capability
	seen := map[Capability]bool{}
	for _, raw := range tags {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case TagNeedsListener:
			if !seen[LocalhostListener] {
				out = append(out, LocalhostListener)
				seen[LocalhostListener] = true
			}
		case TagNeedsGateLog:
			if !seen[GateLogWrite] {
				out = append(out, GateLogWrite)
				seen[GateLogWrite] = true
			}
		}
	}
	return out
}

// Missing returns the capabilities in needs that provider does not grant.
func Missing(provider claudia.Provider, needs []Capability) []Capability {
	if len(needs) == 0 {
		return nil
	}
	var out []Capability
	for _, n := range needs {
		if !Has(provider, n) {
			out = append(out, n)
		}
	}
	return out
}

// EligibleBackends lists plan ids that grant every capability in needs,
// sorted. When needs is empty every registered backend is eligible.
func EligibleBackends(needs []Capability) []string {
	var out []string
	for plan, caps := range backendCaps {
		if grantsAll(caps, needs) {
			out = append(out, string(plan))
		}
	}
	sort.Strings(out)
	return out
}

func grantsAll(have, need []Capability) bool {
	if len(need) == 0 {
		return true
	}
	set := map[Capability]bool{}
	for _, c := range have {
		set[c] = true
	}
	for _, n := range need {
		if !set[n] {
			return false
		}
	}
	return true
}

// PlacementRefusal is the non-empty reason spawn/migrate must refuse
// provider for a target that needs the listed capabilities. Empty when
// placement is allowed (no needs, or the backend grants them all).
func PlacementRefusal(provider claudia.Provider, needs []Capability) string {
	miss := Missing(provider, needs)
	if len(miss) == 0 {
		return ""
	}
	elig := EligibleBackends(needs)
	eligText := "none registered"
	if len(elig) > 0 {
		eligText = strings.Join(elig, ", ")
	}
	plan := string(cli.PlanProvider(provider))
	if plan == "" {
		plan = string(provider)
	}
	return fmt.Sprintf(
		"backend %s lacks sandbox capability %s required by the target (🎯T596); eligible backends: %s",
		plan, joinCaps(miss), eligText)
}

func joinCaps(caps []Capability) string {
	parts := make([]string, len(caps))
	for i, c := range caps {
		parts[i] = string(c)
	}
	return strings.Join(parts, ", ")
}

// BlockerLine is the jevons: blocker value a mid-mission capability_blocked
// finish-report carries.
func BlockerLine(provider claudia.Provider, missing []Capability) string {
	plan := string(cli.PlanProvider(provider))
	if plan == "" {
		plan = string(provider)
	}
	return fmt.Sprintf("sandbox capability %s unavailable on %s; eligible: %s",
		joinCaps(missing), plan, strings.Join(EligibleBackends(missing), ", "))
}

// FinishReport is a capability_blocked finish-report body a seat can emit
// when it discovers the block mid-mission instead of burning turns.
func FinishReport(targetID string, provider claudia.Provider, missing []Capability) string {
	targetID = strings.TrimSpace(strings.TrimPrefix(targetID, "🎯"))
	var b strings.Builder
	b.WriteString("```jevons\n")
	b.WriteString("jevons: kind finish-report\n")
	if targetID != "" {
		fmt.Fprintf(&b, "jevons: target %s\n", targetID)
	}
	fmt.Fprintf(&b, "jevons: status %s\n", StatusCapabilityBlocked)
	fmt.Fprintf(&b, "jevons: blocker %s\n", BlockerLine(provider, missing))
	b.WriteString("jevons: silent-ledger none\n")
	b.WriteString("```\n")
	fmt.Fprintf(&b, "Backend %s cannot grant %s; not burning further turns (🎯T596).\n",
		cli.PlanProvider(provider), joinCaps(missing))
	return b.String()
}
