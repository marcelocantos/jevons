// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"time"

	"github.com/marcelocantos/claudia"
)

// ResolveMint is the omit-provider dest pick (🎯T652): Claudia chooses a
// session harness. PreferPlan + prefer Claude; exhausted / weekly-hot /
// session-low rows are skipped. Jevons does not default that mint to grok.
func ResolveMint(ctx context.Context, cands []DestCand, now time.Time, th Thresholds) (claudia.ModelPick, error) {
	ct := thresholdsToClaudia(th)
	return claudia.Resolve(ctx, claudia.ModelPredicates{
		Mode:           claudia.CapabilitySession,
		PreferPlan:     true,
		PreferProvider: claudia.ProviderClaude,
		Now:            now,
		Usage:          backendsToPlanUsage(cands),
		Thresholds:     &ct,
	})
}

func windowToClaudia(w Window) claudia.PlanWindow {
	pw := claudia.PlanWindow{
		Name:             claudia.PlanWindowName(w.Name),
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
