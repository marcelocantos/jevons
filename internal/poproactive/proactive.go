// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package poproactive implements pure PO proactive-until-empty-then-sleep
// policy (🎯T325.1). Callers assemble frontier leaf observations from ledger
// + engagement; this package does not shell to bullseye or touch the fleet
// registry. Residual: instructional doctrine + pure classifier; hard daemon
// sleep gate may follow.
package poproactive

import (
	"strings"
)

// Mode is the product-owner pass decision for 🎯T325.1.
type Mode int

const (
	// Kick: product frontier has ready (kickable) leaves — keep spawn/brief
	// until empty or blocked. Not a one-shot pass that strands work.
	Kick Mode = iota
	// Sleep: empty frontier, or only design-gated / blocked / parked /
	// already-engaged leaves remain — idle without open-mission thrash.
	Sleep
)

func (m Mode) String() string {
	switch m {
	case Kick:
		return "kick"
	case Sleep:
		return "sleep"
	default:
		return "unknown"
	}
}

// LeafKind classifies one frontier leaf for PO proactive policy.
type LeafKind int

const (
	// LeafReady: unblocked Build leaf eligible for spawn/brief.
	LeafReady LeafKind = iota
	// LeafSkipDesign: design-gated / needs-owner / design-discussion / parked-for-design.
	LeafSkipDesign
	// LeafSkipBlocked: explicit block or unmet dependency (caller-marked).
	LeafSkipBlocked
	// LeafSkipEngaged: implementer already bound — progress in flight, not stranded.
	LeafSkipEngaged
	// LeafSkipClosed: achieved / set_aside (should not be frontier; safe).
	LeafSkipClosed
	// LeafSkipSetAsideDep: graph-ready only because depends_on are set_aside
	// (T7→T5 class). Unattended consume must park, not spawn (🎯T337).
	LeafSkipSetAsideDep
	// LeafSkipHighCostMobile: multi-device / iPad megawork (visual+high cost or
	// name Mobile app) without unattended-safe / force-engage (🎯T337).
	LeafSkipHighCostMobile
	// LeafSkipParentActiveChildren: converging/identified parent with active
	// hierarchical children (T10 with T10.2–T10.6). Prefer child leaves (🎯T338).
	LeafSkipParentActiveChildren
	// LeafSkipHighInfra: sqlpipe/CGO/Peer rebuild class (cost≥13 or markers)
	// without unattended-safe / force-engage (🎯T338).
	LeafSkipHighInfra
)

func (k LeafKind) String() string {
	switch k {
	case LeafReady:
		return "ready"
	case LeafSkipDesign:
		return "skip_design"
	case LeafSkipBlocked:
		return "skip_blocked"
	case LeafSkipEngaged:
		return "skip_engaged"
	case LeafSkipClosed:
		return "skip_closed"
	case LeafSkipSetAsideDep:
		return "skip_set_aside_dep"
	case LeafSkipHighCostMobile:
		return "skip_high_cost_mobile"
	case LeafSkipParentActiveChildren:
		return "skip_parent_with_active_children"
	case LeafSkipHighInfra:
		return "skip_high_infra"
	default:
		return "unknown"
	}
}

// HighCostMobileThreshold is the cost floor for the optional mobile megawork
// unattended skip (🎯T337: tags visual + cost≥20, or name Mobile app).
const HighCostMobileThreshold = 20

// HighInfraCostThreshold is the cost floor for optional high-infra unattended
// skip (🎯T338: sqlpipe/CGO/Peer rebuild, cost≥13 multi-child sync parents).
const HighInfraCostThreshold = 13

