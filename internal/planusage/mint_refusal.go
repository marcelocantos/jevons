// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"fmt"
	"strings"
	"time"
)

// MintRefusalDetail says, plan by plan, why none can take a new seat
// (🎯T978): "claude: weekly ahead of pace (62% used); grok: owner override
// exhausted (reason); codex: weekly exhausted". On 2026-10-01 every spawn
// for hours was refused with "resolve: no catalog model matches
// predicates", which named no plan and no reason, and the product owner
// filed a resolver bug for what was policy working as designed.
func MintRefusalDetail(cands []DestCand, now time.Time, th Thresholds) string {
	var parts []string
	for _, c := range cands {
		be := c.Backend
		why := ""
		switch {
		case be.Status != StatusAvailable:
			why = "no reading (" + strings.TrimSpace(be.Status) + ")"
		case be.Stale && !windowRolledOver(be.Windows, now):
			why = fmt.Sprintf("plan reading stale (fetched %s ago)", formatStaleAge(be.AgeSeconds))
		case be.Override != nil && !IsDestBandOverride(be.Override.Band):
			why = "owner override " + string(be.Override.Band)
			if r := strings.TrimSpace(be.Override.Reason); r != "" {
				why += " (" + r + ")"
			}
		case DestEligible(be, now, th):
			if c.Cap > 0 && c.Load >= c.Cap {
				why = fmt.Sprintf("at its seat cap (%d/%d)", c.Load, c.Cap)
			} else {
				why = "eligible"
			}
		default:
			why = windowReason(be, now, th)
			if why == "not a destination" && be.Stale && windowRolledOver(be.Windows, now) {
				why = staleRolloverRefusal(be, now)
			}
		}
		parts = append(parts, c.Provider+": "+why)
	}
	if len(parts) == 0 {
		return "no plan readings"
	}
	return strings.Join(parts, "; ")
}

// windowReason names the window that makes be ineligible.
func windowReason(be Backend, now time.Time, th Thresholds) string {
	switch SessionStatusOf(be, th) {
	case SessionExhausted:
		return "session exhausted"
	case SessionLow:
		return "session low"
	}
	band := WeeklyBandOf(be, now, th)
	for _, w := range be.Windows {
		if w.Name == WindowWeekly && w.UsedPercent != nil {
			switch band {
			case BandAhead:
				return fmt.Sprintf("weekly ahead of pace (%.0f%% used)", *w.UsedPercent)
			case BandHot:
				return fmt.Sprintf("weekly hot (%.0f%% used)", *w.UsedPercent)
			case BandExhausted:
				return fmt.Sprintf("weekly exhausted (%.0f%% used)", *w.UsedPercent)
			}
		}
	}
	if band != "" {
		return "weekly " + string(band)
	}
	return "not a destination"
}

func staleRolloverRefusal(be Backend, now time.Time) string {
	var parts []string
	for _, w := range be.Windows {
		if w.ResetsAt == nil || !w.ResetsAt.Before(now) {
			continue
		}
		age := now.Sub(*w.ResetsAt)
		if age < 0 {
			age = 0
		}
		parts = append(parts, fmt.Sprintf("%s window stale (rolled %s ago, resets_at %s)",
			w.Name, formatStaleAge(int64(age/time.Second)), w.ResetsAt.UTC().Format("2006-01-02 15:04 UTC")))
	}
	if len(parts) == 0 {
		return "plan reading stale (window rolled over)"
	}
	return strings.Join(parts, "; ")
}

func formatStaleAge(ageSeconds int64) string {
	if ageSeconds < 60 {
		return fmt.Sprintf("%ds", ageSeconds)
	}
	if ageSeconds < 3600 {
		return fmt.Sprintf("%dm", ageSeconds/60)
	}
	return fmt.Sprintf("%dh%dm", ageSeconds/3600, (ageSeconds%3600)/60)
}
