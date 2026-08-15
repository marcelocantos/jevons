// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import "fmt"

// Host saturation as an admission dimension (🎯T463).
//
// On 2026-08-15 the fleet drove a 16-core host to a 1-minute load average of
// 247 with swap pinned at 36.3/37.9 GB, while this package concurrently
// reported pressure "normal", load headroom 1, and admitted every class at
// tier "full". Cost and tokens were fine the whole time — they were simply not
// the dimension that was saturated. A controller blind to the saturated
// resource is not a controller.
//
// The reading itself is taken in the snapshot-producing layer
// (internal/hostload); everything here is pure arithmetic over the Snapshot,
// so the classifier can be driven with a synthetic high-load sample.
//
// The ladder is not a new one: host saturation is expressed as a headroom
// fraction and folded into LoadHeadroom, so the existing 🎯T359 thresholds
// (DegradeFraction → elevated, OwnerReserveFraction → tight, 0 → critical)
// decide what happens. Owner turns and open Build missions keep their exemption
// in decide(); a saturated host defers ambient background, never the owner.

const (
	// DefaultLoadPerCoreCritical is the run-queue length per core at which the
	// host has no capacity left to give. 4 runnable threads deep per core is
	// already a machine where every build takes multiples of its normal time;
	// by 15 (the 2026-08-15 reading) processes are being SIGKILLed mid-compile.
	//
	// With the shipped fractions this one knob positions the whole ladder:
	// elevated above 2.4 per core, tight above 3.2, critical at 4.
	DefaultLoadPerCoreCritical = 4.0

	// DefaultSwapCriticalFraction is the share of configured swap in use at
	// which the host is treated as out of memory. Below it, occupancy scales
	// linearly into headroom: macOS pages out freely and a half-full swap file
	// is ordinary, but a swap file with nothing left is the state in which the
	// kernel starts killing whatever is compiling.
	DefaultSwapCriticalFraction = 0.90

	// DefaultProviderCapFallback is the concurrency cap applied to a provider
	// whose published soft cap is 0 or missing.
	//
	// This is the second half of the 2026-08-15 defect: provider_soft_caps read
	// {claude: 0, codex: 6, grok: 12} with 47 claude agents running, and 0 was
	// interpreted as "no limit" — so the provider carrying every agent was the
	// one dimension admission could not see. A cap table that fails open on its
	// busiest entry is worse than no cap table, because it reads as a bound.
	// 0 therefore means "unpublished", not "unlimited", and an unpublished cap
	// is judged against this number.
	DefaultProviderCapFallback = 12
)

// DefaultInferredCapFloor is the lowest headroom an assumed cap may report.
//
// It exists because the fix for a blind spot must not become a new outage. A
// provider that publishes no cap is judged against a number this package made
// up, and a made-up number that reaches zero headroom would put the fleet at
// PressureCritical — where even load-bearing control repair stands down, and
// nothing is left running that could unstick it. Measurements (host load,
// swap, a published cap, spent tokens) may halt the fleet; an assumption may
// only slow it down. The floor sits below OwnerReserveFraction so an assumed
// cap still reaches PressureTight: ambient background yields, control repair
// and Build and the owner keep running.
const DefaultInferredCapFloor = 0.10

// providerCap resolves the effective concurrency cap for a provider whose
// published soft cap is capN. A non-positive published cap is unpublished, not
// unlimited (🎯T463).
func (p *Policy) providerCap(capN int) int {
	if capN > 0 {
		return capN
	}
	if p.ProviderCapFallback > 0 {
		return p.ProviderCapFallback
	}
	return DefaultProviderCapFallback
}

// inferredFloor bounds a headroom derived from an assumed cap, so an
// assumption throttles the fleet without ever halting it.
func inferredFloor(h float64, pol *Policy) float64 {
	if h == unknownHeadroom {
		return h
	}
	floor := pol.OwnerReserveFraction / 2
	if floor <= 0 || floor >= pol.OwnerReserveFraction {
		floor = DefaultInferredCapFloor
	}
	return max(h, floor)
}

// hostHeadroom is the fraction of host capacity left, and the sentence
// explaining the reading. It returns unknownHeadroom when the snapshot carries
// no host reading at all — unknown and fine are different statements, and
// rendering the first as the second is the whole failure being fixed here.
func hostHeadroom(snap Snapshot, pol *Policy) (float64, string) {
	h, reason := unknownHeadroom, ""

	if snap.HostLoad1 > 0 && snap.HostCores > 0 {
		limit := pol.LoadPerCoreCritical
		if limit <= 0 {
			limit = DefaultLoadPerCoreCritical
		}
		perCore := snap.HostLoad1 / float64(snap.HostCores)
		h = clampFraction(1 - perCore/limit)
		reason = fmt.Sprintf("host load average %.1f on %d cores is %.1f× per core (critical at %.1f×) (🎯T463)",
			snap.HostLoad1, snap.HostCores, perCore, limit)
	}

	if snap.HostSwapTotalBytes > 0 {
		limit := pol.SwapCriticalFraction
		if limit <= 0 {
			limit = DefaultSwapCriticalFraction
		}
		used := float64(snap.HostSwapUsedBytes) / float64(snap.HostSwapTotalBytes)
		swap := clampFraction((limit - used) / limit)
		if h == unknownHeadroom || swap < h {
			h = swap
			reason = fmt.Sprintf("host swap %.1f%% occupied (%.1fG of %.1fG, critical at %.0f%%) (🎯T463)",
				used*100, gib(snap.HostSwapUsedBytes), gib(snap.HostSwapTotalBytes), limit*100)
		}
	}
	return h, reason
}

// hostBound reports whether the host is the dimension that decided the
// assessment — the tightest of everything known.
func hostBound(a Assessment) bool {
	return a.HostHeadroom != unknownHeadroom && a.HostHeadroom == a.Headroom
}

// deferReason names the host when the host is what bound, so a caller reading
// only the machine token can tell "the machine is full" from "the budget is
// spent". They call for opposite remedies: one is waited out, the other needs
// the owner.
func deferReason(a Assessment, fallback string) string {
	if hostBound(a) {
		return ReasonHostSaturated
	}
	return fallback
}

// clampFraction bounds a computed headroom to [0,1].
func clampFraction(f float64) float64 { return min(max(f, 0), 1) }

// gib renders bytes as gibibytes, the unit the kernel and Activity Monitor
// both use for swap.
func gib(b int64) float64 { return float64(b) / (1 << 30) }