// LeafObs is a pure observation of one product-scoped frontier leaf.
type LeafObs struct {
	ID      string
	Tags    []string
	Name    string
	Context string
	// Blocked is true when the leaf is blocked (depends unmet, explicit park).
	Blocked bool
	// AlreadyEngaged is true when a work implementer is bound to this target.
	AlreadyEngaged bool
	// Closed is true for achieved / set_aside (defensive; not kickable).
	Closed bool
	// SetAsideDeps lists depends_on that are set_aside (not achieved). When
	// non-empty the leaf is graph-ready but not unattended-safe (🎯T337).
	SetAsideDeps []string
	// ActiveChildren lists hierarchical active descendants (T10.2 of T10).
	// Non-empty ⇒ park parent; ready child leaves still spawn (🎯T338).
	ActiveChildren []string
	// Cost is the portfolio cost estimate (0 when unknown/omitted).
	Cost float64
	// ForceEngage overrides set_aside-dep, high-cost-mobile, parent-with-
	// active-children, and high-infra parks (owner/play residual; tag
	// force-engage on the leaf).
	ForceEngage bool
}

// designGateMarkers are scanned in tags, name, and context (case-insensitive).
// Aligns with T155 / T193 skip sets in the fleet standing brief.
// Prefer explicit gate phrases over bare target ids (T29/T67/T112) so
// unrelated leaves are not false-gated by substring match.
var designGateMarkers = []string{
	"design-gated",
	"needs-owner",
	"design-discussion",
	"parked-for-design",
	"blocked-on-human",
	"docs-only",
	"pure documentation",
	"t29-class",
	// 🎯T254.1 owner-parked prose: targets filed with explicit "do not build
	// yet" language must never be auto-consumed by the daemon sweep.
	"parked",
	"file only",
	"do not implement",
}

// IsDesignGatedLeaf reports whether tags/name/context mark a design-gated or
// parked leaf that the PO must not auto-spawn (T155 / T193 / T325.1 skip set).
func IsDesignGatedLeaf(tags []string, name, context string) bool {
	hay := strings.ToLower(strings.TrimSpace(name) + " " + strings.TrimSpace(context))
	for _, t := range tags {
		hay += " " + strings.ToLower(strings.TrimSpace(t))
	}
	for _, m := range designGateMarkers {
		if strings.Contains(hay, m) {
			return true
		}
	}
	return false
}

// hasTag reports a case-insensitive exact tag match (trimmed).
func hasTag(tags []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return false
	}
	for _, t := range tags {
		if strings.ToLower(strings.TrimSpace(t)) == want {
			return true
		}
	}
	return false
}

// IsUnattendedSafeTag is true when the leaf is explicitly marked safe for
// unattended auto-consume (overrides high-cost mobile and high-infra parks).
func IsUnattendedSafeTag(tags []string) bool {
	return hasTag(tags, "unattended-safe")
}

// IsForceEngageTag is true when the owner/play residual overrides T337/T338 parks.
func IsForceEngageTag(tags []string) bool {
	return hasTag(tags, "force-engage") || hasTag(tags, "force_engage")
}

// HasSetAsideDep is true when the leaf carries at least one set_aside dep.
func HasSetAsideDep(o LeafObs) bool {
	for _, d := range o.SetAsideDeps {
		if strings.TrimSpace(d) != "" {
			return true
		}
	}
	return false
}

// HasActiveChildren is true when the leaf is a hierarchical parent with at
// least one active (identified|converging) child target (🎯T338).
func HasActiveChildren(o LeafObs) bool {
	for _, c := range o.ActiveChildren {
		if strings.TrimSpace(c) != "" {
			return true
		}
	}
	return false
}

// IsHighCostMobileLeaf reports the 🎯T337 optional mobile/iPad megawork class:
// (tags include "visual" AND cost ≥ HighCostMobileThreshold) OR name matches
// Mobile app / iPad product class. unattended-safe and force-engage override
// at ClassifyLeaf time, not here.
func IsHighCostMobileLeaf(tags []string, name string, cost float64) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.Contains(n, "mobile app") || strings.Contains(n, "ipad app") {
		return true
	}
	if hasTag(tags, "mobile") || hasTag(tags, "ipad") {
		if cost >= HighCostMobileThreshold {
			return true
		}
	}
	if hasTag(tags, "visual") && cost >= HighCostMobileThreshold {
		return true
	}
	return false
}

