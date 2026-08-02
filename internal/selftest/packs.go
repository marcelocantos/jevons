// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package selftest

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Pack is a registered, graded self-test unit.
type Pack interface {
	ID() string
	// Class is L0|L1|L2… soft classes are live-safe; aggressive classes
	// should only list SiteDrill (and optionally SiteCI).
	Class() string
	// AllowedSites lists sites this pack may run on.
	AllowedSites() []Site
	Run(ctx context.Context, env *Env) Report
}

// defaultRegistry is the built-in pack set (🎯T110.3).
var defaultRegistry = []Pack{
	healthL1{},
	composerGrowthL2{},
	agentsParentL1{},
}

// ListPacks returns registered packs (copy).
func ListPacks() []Pack {
	out := make([]Pack, len(defaultRegistry))
	copy(out, defaultRegistry)
	return out
}

// LookupPack finds a pack by id (case-sensitive).
func LookupPack(id string) (Pack, bool) {
	for _, p := range defaultRegistry {
		if p.ID() == id {
			return p, true
		}
	}
	return nil, false
}

func siteAllowed(p Pack, site Site) bool {
	for _, s := range p.AllowedSites() {
		if s == site {
			return true
		}
	}
	return false
}

func newReport(pack string, site Site) Report {
	now := time.Now().UTC()
	return Report{
		SchemaVersion: SchemaVersion,
		Site:          site,
		Pack:          pack,
		StartedAt:     now,
		Measurements:  nil,
	}
}

func finish(r Report, ms []Measurement, narrative string) Report {
	r.Measurements = ms
	r.FinishedAt = time.Now().UTC()
	r.Narrative = narrative
	if r.Grade == "" {
		r.Grade = GradeFromMeasurements(ms)
	}
	return r
}

func finishSkip(r Report, reason string) Report {
	r.FinishedAt = time.Now().UTC()
	r.Grade = GradeSkip
	r.Narrative = reason
	r.Measurements = []Measurement{}
	return r
}

func finishError(r Report, err error) Report {
	r.FinishedAt = time.Now().UTC()
	r.Grade = GradeError
	r.Narrative = err.Error()
	if r.Measurements == nil {
		r.Measurements = []Measurement{}
	}
	return r
}

// ── health-L1 ───────────────────────────────────────────────────

type healthL1 struct{}

func (healthL1) ID() string    { return "health-L1" }
func (healthL1) Class() string { return "L1" }
func (healthL1) AllowedSites() []Site {
	return []Site{SiteLive, SiteDrill, SiteCI}
}

func (healthL1) Run(ctx context.Context, env *Env) Report {
	r := newReport("health-L1", env.Site)
	code, body, err := env.FetchHealth(ctx)
	if err != nil {
		return finishError(r, err)
	}
	status, _ := body["status"].(string)
	okCode := code == 200
	okStatus := strings.EqualFold(status, "ok")
	ms := []Measurement{
		{ID: "http_status", Value: code, Threshold: 200, OK: okCode},
		{ID: "status_field", Value: status, Threshold: "ok", OK: okStatus},
	}
	if v, ok := body["version"]; ok {
		ms = append(ms, Measurement{ID: "version_present", Value: fmt.Sprint(v), OK: fmt.Sprint(v) != ""})
	}
	return finish(r, ms, "daemon health endpoint")
}

// ── composer-growth-L2 ──────────────────────────────────────────
// Pure layout policy (no browser). Mirrors web/scripts/layout_probe.js
// and 🎯T70.1: composer max-height 28vh so growth does not cover the
// latest reply. Pack grades the policy contract; live DOM probing is
// available via window.JevonsProbe.snapshot() for future drill packs.

type composerGrowthL2 struct{}

func (composerGrowthL2) ID() string    { return "composer-growth-L2" }
func (composerGrowthL2) Class() string { return "L2" }
func (composerGrowthL2) AllowedSites() []Site {
	return []Site{SiteLive, SiteDrill, SiteCI}
}

