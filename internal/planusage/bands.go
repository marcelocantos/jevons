// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"fmt"
	"strings"
	"time"
)

// BandInfo is one provider's weekly band with the human reason the cockpit
// paints beside it (🎯T285.2). The band is the same Go classification the
// sweep uses (WeeklyBandOf) — the fleet tree's "!" mark and the migrate
// menu's disabled rows must never re-derive it client-side.
type BandInfo struct {
	Band WeeklyBand `json:"band"`
	// Reason is the owner-facing explanation, e.g.
	// "Grok weekly hot — burn 2.5×, 15% remaining".
	Reason string `json:"reason"`
	// Eligible mirrors DestEligible: this provider may receive migrated work.
	Eligible bool `json:"eligible"`
}

// WeeklyBandDetail classifies one backend and renders the reason string the
// UI shows on the "!" tooltip and on disabled menu rows. Same inputs, same
// vertices, same damping as WeeklyBandOf — the words are the only addition.
func WeeklyBandDetail(be Backend, now time.Time, th Thresholds) BandInfo {
	band := WeeklyBandOf(be, now, th)
	return BandInfo{
		Band:     band,
		Reason:   bandReason(be, band, now, th),
		Eligible: DestEligible(be, now, th),
	}
}

func bandReason(be Backend, band WeeklyBand, now time.Time, th Thresholds) string {
	name := titleProvider(be.Provider)
	switch band {
	case BandExhausted:
		// Only a published zero reaches here now (🎯T677): a failed
		// reading classifies as unpublished, with its reason shown.
		return name + " plan allowance exhausted — 0% remaining"
	case BandUnpublished:
		if strings.TrimSpace(be.Reason) != "" {
			return name + " — " + oneLine(be.Reason)
		}
		return name + " — no plan window published"
	}
	w, ok := be.PrimaryAllowanceWindow()
	if !ok {
		return name + " — no plan window published"
	}
	winLabel := allowanceWindowLabel(w)
	used := usedPercent(w)
	rtp, hasTime := remainingTimePercent(w, now)
	burn := ""
	if used != nil && hasTime {
		elapsed := 100 - rtp
		if elapsed > 0 {
			burn = fmt.Sprintf("burn %.1f×", dampedBurn(*used, elapsed, th.DampLambdaPercent))
		}
	}
	remaining := ""
	if w.RemainingPercent != nil {
		remaining = fmt.Sprintf("%.0f%% remaining", *w.RemainingPercent)
	}
	detail := joinNonEmpty(burn, remaining)
	var head string
	switch band {
	case BandHot:
		head = name + " " + winLabel + " hot"
	case BandAhead:
		head = name + " " + winLabel + " ahead of pace"
	case BandUnder:
		head = name + " " + winLabel + " under pace"
	case BandLocked:
		head = name + " " + winLabel + " surplus locked in"
	default:
		head = name + " " + winLabel + " on pace"
	}
	reason := withDetail(head, detail)
	// The month can be half full while named models are refused. Say so
	// without letting that bucket reclassify the plan band (a spent Fable
	// window must not exhaust Claude).
	if note := includedAPINote(be); note != "" {
		reason += "; " + note
	}
	return reason
}

// NamedModelsExhausted reports that this provider's named-model bucket
// is spent. Another model of the same provider would meet the same refusal.
func NamedModelsExhausted(be Backend) bool {
	return includedAPINote(be) != ""
}

func includedAPINote(be Backend) string {
	for _, w := range be.Windows {
		if !strings.EqualFold(strings.TrimSpace(w.Model), "API") {
			continue
		}
		if w.RemainingPercent != nil && *w.RemainingPercent <= 0 {
			return "included API usage exhausted"
		}
	}
	return ""
}

func withDetail(head, detail string) string {
	if detail == "" {
		return head
	}
	return head + " — " + detail
}

func joinNonEmpty(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, ", ")
}

func titleProvider(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "provider"
	}
	return strings.ToUpper(p[:1]) + p[1:]
}
