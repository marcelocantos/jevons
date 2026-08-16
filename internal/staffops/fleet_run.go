// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package staffops

import (
	"fmt"
	"strings"
)

// Fleet-run causes (🎯T407). These are daemon-held reasons the fleet cannot
// run. Ready-leaf count is deliberately not an input: a paused or quota-
// blocked fleet can have dozens of ready leaves, and that is not neglect.
const (
	// BlockNone: the fleet can run. Genuine unattended-ready is a stall.
	BlockNone = ""
	// BlockAutoSpawnPaused: config frontier_consume.disabled (owner pause).
	BlockAutoSpawnPaused = "auto_spawn_paused"
	// BlockProviderQuota: recent provider_failure failure_class=rate_limit.
	BlockProviderQuota = "provider_quota"
	// BlockProviderAuth: recent provider_failure failure_class=auth.
	BlockProviderAuth = "provider_auth"
)

// FleetBlockObs is daemon-held evidence that the fleet cannot run (🎯T407).
// Empty Cause means it can. The sentinel reports this cause instead of
// stall:frontier / po_stall, and must not instruct the PO to spawn.
type FleetBlockObs struct {
	Cause  string
	Detail string
}

// Blocked reports whether the fleet cannot run.
func (b FleetBlockObs) Blocked() bool {
	return strings.TrimSpace(b.Cause) != ""
}

// FleetRunEvidence is the daemon-held input to ClassifyFleetRun.
// Ready-leaf count is not a field — a classifier that blocked on depth
// alone would pass every paused-fleet check and fail the healthy fixture.
type FleetRunEvidence struct {
	// AutoSpawnPaused is config frontier_consume.disabled (or equivalent).
	AutoSpawnPaused bool
	// ProviderFailures are recent provider_failure rows with
	// failure_class in {auth, rate_limit}. The harness already applied
	// the event window; this function does not re-filter by time.
	ProviderFailures []ProviderFailureObs
}

// ProviderFailureObs is one clustered provider-failure class.
type ProviderFailureObs struct {
	// Class is the stable failure_class: auth | rate_limit.
	Class string
	// Count is how many recent events carried this class.
	Count int
	// Detail is a sample raw/msg line for the wire.
	Detail string
}

// FleetRunVerdict is whether the fleet can run, and why not if it cannot.
type FleetRunVerdict struct {
	Runnable bool
	Cause    string
	Detail   string
}

// ClassifyFleetRun answers whether the fleet can run from daemon-held
// evidence (🎯T407). Pause is the standing owner order and wins when set,
// even if old quota events linger in the window. Otherwise auth, then
// rate_limit. Zero evidence ⇒ runnable.
func ClassifyFleetRun(ev FleetRunEvidence) FleetRunVerdict {
	if ev.AutoSpawnPaused {
		return FleetRunVerdict{
			Cause:  BlockAutoSpawnPaused,
			Detail: "frontier_consume.disabled",
		}
	}
	var authN, quotaN int
	var authD, quotaD string
	for _, f := range ev.ProviderFailures {
		switch strings.ToLower(strings.TrimSpace(f.Class)) {
		case "auth":
			n := f.Count
			if n < 1 {
				n = 1
			}
			authN += n
			if authD == "" {
				authD = firstNonEmpty(f.Detail, "provider_failure failure_class=auth")
			}
		case "rate_limit":
			n := f.Count
			if n < 1 {
				n = 1
			}
			quotaN += n
			if quotaD == "" {
				quotaD = firstNonEmpty(f.Detail, "provider_failure failure_class=rate_limit")
			}
		}
	}
	if authN > 0 {
		return FleetRunVerdict{
			Cause:  BlockProviderAuth,
			Detail: fmt.Sprintf("provider_failure auth count=%d %s", authN, authD),
		}
	}
	if quotaN > 0 {
		return FleetRunVerdict{
			Cause:  BlockProviderQuota,
			Detail: fmt.Sprintf("provider_failure rate_limit count=%d %s", quotaN, quotaD),
		}
	}
	return FleetRunVerdict{Runnable: true}
}

// AsObs projects a verdict onto ObserveInput.FleetBlock.
func (v FleetRunVerdict) AsObs() FleetBlockObs {
	if v.Runnable || v.Cause == "" {
		return FleetBlockObs{}
	}
	return FleetBlockObs{Cause: v.Cause, Detail: v.Detail}
}

// CollectProviderFailures reduces EventRows to auth/rate_limit clusters
// (🎯T407 clause 2). Other classes are ignored — a backend_unavailable
// blip is not a wall, and client_bug is not a fleet-wide pause.
func CollectProviderFailures(rows []EventRow) []ProviderFailureObs {
	type agg struct {
		n      int
		sample string
	}
	by := map[string]*agg{}
	for _, r := range rows {
		class := providerBlockClass(r)
		if class == "" {
			continue
		}
		a := by[class]
		if a == nil {
			a = &agg{}
			by[class] = a
		}
		a.n++
		if a.sample == "" {
			a.sample = firstNonEmpty(r.Msg, r.Component+" "+class)
		}
	}
	out := make([]ProviderFailureObs, 0, len(by))
	for _, class := range []string{"auth", "rate_limit"} {
		a := by[class]
		if a == nil || a.n < 1 {
			continue
		}
		out = append(out, ProviderFailureObs{
			Class: class, Count: a.n, Detail: a.sample,
		})
	}
	return out
}

// providerBlockClass returns auth or rate_limit when the row is a
// provider_failure of that class, else "".
func providerBlockClass(r EventRow) string {
	class := strings.ToLower(strings.TrimSpace(r.FailureClass))
	if class == "" {
		class = strings.ToLower(strings.TrimSpace(r.Decision))
	}
	if class != "auth" && class != "rate_limit" {
		return ""
	}
	comp := normalizeDrillToken(r.Component)
	msg := strings.ToLower(r.Msg)
	if comp == "provider_failure" || strings.Contains(msg, "provider_failure") || r.FailureClass != "" {
		return class
	}
	return ""
}