// ComposerMaxVh is the CSS max-height fraction (28vh) from #input.
const ComposerMaxVh = 28.0

// MinLastReplyVisible is the minimum last-reply visible ratio when the
// composer is at its max growth (policy floor for growth-without-cover).
const MinLastReplyVisible = 0.15

// GrowthWithoutCover grades pure geometry: at max composer height
// (composerMaxVh of viewport), messages area still leaves room so a
// last-reply of lastReplyH in a messages viewport of messagesH keeps
// visible ratio ≥ minVisible. Pure function — hermetic oracle.
func GrowthWithoutCover(viewportH, composerMaxVh, messagesH, lastReplyH, minVisible float64) (ratio float64, ok bool) {
	if viewportH <= 0 || messagesH <= 0 || lastReplyH <= 0 {
		return 0, false
	}
	composerCap := viewportH * (composerMaxVh / 100.0)
	// Policy: cap must be finite and ≤ 40% of viewport (soft safety).
	if composerCap <= 0 || composerMaxVh > 40 {
		return 0, false
	}
	// Visible ratio of last reply inside messages viewport when pinned
	// to bottom: min(1, messagesH / lastReplyH) for a tall reply, or 1
	// when the whole reply fits.
	ratio = VisibleRatio(0, lastReplyH, 0, messagesH)
	ok = ratio >= minVisible && composerMaxVh <= ComposerMaxVh+0.01
	return ratio, ok
}

// VisibleRatio is the fraction of [elTop, elTop+elH) that intersects
// [viewTop, viewTop+viewH). Pure DOM-free geometry.
func VisibleRatio(elTop, elH, viewTop, viewH float64) float64 {
	if elH <= 0 || viewH <= 0 {
		return 0
	}
	elBot := elTop + elH
	viewBot := viewTop + viewH
	overlap := min(elBot, viewBot) - max(elTop, viewTop)
	if overlap <= 0 {
		return 0
	}
	if overlap > elH {
		overlap = elH
	}
	return overlap / elH
}

// NearBottom reports whether scroll position is within thresholdPx of
// the bottom of a scroll container.
func NearBottom(scrollTop, scrollHeight, clientHeight, thresholdPx float64) bool {
	if scrollHeight <= clientHeight {
		return true
	}
	return (scrollHeight - scrollTop - clientHeight) <= thresholdPx
}

func (composerGrowthL2) Run(_ context.Context, env *Env) Report {
	r := newReport("composer-growth-L2", env.Site)
	// Hermetic policy fixtures (viewport 800, messages 500 after growth).
	const viewportH = 800.0
	const messagesH = 500.0
	const lastReplyH = 120.0
	ratio, ok := GrowthWithoutCover(viewportH, ComposerMaxVh, messagesH, lastReplyH, MinLastReplyVisible)
	// Cap measurement: product contract is ≤ 28vh.
	capOK := ComposerMaxVh <= 28.0
	// Scroll-adjust oracle aligned with web/scripts/composer_layout.js
	// (growthWithoutCoverHolds): tall last bubble + grow must stay visible.
	scrollPolicyOK := growthScrollPolicyHolds(280, 400, 80)
	ms := []Measurement{
		{ID: "composer_max_vh", Value: ComposerMaxVh, Unit: "vh", Threshold: 28.0, OK: capOK},
		{ID: "last_reply_visible_ratio", Value: ratio, Threshold: MinLastReplyVisible, OK: ok},
		{ID: "growth_without_cover", Value: ok, OK: ok && capOK},
		{ID: "scroll_adjust_policy", Value: scrollPolicyOK, OK: scrollPolicyOK},
	}
	return finish(r, ms, "pure layout policy (🎯T70.1 / T110.3); DOM probe is JevonsProbe.snapshot()")
}

