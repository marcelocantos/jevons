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