// IsHighInfraLeaf reports the 🎯T338 optional infrastructure megawork class:
// sqlpipe / CGO / Peer rebuild markers, or cost ≥ HighInfraCostThreshold with
// multi-child parent shape (ActiveChildren ≥ 1). unattended-safe and
// force-engage override at ClassifyLeaf time, not here.
func IsHighInfraLeaf(tags []string, name, context string, cost float64, activeChildren []string) bool {
	hay := strings.ToLower(strings.TrimSpace(name) + " " + strings.TrimSpace(context))
	for _, t := range tags {
		hay += " " + strings.ToLower(strings.TrimSpace(t))
	}
	// Content markers for the T10 sqlpipe / CGO / Peer class.
	if strings.Contains(hay, "sqlpipe") || strings.Contains(hay, "cgo") {
		return true
	}
	// "peer" alone is noisy; require peer + rebuild/sync/sqlpipe-adjacent cost.
	if strings.Contains(hay, "peer") && (strings.Contains(hay, "rebuild") ||
		strings.Contains(hay, "sync") || cost >= HighInfraCostThreshold) {
		return true
	}
	if hasTag(tags, "infra") || hasTag(tags, "high-infra") {
		if cost >= HighInfraCostThreshold {
			return true
		}
	}
	// cost≥13 multi-child sync parents (defense-in-depth with ActiveChildren).
	if cost >= HighInfraCostThreshold {
		nActive := 0
		for _, c := range activeChildren {
			if strings.TrimSpace(c) != "" {
				nActive++
			}
		}
		if nActive >= 1 {
			return true
		}
	}
	return false
}

// ClassifyLeaf assigns one leaf to ready vs skip for PO proactive / frontier
// consume. Priority: closed > set_aside_dep > parent_active_children > design >
// high_cost_mobile > high_infra > blocked > engaged > ready.
// ForceEngage (or force-engage tag) overrides set_aside_dep, parent_active_
// children, high_cost_mobile, and high_infra (design/blocked still skip).
// unattended-safe overrides high_cost_mobile and high_infra only — not
// parent-with-active-children (structural; prefer ready child leaves).
func ClassifyLeaf(o LeafObs) LeafKind {
	if o.Closed {
		return LeafSkipClosed
	}
	force := o.ForceEngage || IsForceEngageTag(o.Tags)
	if !force && HasSetAsideDep(o) {
		return LeafSkipSetAsideDep
	}
	if !force && HasActiveChildren(o) {
		return LeafSkipParentActiveChildren
	}
	if IsDesignGatedLeaf(o.Tags, o.Name, o.Context) {
		return LeafSkipDesign
	}
	if !force && !IsUnattendedSafeTag(o.Tags) && IsHighCostMobileLeaf(o.Tags, o.Name, o.Cost) {
		return LeafSkipHighCostMobile
	}
	if !force && !IsUnattendedSafeTag(o.Tags) &&
		IsHighInfraLeaf(o.Tags, o.Name, o.Context, o.Cost, o.ActiveChildren) {
		return LeafSkipHighInfra
	}
	if o.Blocked {
		return LeafSkipBlocked
	}
	if o.AlreadyEngaged {
		return LeafSkipEngaged
	}
	return LeafReady
}

// Decision is the hermetic result of one proactive pass evaluation.
type Decision struct {
	Mode Mode
	// ReadyIDs are kickable leaf ids (spawn/brief targets).
	ReadyIDs []string
	// Reason is a stable code: ready_leaves | empty_frontier | only_gated_or_engaged.
	Reason string
}

