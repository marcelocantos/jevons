// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package planusage is the jevons-side consumer of claudia's plan-usage API
// (🎯T390): how much of each backend's subscription allowance is left, and
// when it rolls over.
//
// The producer already existed. claudia 🎯T18 landed QueryPlanUsage /
// QueryAllPlanUsage — Claude OAuth five_hour→session and seven_day→weekly,
// Codex windows classified by duration, and an explicit unavailable verdict
// for Grok and Bedrock. It landed as a library API, which is what that
// target asked for, and nothing in jevons ever called it. So the owner ran a
// fleet of dozens of agents with no view of the allowance they were spending,
// and had to ask whether the metering he commissioned existed at all.
//
// This package is the missing consumer, and it is deliberately thin:
//
//   - claudia stays the single implementation. Nothing here parses a vendor
//     usage response or knows an endpoint; Convert only re-shapes what
//     claudia already decided.
//   - Unavailable is a first-class answer, never a zero. A backend that
//     publishes nothing renders as "unavailable" with the provider's own
//     reason attached. Grok SuperGrok has no public remaining API and
//     Bedrock has no subscription window at all, so those two are permanently
//     unavailable and must SAY so — a blank or a 0% would read as "you have
//     nothing left", which is a different and false statement.
//   - A cached reading that has aged out is marked stale rather than served
//     as current. A number that was true forty minutes ago is not a lie, but
//     presenting it as now is.
//
// Everything in this file is pure: readings and a clock in, a Snapshot out.
// The polling seam lives in reader.go.
package planusage

