// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// destAuthor is the placement stamp T691 records on PlanAction. The
// published pin (claudia v0.40.0) does not export DecisionAuthor —
// that landed in unpublished claudia T691. 🎯T707 measures the pin, so
// the string is local; development still consumes sibling HEAD via
// ../go.work (🎯T448).
const destAuthor = "claudia"

// ResolveMint is the omit-provider dest pick (🎯T691 / 🎯T652 / 🎯T693).
// Prefer Claude among dest-band backends. Published v0.40.0 Resolve
// ranks by slack, which is the T693 false-green the sibling replace
// hid; pickDest is the pin-compat destBandRank (🎯T707).
func ResolveMint(ctx context.Context, cands []DestCand, now time.Time, th Thresholds) (claudia.ModelPick, error) {
	_ = ctx
	return pickDest(cands, string(claudia.ProviderClaude), "", now, th)
}

// ResolveDest is the migrate/park dest pick (🎯T691 / 🎯T693). exclude
// drops the seat's current provider. No PreferProvider — not Claude-first.
func ResolveDest(ctx context.Context, cands []DestCand, exclude string, now time.Time, th Thresholds) (claudia.ModelPick, error) {
	_ = ctx
	return pickDest(cands, "", exclude, now, th)
}

type destRow struct {
	provider string
	band     WeeklyBand
	pressure float64
}

func pickDest(cands []DestCand, prefer, exclude string, now time.Time, th Thresholds) (claudia.ModelPick, error) {
	prefer = strings.ToLower(strings.TrimSpace(prefer))
	exclude = strings.ToLower(strings.TrimSpace(exclude))
	var dests []destRow
	for _, c := range cands {
		p := strings.ToLower(strings.TrimSpace(c.Provider))
		if p == "" {
			p = strings.ToLower(strings.TrimSpace(c.Backend.Provider))
		}
		if p == "" || p == exclude {
			continue
		}
		if !DestEligible(c.Backend, now, th) {
			continue
		}
		dests = append(dests, destRow{
			provider: p,
			band:     WeeklyBandOf(c.Backend, now, th),
			pressure: destPressure(c.Backend, now, th),
		})
	}
	if len(dests) == 0 {
		return claudia.ModelPick{}, fmt.Errorf("resolve: no dest-band dest")
	}
	pool := dests
	if prefer != "" {
		var pref []destRow
		for _, d := range dests {
			if d.provider == prefer {
				pref = append(pref, d)
			}
		}
		if len(pref) > 0 {
			pool = pref
		}
	}
	best := pool[0]
	for _, d := range pool[1:] {
		if better, ok := destBetter(d.band, d.pressure, best.band, best.pressure); ok && better {
			best = d
		}
	}
	reason := fmt.Sprintf("band=%s", best.band)
	if prefer != "" && best.provider == prefer {
		reason += " prefer_provider"
	}
	return claudia.ModelPick{
		Provider: claudia.Provider(best.provider),
		Band:     claudia.PlanBand(best.band),
		Reason:   reason,
	}, nil
}

func destBandRank(b WeeklyBand) (int, bool) {
	switch b {
	case BandLocked:
		return 0, true
	case BandUnder:
		return 1, true
	case BandOK:
		return 2, true
	default:
		return 0, false
	}
}

func destBetter(cBand WeeklyBand, cPress float64, bestBand WeeklyBand, bestPress float64) (cBetter bool, decided bool) {
	cr, cOK := destBandRank(cBand)
	br, bOK := destBandRank(bestBand)
	if cOK != bOK {
		return cOK, true
	}
	if !cOK {
		return false, false
	}
	if cr != br {
		return cr < br, true
	}
	return slackDecides(cPress, bestPress)
}

func slackDecides(c, best float64) (cBetter bool, decided bool) {
	const slackEps = 0.05
	if c < best-slackEps {
		return true, true
	}
	if c > best+slackEps {
		return false, true
	}
	return false, false
}

// shouldVacate is the T691 bounce bar. Published v0.40.0 does not
// export claudia.ShouldVacate; the predicate is session exhausted or
// weekly hot/exhausted.
func shouldVacate(be Backend, now time.Time, th Thresholds) bool {
	v := claudia.ClassifyPlan(backendToPlanUsage(be), now, claudiaThresholdsPtr(th))
	if v.Session == claudia.PlanSessionExhausted {
		return true
	}
	switch v.Weekly {
	case claudia.PlanBandHot, claudia.PlanBandExhausted:
		return true
	default:
		return false
	}
}

// isDestBand reports locked / under / ok. hot, ahead, exhausted, and
// unpublished are never destinations (🎯T693).
func isDestBand(b WeeklyBand) bool {
	switch b {
	case BandLocked, BandUnder, BandOK:
		return true
	default:
		return false
	}
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

func backendToPlanUsage(be Backend) claudia.PlanUsage {
	out := backendsToPlanUsage([]DestCand{{Provider: be.Provider, Backend: be}})
	if len(out) == 0 {
		return claudia.PlanUsage{
			Provider: claudia.Provider(be.Provider),
			Status:   claudia.PlanUsageStatus(be.Status),
			Reason:   be.Reason,
		}
	}
	return out[0]
}

func claudiaThresholdsPtr(th Thresholds) *claudia.PlanThresholds {
	ct := thresholdsToClaudia(th)
	return &ct
}