// Classify decides kick vs sleep for the product-scoped frontier.
//
//	kick  — at least one ready (unblocked, non-design, unengaged) leaf
//	sleep — zero ready leaves (empty, or only gated/blocked/parked/engaged)
//
// Non-empty with only engaged leaves ⇒ sleep (progress already in flight; no
// thrash re-spawn). Non-empty with ready leaves ⇒ kick until empty/blocked.
//
// Also exported as ClassifyPOProactive for doctrine/doc greps.
func Classify(leaves []LeafObs) Decision {
	var ready []string
	anyLeaf := false
	for _, leaf := range leaves {
		if strings.TrimSpace(leaf.ID) == "" {
			continue
		}
		anyLeaf = true
		if ClassifyLeaf(leaf) == LeafReady {
			ready = append(ready, strings.TrimSpace(leaf.ID))
		}
	}
	if len(ready) > 0 {
		return Decision{
			Mode:     Kick,
			ReadyIDs: ready,
			Reason:   "ready_leaves",
		}
	}
	if !anyLeaf {
		return Decision{
			Mode:   Sleep,
			Reason: "empty_frontier",
		}
	}
	return Decision{
		Mode:   Sleep,
		Reason: "only_gated_or_engaged",
	}
}

// ClassifyPOProactive is the doctrine-facing name for Classify (🎯T325.1).
func ClassifyPOProactive(leaves []LeafObs) Decision { return Classify(leaves) }

// ClassifyFrontierLeaf is the doctrine-facing name for ClassifyLeaf.
func ClassifyFrontierLeaf(o LeafObs) LeafKind { return ClassifyLeaf(o) }

// ShouldKeepKicking is true when Classify reports kick mode.
func ShouldKeepKicking(leaves []LeafObs) bool {
	return Classify(leaves).Mode == Kick
}

// looksLikePOOrBoss mirrors mcpserver fan-out name heuristic (thin, pure).
func looksLikePOOrBoss(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	if n == "po" || strings.HasSuffix(n, "-po") || strings.HasSuffix(n, "_po") || strings.Contains(n, "-po-") {
		return true
	}
	if strings.Contains(n, "-boss") || strings.Contains(n, "_boss") || strings.HasSuffix(n, "boss") {
		return true
	}
	return false
}

// AgentObs is the minimal agent shape for open-mission thrash compose (no claudia).
type AgentObs struct {
	Name     string
	Purpose  string // work | aside | overseer; empty = work
	TargetID string
}

// OpenMissionForProactive composes 🎯T244 with T325.1: when the PO should
// sleep and has zero work children and no bound target, it is standing idle —
// not open mission (no perpetual create thrash / worker-idle noise).
// When mode is kick, or work children remain, open-mission heuristics match
// the T244 unbound-PO rule.
//
// Interruptibility is product: the PO agent stays registered and accepts
// owner/overseer directs while sleeping (no deregister). This helper only
// gates open-mission thrash classification.
//
// Also exported as POOpenMissionForProactive for doctrine greps.
func OpenMissionForProactive(d AgentObs, mode Mode, workChildCount int) bool {
	purpose := strings.TrimSpace(d.Purpose)
	if purpose == "" {
		purpose = "work"
	}
	if purpose != "work" {
		return false
	}
	tid := strings.TrimSpace(d.TargetID)
	if mode == Sleep && workChildCount <= 0 && tid == "" {
		// Standing idle after empty frontier — never thrash as open mission.
		if looksLikePOOrBoss(d.Name) {
			return false
		}
	}
	// T244: unbound PO/boss with zero work children = not open mission.
	if tid == "" {
		if looksLikePOOrBoss(d.Name) && workChildCount <= 0 {
			return false
		}
		return true // unbound implementer still visible to parent
	}
	return true // bound target open by default
}

// POOpenMissionForProactive is the doctrine-facing name for OpenMissionForProactive.
func POOpenMissionForProactive(d AgentObs, mode Mode, workChildCount int) bool {
	return OpenMissionForProactive(d, mode, workChildCount)
}
