// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/seatprovider"
)

// destAuthor is the placement stamp T691 records on PlanAction, now
// aliased to claudia.DecisionAuthor (🎯T1008): the pin exports it as of
// claudia v0.51.0.
const destAuthor = claudia.DecisionAuthor

// eventJournal is the optional durable-log destination for every resolve
// attempt this package makes against Claudia (🎯 cross-service audit trail).
// Set once at startup via SetEventLog; nil keeps the package usable in
// tests and logs nothing (eventlog.LogEvent is nil-journal safe).
//
// This is deliberately logged on the Jevons side even though Claudia may
// log its own resolve internals: when a resolve fails opaquely (e.g.
// "no catalog model matches predicates") and Claudia's own trace is thin
// or missing, Jevons still has a record of exactly what it asked for and
// what Claudia told it back, without needing to go spelunking in a
// second service's logs.
var (
	eventJournalMu sync.RWMutex
	eventJournal   *eventlog.Journal
)

// SetEventLog attaches the durable event journal used by ResolveMint and
// ResolveDest to record resolve attempts. Call once at server startup.
func SetEventLog(j *eventlog.Journal) {
	eventJournalMu.Lock()
	defer eventJournalMu.Unlock()
	eventJournal = j
}

func currentEventJournal() *eventlog.Journal {
	eventJournalMu.RLock()
	defer eventJournalMu.RUnlock()
	return eventJournal
}

// ResolveMint is the omit-provider dest pick (🎯T691 / 🎯T652 / 🎯T693).
// Claudia owns ranking; this adapter supplies Jevons' session-cap and
// steerability constraints. No PreferProvider: the owner-economics
// "Claude first while it has headroom" rule (🎯T561 / 🎯T583) was removed
// outright (🎯T1013.6), not relocated — an omit-provider mint is decided
// purely by claudia.Resolve's own dest-band ranking (🎯T693: locked, then
// under, then ok; slack breaks ties within a band).
func ResolveMint(ctx context.Context, cands []DestCand, now time.Time, th Thresholds) (claudia.ModelPick, error) {
	return resolvePlanCandidates(ctx, cands, "", "", true, now, th)
}

// ResolveDest is the migrate/park dest pick (🎯T691 / 🎯T693). exclude
// drops the seat's current provider. No PreferProvider — not Claude-first.
// Steerability is not filtered here: 🎯T791 scopes the exclusion to mint.
func ResolveDest(ctx context.Context, cands []DestCand, exclude string, now time.Time, th Thresholds) (claudia.ModelPick, error) {
	return resolvePlanCandidates(ctx, cands, "", claudia.Provider(exclude), false, now, th)
}

func resolvePlanCandidates(ctx context.Context, cands []DestCand, prefer, exclude claudia.Provider, steerableOnly bool, now time.Time, th Thresholds) (claudia.ModelPick, error) {
	excluded := map[claudia.Provider]bool{seatprovider.Resolve(exclude).Plan.AsProvider(): exclude != ""}
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
		provider := seatprovider.Resolve(claudia.Provider(p)).Plan.AsProvider()
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
	// The ahead/hot/exhausted veto is Claudia's own (Background below),
	// not an entry in ExcludeProviders: ExcludeProviders is the caller's
	// hard veto (a session cap, an unsteerable harness) and Resolve
	// checks it ahead of any owner pin, whereas Background is applied
	// after the 🎯T1013.1 dest-band pin, so a provider the owner has
	// pinned into a dest band still gets its chance (🎯T1008).
	var exclusions []claudia.Provider
	for _, row := range claudia.ModelCatalog() {
		if excluded[row.Provider] {
			exclusions = append(exclusions, row.Provider)
		}
	}
	exclusionStrs := make([]string, 0, len(exclusions))
	for _, p := range exclusions {
		exclusionStrs = append(exclusionStrs, string(p))
	}
	j := currentEventJournal()
	eventlog.LogEvent(j, "info", "claudia_resolve", "request", "resolve: requesting plan destination", map[string]any{
		"prefer_provider":    string(prefer),
		"exclude_provider":   string(exclude),
		"steerable_only":     steerableOnly,
		"catalog_exclusions": exclusionStrs,
		"capped":             capped,
		"unsteerable":        unsteer,
		"owner_kept_off":     overridden,
		"candidate_count":    len(cands),
	})
	pick, err := claudia.Resolve(ctx, claudia.ModelPredicates{
		Mode: claudia.CapabilitySession, Quality: claudia.ModelQualityStandard,
		PreferPlan: true, RequireUsage: true, Background: true,
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
		combined := strings.Join(parts, "; ")
		// 🎯 Logged on the Jevons side regardless of what Claudia itself
		// logs (or fails to log) for this same resolve call — this is the
		// record that would have answered "why did ge-po's resolve fail"
		// without needing to inspect Claudia's internals at all.
		eventlog.LogEvent(j, "error", "claudia_resolve", "failed", "resolve: "+combined, map[string]any{
			"prefer_provider":  string(prefer),
			"exclude_provider": string(exclude),
			"error":            combined,
			"claudia_error":    err.Error(),
		})
		return claudia.ModelPick{}, fmt.Errorf("%s", combined)
	}
	eventlog.LogEvent(j, "info", "claudia_resolve", "picked", "resolve: picked "+string(pick.Provider), map[string]any{
		"prefer_provider":  string(prefer),
		"exclude_provider": string(exclude),
		"picked_provider":  string(pick.Provider),
		"picked_model":     pick.Model,
	})
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
		LowRemainingPercent: th.LowRemainingPercent,
		ShrinkPriorK:        th.ShrinkPriorK,
		PanicAmberLn:        th.PanicAmberLn,
		PanicRedLn:          th.PanicRedLn,
		WasteUnderLn:        th.WasteUnderLn,
		WasteLockedLn:       th.WasteLockedLn,
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

func claudiaThresholdsPtr(th Thresholds) *claudia.PlanThresholds {
	ct := thresholdsToClaudia(th)
	return &ct
}
