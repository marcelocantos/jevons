// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import (
	"fmt"
	"sort"
	"strings"
)

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

	// DefaultMemoryFreeCriticalPercent is the kernel free-memory level at or
	// below which the host is out of memory (🎯T573). Free memory scales
	// linearly into headroom above it. macOS's own memorystatus daemon starts
	// killing at a level in the low teens; 20 leaves a margin for the panes
	// already minted to finish their work.
	DefaultMemoryFreeCriticalPercent = 20.0

	// Kernel memory-pressure verdicts as carried in Snapshot.HostMemoryPressure.
	MemoryPressureNormal   = "normal"
	MemoryPressureWarn     = "warn"
	MemoryPressureCritical = "critical"

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

// memoryGrindHeadroom is memory occupancy that risks the kernel paging or
// killing the fleet (🎯T566.2). Load-average is not this signal.
//
// The reading is the kernel's own free-memory level and pressure verdict
// (🎯T573). Swap occupancy used to be the reading, and on 2026-08-29 it
// refused every pane for over an hour on a 137 GB host at 75% free: Apple
// Silicon never shrinks a swapfile after pressure subsides, so a full swap is
// a scar of past pressure, not present pressure. When the kernel level is
// read, swap is advisory text only and never a halt. Swap remains the
// fallback reading on a host that publishes no memory level.
func memoryGrindHeadroom(snap Snapshot, pol *Policy) (float64, string) {
	if pol != nil && pol.MemoryGateOff {
		// Unknown, not "fine": the same verdict a host publishing no memory
		// reading gets. Assess skips unknown dimensions, so memory stops
		// halting spawns AND stops moving the overall headroom — which is
		// what makes it eliminated rather than merely relaxed.
		return unknownHeadroom, ""
	}
	if snap.HostMemoryPressure == "" {
		return swapGrindHeadroom(snap, pol)
	}
	limit := pol.MemoryFreeCriticalPercent
	if limit <= 0 {
		limit = DefaultMemoryFreeCriticalPercent
	}
	free := float64(snap.HostMemoryFreePercent)
	h := clampFraction((free - limit) / (100 - limit))
	switch snap.HostMemoryPressure {
	case MemoryPressureCritical:
		h = 0
	case MemoryPressureWarn:
		// The kernel is already compressing: elevated (below the degrade
		// line) but not a halt — that verdict belongs to critical.
		h = min(h, pol.OwnerReserveFraction)
	}
	reason := fmt.Sprintf("memory grind: host memory %.0f%% free, kernel pressure %s (critical at %.0f%% free)",
		free, snap.HostMemoryPressure, limit)
	if snap.HostSwapTotalBytes > 0 {
		reason += fmt.Sprintf("; swap %.1fG of %.1fG is advisory only (🎯T573)",
			gib(snap.HostSwapUsedBytes), gib(snap.HostSwapTotalBytes))
	}
	return h, reason + " (🎯T566.2)"
}

// swapGrindHeadroom is the pre-🎯T573 reading, kept for hosts with no kernel
// memory level.
func swapGrindHeadroom(snap Snapshot, pol *Policy) (float64, string) {
	if snap.HostSwapTotalBytes <= 0 {
		return unknownHeadroom, ""
	}
	limit := pol.SwapCriticalFraction
	if limit <= 0 {
		limit = DefaultSwapCriticalFraction
	}
	used := float64(snap.HostSwapUsedBytes) / float64(snap.HostSwapTotalBytes)
	h := clampFraction((limit - used) / limit)
	reason := fmt.Sprintf("memory grind: host swap %.1f%% occupied (%.1fG of %.1fG, critical at %.0f%%; no kernel memory level read) (🎯T566.2)",
		used*100, gib(snap.HostSwapUsedBytes), gib(snap.HostSwapTotalBytes), limit*100)
	return h, reason
}

