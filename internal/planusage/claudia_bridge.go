// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
)

// destAuthor is the placement stamp T691 records on PlanAction. The
// published pin (claudia v0.40.0) does not export DecisionAuthor —
// that landed in unpublished claudia T691. 🎯T707 measures the pin, so
// the string is local; development still consumes sibling HEAD via
// ../go.work (🎯T448).
const destAuthor = "claudia"

// ResolveMint is the omit-provider dest pick (🎯T691 / 🎯T652 / 🎯T693).
// Claudia owns ranking; this adapter supplies Jevons' session-cap and
// steerability constraints. A preference for Claude is not a ban on others.
func ResolveMint(ctx context.Context, cands []DestCand, now time.Time, th Thresholds) (claudia.ModelPick, error) {
	return resolvePlanCandidates(ctx, cands, claudia.ProviderClaude, "", true, now, th)
}

// ResolveDest is the migrate/park dest pick (🎯T691 / 🎯T693). exclude
// drops the seat's current provider. No PreferProvider — not Claude-first.
// Steerability is not filtered here: 🎯T791 scopes the exclusion to mint.
func ResolveDest(ctx context.Context, cands []DestCand, exclude string, now time.Time, th Thresholds) (claudia.ModelPick, error) {
	return resolvePlanCandidates(ctx, cands, "", claudia.Provider(exclude), false, now, th)
}

func resolvePlanCandidates(ctx context.Context, cands []DestCand, prefer, exclude claudia.Provider, steerableOnly bool, now time.Time, th Thresholds) (claudia.ModelPick, error) {
	excluded := map[claudia.Provider]bool{cli.PlanProvider(exclude): exclude != ""}
	usage := make([]claudia.PlanUsage, 0, len(cands))
	// 🎯T1013.1: the owner's band override is an input to claudia.Resolve
	// itself, not a filter jevons applies before calling it. Jevons used
	// to pre-exclude an override-exhausted provider here (🎯T987) and
	// separately inject an explicit pin before ever calling Resolve
	// (🎯T948, mintProviderPick/planOverrideMint) — two call-site seams
	// that had to agree with each other and with Resolve's own ranking.
	// On 2026-10-02 a bare start bypassed the first seam and landed on
	// grok while its override said the quota was dangerously low.
	// ownerOverride is now just carried through for Resolve to apply.
	ownerOverride := map[claudia.Provider]claudia.OwnerOverride{}
	var capped, unsteer, overridden []string
	for _, c := range cands {
		p := strings.ToLower(strings.TrimSpace(c.Provider))
		if p == "" {
			p = strings.ToLower(strings.TrimSpace(c.Backend.Provider))
		}
		if p == "" {
			continue
		}
		provider := cli.PlanProvider(claudia.Provider(p))
		u := backendToPlanUsage(c.Backend)
		u.Provider = provider
		usage = append(usage, u)
		if ov := c.Backend.Override; ov != nil {
			ownerOverride[provider] = claudia.OwnerOverride{
				Band: claudia.PlanBand(ov.Band), Reason: ov.Reason,
			}
			overridden = append(overridden, fmt.Sprintf("%s (owner override %s: %s)", p, ov.Band, ov.Reason))
		}
		if why := UnsteerableReason(p); steerableOnly && why != "" {
			unsteer = append(unsteer, fmt.Sprintf("%s (%s)", p, why))
			excluded[provider] = true
			continue
		}
		if destAtSessionCap(c) {
			capped = append(capped, fmt.Sprintf("%s %d/%d", p, c.Load, c.Cap))
			excluded[provider] = true
		}
	}
	cth := claudiaThresholdsPtr(th)
	// Overspend exclusion is a readings-only veto (ahead/hot/exhausted).
	// A dest-band owner override is exactly the case where the readings
	// are overruled — claudia.Resolve's OwnerOverride pin would never
	// get a chance to run if jevons' own ExcludeProviders already
	// dropped the row first (ExcludeProviders is checked ahead of any
	// pin inside Resolve, by design: it is the caller's hard veto, e.g.
	// a session cap or an unsteerable harness). So this veto does not
	// apply to a provider the owner has pinned into a dest band.
	for p := range overspendProviders(usage, now, cth) {
		if ov, ok := ownerOverride[p]; ok && claudia.IsDestBand(ov.Band) {
			continue
		}
		excluded[p] = true
	}
	var exclusions []claudia.Provider
	for _, row := range claudia.ModelCatalog() {
		if excluded[row.Provider] {
			exclusions = append(exclusions, row.Provider)
		}
	}
	pick, err := claudia.Resolve(ctx, claudia.ModelPredicates{
		Mode: claudia.CapabilitySession, Quality: claudia.ModelQualityStandard,
		PreferPlan: true, RequireUsage: true,
		PreferProvider: prefer, ExcludeProviders: exclusions,
		Usage: usage, Now: now, Thresholds: cth,
		OwnerOverride: ownerOverride,
	})
	if err != nil {
		parts := []string{err.Error()}
		if len(capped) > 0 {
			parts = append(parts, "soft cap reached: "+strings.Join(capped, ", "))
		}
		if len(unsteer) > 0 {
			parts = append(parts, "excluded unsteerable: "+strings.Join(unsteer, ", "))
		}
		if len(overridden) > 0 {
			parts = append(parts, "owner override: "+strings.Join(overridden, ", "))
		}
		return claudia.ModelPick{}, fmt.Errorf("%s", strings.Join(parts, "; "))
	}
	return pick, nil
}

