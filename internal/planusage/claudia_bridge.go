// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// ResolveMint is the omit-provider dest pick (🎯T691 / 🎯T652 / 🎯T693):
// Claudia chooses a session harness. PreferPlan + prefer Claude;
// RequireUsage fails closed when no published dest remains. Ranking is
// claudia destBandRank / destBetter (under before ok; pressure tiebreak;
// hot/ahead never dest). Jevons records the pick and does not re-rank.
func ResolveMint(ctx context.Context, cands []DestCand, now time.Time, th Thresholds) (claudia.ModelPick, error) {
	ct := thresholdsToClaudia(th)
	return claudia.Resolve(ctx, claudia.ModelPredicates{
		Mode:           claudia.CapabilitySession,
		PreferPlan:     true,
		PreferProvider: claudia.ProviderClaude,
		RequireUsage:   true,
		Now:            now,
		Usage:          backendsToPlanUsage(cands),
		Thresholds:     &ct,
	})
}

// ResolveDest is the migrate/park dest pick (🎯T691 / 🎯T693): Claudia
// chooses among published dest-band dests. exclude drops the seat's
// current provider. No PreferProvider — ranking is destBandRank (locked,
// under, ok) then pressure, not Claude-first.
func ResolveDest(ctx context.Context, cands []DestCand, exclude string, now time.Time, th Thresholds) (claudia.ModelPick, error) {
	ct := thresholdsToClaudia(th)
	pred := claudia.ModelPredicates{
		Mode:         claudia.CapabilitySession,
		PreferPlan:   true,
		RequireUsage: true,
		Now:          now,
		Usage:        backendsToPlanUsage(cands),
		Thresholds:   &ct,
	}
	if e := strings.TrimSpace(exclude); e != "" {
		pred.ExcludeProviders = []claudia.Provider{claudia.Provider(e)}
	}
	return claudia.Resolve(ctx, pred)
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