// loadAverageHeadroom is run-queue length per core. Ambient may glance;
// AdmitSpawn must not (🎯T566.1).
func loadAverageHeadroom(snap Snapshot, pol *Policy) (float64, string) {
	if snap.HostLoad1 <= 0 || snap.HostCores <= 0 {
		return unknownHeadroom, ""
	}
	limit := pol.LoadPerCoreCritical
	if limit <= 0 {
		limit = DefaultLoadPerCoreCritical
	}
	perCore := snap.HostLoad1 / float64(snap.HostCores)
	h := clampFraction(1 - perCore/limit)
	reason := fmt.Sprintf("host load average %.1f on %d cores is %.1f per core (ambient glance; not a Build-stop) (T566.1)",
		snap.HostLoad1, snap.HostCores, perCore)
	return h, reason
}

// hostHeadroom is the memory-grind reading (swap). Load-average used to
// fold in here; that made high load look like a melted host (🎯T566.1).
func hostHeadroom(snap Snapshot, pol *Policy) (float64, string) {
	return memoryGrindHeadroom(snap, pol)
}

// MemoryGrindBlocks reports the T566.2 memory-grind halt.
func MemoryGrindBlocks(a Assessment) bool { return memoryGrindBlocks(a) }

func memoryGrindBlocks(a Assessment) bool {
	return a.MemoryHeadroom != unknownHeadroom && a.MemoryHeadroom <= 0
}

// SeatCountBlocks reports the T566.2 seat-count / fork-bomb halt.
func SeatCountBlocks(a Assessment) bool { return seatCountBlocks(a) }

func seatCountBlocks(a Assessment) bool {
	return a.SeatHeadroom != unknownHeadroom && a.SeatHeadroom <= 0
}

// SeatBinding names the dimension that decided the seat headroom: the
// session census, or one provider's soft cap.
type SeatBinding struct {
	// Provider is the provider whose cap bound, or "" when the session
	// census bound.
	Provider string
	Used     int
	Limit    int
	// Inferred is true when the cap is this package's fallback rather than
	// a published number (🎯T463) — a made-up denominator must say so.
	Inferred bool
	Headroom float64
}

// SeatDimension reports which seat dimension is tightest. It reads the
// same numbers the headroom arithmetic reads, so a reason built from it
// cannot name a dimension other than the one that decided.
func SeatDimension(snap Snapshot, pol *Policy) SeatBinding {
	if pol == nil {
		pol = DefaultPolicy()
	}
	b := SeatBinding{Used: snap.ActiveSessions, Limit: snap.MaxSessions,
		Headroom: fraction(float64(snap.ActiveSessions), float64(snap.MaxSessions))}
	for prov, capN := range snap.ProviderSoftCaps {
		name := strings.ToLower(strings.TrimSpace(prov))
		limit := pol.providerCap(capN)
		used := snap.ProviderLoad[name]
		h := fraction(float64(used), float64(limit))
		if capN <= 0 {
			h = inferredFloor(h, pol)
		}
		if h == unknownHeadroom {
			continue
		}
		if b.Headroom == unknownHeadroom || h < b.Headroom ||
			(h == b.Headroom && b.Provider != "" && name < b.Provider) {
			b = SeatBinding{Provider: name, Used: used, Limit: limit, Inferred: capN <= 0, Headroom: h}
		}
	}
	return b
}

// seatCountReason names the dimension that actually bound.
//
// The old form printed the session numbers whenever a session bound was
// configured, including when a provider soft cap was what reached zero. On
// 2026-09-20 claude sat at 12 of a published soft cap of 12 and three Build
// spawns were refused, while the sentence every product owner read was
// "seat-count runaway: 0 live sessions of 20" — a count that was not the
// reason, from a census that could not see the fleet at all. A reason that
// names the wrong dimension is worse than no reason: it is a lead, and it
// is false. Two product owners spent an evening on a session counter that
// was not what refused them.
func seatCountReason(snap Snapshot, pol *Policy, a Assessment) string {
	return formatSeatBinding(snap, SeatDimension(snap, pol), a.SeatHeadroom)
}