// destAtSessionCap reports a dest whose published session soft cap is
// exhausted (🎯T715). Cap <= 0 is unpublished — not a skip. A mutation
// that stops threading Cap (zero value) restores pin-first resolution
// when the pin is at cap.
func destAtSessionCap(c DestCand) bool {
	if c.Cap <= 0 {
		return false
	}
	return c.Load >= c.Cap
}

func windowToClaudia(w Window) claudia.PlanWindow {
	pw := claudia.PlanWindow{
		Name:             claudia.PlanWindowName(w.Name),
		Model:            w.Model,
		UsedPercent:      w.UsedPercent,
		RemainingPercent: w.RemainingPercent,
		ResetsAt:         w.ResetsAt,
	}
	if w.LimitWindowSeconds != nil && *w.LimitWindowSeconds > 0 {
		pw.LimitWindow = time.Duration(*w.LimitWindowSeconds) * time.Second
	}
	return pw
}

func thresholdsToClaudia(th Thresholds) claudia.PlanThresholds {
	return claudia.PlanThresholds{
		WarmupElapsedPercent:     th.WarmupElapsedPercent,
		EarlyAlarmUsedPercent:    th.EarlyAlarmUsedPercent,
		LowRemainingPercent:      th.LowRemainingPercent,
		CriticalRemainingPercent: th.CriticalRemainingPercent,
		DampLambdaPercent:        th.DampLambdaPercent,
		AheadMarginPercent:       th.AheadMarginPercent,
		ShrinkPriorK:             th.ShrinkPriorK,
		PanicAmberLn:             th.PanicAmberLn,
		PanicRedLn:               th.PanicRedLn,
		WasteUnderLn:             th.WasteUnderLn,
		WasteLockedLn:            th.WasteLockedLn,
	}
}

func backendsToPlanUsage(cands []DestCand) []claudia.PlanUsage {
	var out []claudia.PlanUsage
	for _, c := range cands {
		p := claudia.Provider(c.Provider)
		if p == "" {
			p = claudia.Provider(c.Backend.Provider)
		}
		u := claudia.PlanUsage{
			Provider:  p,
			Status:    claudia.PlanUsageStatus(c.Backend.Status),
			Reason:    c.Backend.Reason,
			PlanType:  c.Backend.PlanType,
			FetchedAt: c.Backend.FetchedAt,
		}
		for _, w := range c.Backend.Windows {
			u.Windows = append(u.Windows, windowToClaudia(w))
		}
		out = append(out, u)
	}
	return out
}

// classifyPlan is claudia.ClassifyPlan with 🎯T677 applied first: an
// unreadable backend is unpublished, never exhausted. The published pin
// (v0.40.0) checks the reason before the status, so a 429 from the usage
// meter comes back as a spent allowance; claudia master has the T677
// ordering but no tag carries it yet. Once the pin does, this guard is a
// no-op and the verdict is claudia's alone.
func classifyPlan(be Backend, now time.Time, th Thresholds) claudia.PlanVerdict {
	u := backendToPlanUsage(be)
	if u.Status != claudia.PlanUsageAvailable {
		return claudia.PlanVerdict{
			Usage:   u,
			Weekly:  claudia.PlanBandUnpublished,
			Session: claudia.PlanSessionUnpublished,
		}
	}
	return claudia.ClassifyPlan(u, now, claudiaThresholdsPtr(th))
}

func backendToPlanUsage(be Backend) claudia.PlanUsage {
	out := backendsToPlanUsage([]DestCand{{Provider: be.Provider, Backend: be}})
	if len(out) == 0 {
		return claudia.PlanUsage{
			Provider: claudia.Provider(be.Provider),
			Status:   claudia.PlanUsageStatus(be.Status),
			Reason:   be.Reason,
		}
	}
	// Keep stale readings visible in the cockpit, but never let them move
	// a seat or admit a new one. Claudia sees an unavailable policy input.
	if be.Stale {
		out[0].Status = claudia.PlanUsageUnavailable
		out[0].Reason = "stale plan reading"
	}
	return out[0]
}

// overspendProviders names plans whose weekly band is ahead, hot, or
// exhausted. The pinned Resolve skips hot and exhausted on its own;
// ahead is the Background predicate that landed after v0.42.0.
func overspendProviders(usage []claudia.PlanUsage, now time.Time, th *claudia.PlanThresholds) map[claudia.Provider]bool {
	out := map[claudia.Provider]bool{}
	for _, u := range usage {
		if u.Status != "" && u.Status != claudia.PlanUsageAvailable {
			continue
		}
		switch claudia.ClassifyPlan(u, now, th).Weekly {
		case claudia.PlanBandAhead, claudia.PlanBandHot, claudia.PlanBandExhausted:
			out[cli.PlanProvider(u.Provider)] = true
		}
	}
	return out
}

func claudiaThresholdsPtr(th Thresholds) *claudia.PlanThresholds {
	ct := thresholdsToClaudia(th)
	return &ct
}