import (
	"sort"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// Status values mirror claudia's PlanUsageStatus on the wire, so the cockpit
// never has to map two vocabularies.
const (
	// StatusAvailable means the backend published at least one window.
	StatusAvailable = "available"
	// StatusUnavailable means the backend publishes no remaining/rollover.
	// Windows is empty and Reason says why — numbers are never invented.
	StatusUnavailable = "unavailable"
)

// Window names, mirroring claudia's PlanWindowName.
const (
	WindowSession = "session"
	WindowWeekly  = "weekly"
)

// DefaultStaleAfter is how old a reading may be before it is served as stale.
// Claude's five-hour window moves slowly enough that a few minutes is fine;
// a quarter hour is where "current" stops being an honest word.
const DefaultStaleAfter = 15 * time.Minute

// DefaultRefresh is the poll interval. Plan windows are hours long, so this
// is about freshness on the owner's screen, not about resolution.
const DefaultRefresh = 5 * time.Minute

// Window is one published allowance window for one backend.
type Window struct {
	// Name is "session" (short rolling window) or "weekly", or the
	// provider's own label when it publishes a window we cannot classify.
	Name string `json:"name"`
	// RemainingPercent is 0–100 still available, when published.
	RemainingPercent *float64 `json:"remaining_percent,omitempty"`
	// UsedPercent is 0–100 consumed, when published.
	UsedPercent *float64 `json:"used_percent,omitempty"`
	// ResetsAt is the next rollover, when published.
	ResetsAt *time.Time `json:"resets_at,omitempty"`
}

// Backend is one provider's plan-usage picture.
type Backend struct {
	Provider string `json:"provider"`
	// Status is StatusAvailable or StatusUnavailable.
	Status string `json:"status"`
	// Reason explains an unavailable status in the provider's own words.
	Reason string `json:"reason,omitempty"`
	// PlanType is the provider's plan label when published ("pro", …).
	PlanType string `json:"plan_type,omitempty"`
	// Windows is empty for an unavailable backend.
	Windows []Window `json:"windows,omitempty"`
	// FetchedAt is when this reading was taken.
	FetchedAt time.Time `json:"fetched_at"`
	// Stale is true when the reading is older than the staleness bound. A
	// stale reading is still shown — with its age — because "we last saw
	// 62% forty minutes ago" beats showing nothing.
	Stale bool `json:"stale,omitempty"`
	// AgeSeconds is how old the reading is at snapshot time.
	AgeSeconds int64 `json:"age_seconds,omitempty"`
	// FleetAgents is how many registered fleet agents are running on this
	// backend right now — this is what makes a backend one the fleet is
	// "actually running on" rather than one we merely support.
	FleetAgents int `json:"fleet_agents,omitempty"`
}

// Available reports whether this backend published anything usable.
func (b Backend) Available() bool { return b.Status == StatusAvailable && len(b.Windows) > 0 }

// Window returns the named window and whether it was published.
func (b Backend) Window(name string) (Window, bool) {
	for _, w := range b.Windows {
		if strings.EqualFold(w.Name, name) {
			return w, true
		}
	}
	return Window{}, false
}

// Snapshot is the whole cockpit-facing picture.
type Snapshot struct {
	At time.Time `json:"at"`
	// Pending is true before the first fetch completes: the daemon has
	// wired plan usage but has not heard from any backend yet. Distinct
	// from every backend being unavailable, which is an answer.
	Pending bool `json:"pending,omitempty"`
	// Error is a whole-query failure (claudia refused the arguments). A
	// per-backend failure is not an error — it is an unavailable Backend.
	Error    string    `json:"error,omitempty"`
	Backends []Backend `json:"backends,omitempty"`
}

// Backend returns the named provider's entry.
func (s Snapshot) Backend(provider string) (Backend, bool) {
	for _, b := range s.Backends {
		if strings.EqualFold(b.Provider, provider) {
			return b, true
		}
	}
	return Backend{}, false
}

// SupportedProviders are the backends claudia can be asked about. Grok and
// Bedrock are included deliberately: their answer is "unavailable", and the
// owner is entitled to see that stated rather than to wonder whether they
// were forgotten.
func SupportedProviders() []claudia.Provider {
	return []claudia.Provider{
		claudia.ProviderClaude,
		claudia.ProviderCodex,
		claudia.ProviderGrok,
		claudia.ProviderBedrock,
	}
}

// TightestRemaining returns the lowest published remaining fraction (0–1)
// across the backends the fleet is actually running on, and what produced it
// ("claude session"). ok is false when no running backend published a number
// — the honest answer, and the one that must never be rendered as 0.
//
// Backends with no fleet agents are ignored: an exhausted Codex window does
// not constrain a fleet running entirely on Claude.
func (s Snapshot) TightestRemaining() (fraction float64, source string, ok bool) {
	for _, b := range s.Backends {
		if b.FleetAgents <= 0 || !b.Available() {
			continue
		}
		for _, w := range b.Windows {
			if w.RemainingPercent == nil {
				continue
			}
			f := *w.RemainingPercent / 100
			if f < 0 {
				f = 0
			}
			if f > 1 {
				f = 1
			}
			if !ok || f < fraction {
				fraction, source, ok = f, b.Provider+" "+w.Name, true
			}
		}
	}
	return fraction, source, ok
}

// Convert re-shapes claudia readings into a Snapshot, stamping fleet load,
// age and staleness. It is the whole of this package's judgment, and it is
// pure so the oracle can drive it with fixtures.
//
// load maps provider name → registered agent count (mcpserver.HarnessLoad).
// staleAfter ≤ 0 disables staleness marking.
func Convert(readings []claudia.PlanUsage, load map[string]int, now time.Time, staleAfter time.Duration) Snapshot {
	snap := Snapshot{At: now}
	norm := map[string]int{}
	for k, v := range load {
		norm[strings.ToLower(strings.TrimSpace(k))] = v
	}

	for _, r := range readings {
		provider := strings.ToLower(strings.TrimSpace(string(r.Provider)))
		b := Backend{
			Provider:    provider,
			Status:      string(r.Status),
			Reason:      r.Reason,
			PlanType:    r.PlanType,
			FetchedAt:   r.FetchedAt,
			FleetAgents: norm[provider],
		}
		if b.Status == "" {
			b.Status = StatusUnavailable
		}
		for _, w := range r.Windows {
			b.Windows = append(b.Windows, Window{
				Name:             string(w.Name),
				RemainingPercent: copyFloat(w.RemainingPercent),
				UsedPercent:      copyFloat(w.UsedPercent),
				ResetsAt:         copyTime(w.ResetsAt),
			})
		}
		// A backend claiming available with nothing published is a producer
		// bug; report it as unavailable rather than render an empty row that
		// looks like a reading.
		if b.Status == StatusAvailable && len(b.Windows) == 0 {
			b.Status = StatusUnavailable
			if b.Reason == "" {
				b.Reason = "backend reported available but published no windows"
			}
		}
		if !r.FetchedAt.IsZero() {
			age := now.Sub(r.FetchedAt)
			if age < 0 {
				age = 0
			}
			b.AgeSeconds = int64(age / time.Second)
			b.Stale = staleAfter > 0 && age > staleAfter
		}
		snap.Backends = append(snap.Backends, b)
	}
	sortBackends(snap.Backends)
	return snap
}

// sortBackends puts the backends the fleet is actually running on first
// (busiest first), then the rest alphabetically. The owner's eye should land
// on the allowance being spent, not on Bedrock.
func sortBackends(bs []Backend) {
	sort.SliceStable(bs, func(i, j int) bool {
		li, lj := bs[i].FleetAgents, bs[j].FleetAgents
		if (li > 0) != (lj > 0) {
			return li > 0
		}
		if li != lj {
			return li > lj
		}
		return bs[i].Provider < bs[j].Provider
	})
}

func copyFloat(f *float64) *float64 {
	if f == nil {
		return nil
	}
	v := *f
	return &v
}

func copyTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}