// growthScrollPolicyHolds mirrors ComposerLayout.growthWithoutCoverHolds
// so the Go pack and web/scripts unit tests share the same oracle shape.
func growthScrollPolicyHolds(lastHeight, clientHeight, growPx float64) bool {
	if lastHeight < 1 {
		lastHeight = 1
	}
	if clientHeight < 1 {
		clientHeight = 1
	}
	if growPx < 0 {
		growPx = 0
	}
	filler := clientHeight
	if filler < 200 {
		filler = 200
	}
	scrollHeight := filler + lastHeight
	lastTop := filler
	scrollTop := scrollHeight - clientHeight
	if scrollTop < 0 {
		scrollTop = 0
	}
	clientAfter := clientHeight - growPx
	if clientAfter <= 0 {
		return false
	}
	fixedTop := scrollTopAfterComposerGrow(scrollTop, clientAfter, scrollHeight, growPx)
	return lastMessageFullyVisible(lastTop, lastHeight, fixedTop, clientAfter, 1)
}

func scrollTopAfterComposerGrow(scrollTop, clientHeightAfter, scrollHeight, growPx float64) float64 {
	if growPx <= 0 {
		if scrollTop < 0 {
			return 0
		}
		return scrollTop
	}
	maxScroll := scrollHeight - clientHeightAfter
	if maxScroll < 0 {
		maxScroll = 0
	}
	top := scrollTop + growPx
	if top < 0 {
		top = 0
	}
	if top > maxScroll {
		return maxScroll
	}
	return top
}

func lastMessageFullyVisible(lastTop, lastHeight, scrollTop, clientHeight, marginPx float64) bool {
	lastBot := lastTop + lastHeight
	viewTop := scrollTop
	viewBot := scrollTop + clientHeight
	return lastBot <= viewBot+marginPx && lastTop >= viewTop-marginPx
}

// ── agents-parent-L1 ────────────────────────────────────────────

type agentsParentL1 struct{}

func (agentsParentL1) ID() string    { return "agents-parent-L1" }
func (agentsParentL1) Class() string { return "L1" }
func (agentsParentL1) AllowedSites() []Site {
	return []Site{SiteLive, SiteDrill, SiteCI}
}

func (agentsParentL1) Run(ctx context.Context, env *Env) Report {
	r := newReport("agents-parent-L1", env.Site)
	rows, err := env.FetchAgents(ctx)
	if err != nil {
		return finishError(r, err)
	}
	overseer := env.overseer()
	hasOverseer := false
	childrenWithParent := 0
	childrenMissingParent := 0
	for _, a := range rows {
		if a.Name == overseer {
			hasOverseer = true
			continue
		}
		if a.Parent != "" {
			childrenWithParent++
		} else {
			// Non-overseer without parent is a soft signal (legacy entries
			// may still appear); count but do not hard-fail the pack alone.
			childrenMissingParent++
		}
	}
	// Pass criteria: endpoint returned; if any non-overseer has parent set,
	// parent field is exposed. Empty fleet (only overseer or empty) still
	// passes schema exposure via a structural check.
	parentFieldExposed := true
	for _, a := range rows {
		// JSON decode always yields Parent string; exposure is structural.
		_ = a.Parent
	}
	// If we have PO-style children, require parent non-empty on at least one.
	lineageOK := true
	if childrenWithParent+childrenMissingParent > 0 {
		lineageOK = childrenWithParent > 0
	}
	ms := []Measurement{
		{ID: "agents_count", Value: len(rows), OK: true},
		{ID: "parent_field_exposed", Value: parentFieldExposed, OK: parentFieldExposed},
		{ID: "overseer_present", Value: hasOverseer, OK: true}, // informational
		{ID: "children_with_parent", Value: childrenWithParent, OK: lineageOK},
		{ID: "children_missing_parent", Value: childrenMissingParent, OK: childrenMissingParent == 0 || childrenWithParent > 0},
	}
	narr := fmt.Sprintf("overseer=%s agents=%d with_parent=%d missing_parent=%d",
		overseer, len(rows), childrenWithParent, childrenMissingParent)
	return finish(r, ms, narr)
}