func formatSeatBinding(snap Snapshot, b SeatBinding, headroom float64) string {
	if b.Provider != "" {
		cap := "published soft cap"
		if b.Inferred {
			cap = "assumed cap (none published, 🎯T463)"
		}
		census := "session census unknown"
		if snap.MaxSessions > 0 {
			census = fmt.Sprintf("session census %d of %d", snap.ActiveSessions, snap.MaxSessions)
		}
		return fmt.Sprintf("seat-count runaway: provider %s at %d of its %s %d (🎯T566.2 / T325.2; %s)",
			b.Provider, b.Used, cap, b.Limit, census)
	}
	if snap.MaxSessions > 0 {
		return fmt.Sprintf("seat-count runaway: %d live seats of %d (🎯T566.2)", snap.ActiveSessions, snap.MaxSessions)
	}
	return fmt.Sprintf("seat-count runaway: the session bound is exhausted (headroom %.0f%%) (🎯T566.2)", headroom*100)
}

// destSeatBinding is the seat dimension for one dest (🎯T715): the tighter
// of the session census and that dest's own published cap. Other dests'
// caps do not bind — claude 12/12 must not refuse a grok mint.
func destSeatBinding(dest string, snap Snapshot, pol *Policy) SeatBinding {
	if pol == nil {
		pol = DefaultPolicy()
	}
	dest = strings.ToLower(strings.TrimSpace(dest))
	session := SeatBinding{
		Used:     snap.ActiveSessions,
		Limit:    snap.MaxSessions,
		Headroom: fraction(float64(snap.ActiveSessions), float64(snap.MaxSessions)),
	}
	if dest == "" || snap.ProviderSoftCaps == nil {
		return session
	}
	capN, ok := snap.ProviderSoftCaps[dest]
	if !ok {
		return session
	}
	limit := pol.providerCap(capN)
	used := 0
	if snap.ProviderLoad != nil {
		used = snap.ProviderLoad[dest]
	}
	h := fraction(float64(used), float64(limit))
	inferred := capN <= 0
	if inferred {
		h = inferredFloor(h, pol)
	}
	destB := SeatBinding{Provider: dest, Used: used, Limit: limit, Inferred: inferred, Headroom: h}
	if session.Headroom != unknownHeadroom && (destB.Headroom == unknownHeadroom || session.Headroom < destB.Headroom) {
		return session
	}
	if destB.Headroom == unknownHeadroom {
		return session
	}
	return destB
}

func destSeatBlocks(b SeatBinding) bool {
	return b.Headroom != unknownHeadroom && b.Headroom <= 0
}

func sessionCensusFull(snap Snapshot) bool {
	return snap.MaxSessions > 0 && snap.ActiveSessions >= snap.MaxSessions
}

// anyPublishedDestHasHeadroom is true when at least one dest publishes a
// positive soft cap and is under it. No published caps → true (session
// census is the only seat lever). 🎯T715: dest-unaware AdmitSpawn must
// not refuse a pane because a *different* dest is at cap.
func anyPublishedDestHasHeadroom(snap Snapshot, pol *Policy) bool {
	if pol == nil {
		pol = DefaultPolicy()
	}
	saw := false
	for prov, capN := range snap.ProviderSoftCaps {
		if capN <= 0 {
			continue
		}
		saw = true
		used := 0
		if snap.ProviderLoad != nil {
			used = snap.ProviderLoad[strings.ToLower(strings.TrimSpace(prov))]
		}
		if used < capN {
			return true
		}
	}
	return !saw
}

// DestsWithHeadroom lists published dests still under their soft cap
// (🎯T715 clause 2). Unpublished caps are omitted — a made-up denominator
// must not look like published room.
func DestsWithHeadroom(snap Snapshot, pol *Policy) []string {
	if pol == nil {
		pol = DefaultPolicy()
	}
	type row struct {
		name      string
		used, cap int
	}
	var rows []row
	seen := map[string]bool{}
	for prov, capN := range snap.ProviderSoftCaps {
		if capN <= 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(prov))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		used := 0
		if snap.ProviderLoad != nil {
			used = snap.ProviderLoad[name]
		}
		if used < capN {
			rows = append(rows, row{name: name, used: used, cap: capN})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = fmt.Sprintf("%s (%d/%d)", r.name, r.used, r.cap)
	}
	return out
}

func destsWithHeadroomSuffix(snap Snapshot, pol *Policy) string {
	heads := DestsWithHeadroom(snap, pol)
	if len(heads) == 0 {
		return "; dests with headroom: none (every configured dest is at cap)"
	}
	return "; dests with headroom: " + strings.Join(heads, ", ")
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
