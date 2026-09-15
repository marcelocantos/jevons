// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package seatstop records why a fleet seat stopped and names a mass stop
// (🎯T662).
//
// On 2026-09-15 every worker under jevons-po showed stopped twice in one
// evening with no daemon bounce, and the PO's only evidence was "rehydrated
// after dead/stopped process" on its next send. Nothing had recorded why
// each seat stopped, and nothing noticed that five had stopped together. A
// stop the daemon itself performs (stop, kill, reap, park) has a reason at
// the site that performs it; a stop the daemon merely discovers (a process
// that is no longer alive at the next sweep) has none, and the honest
// record says so — "unknown" is a recorded answer, silence is not.
package seatstop

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Source classifies who or what stopped the seat.
type Source string

const (
	// SourceSupervisor: a jevons_agent_stop / jevons_agent_kill by a named actor.
	SourceSupervisor Source = "supervisor"
	// SourceReap: the finished-work or achieve reap (🎯T165 / 🎯T195).
	SourceReap Source = "reap"
	// SourcePlanPolicy: the plan-usage policy parked the seat (🎯T542).
	SourcePlanPolicy Source = "plan_policy"
	// SourceUnbriefed: an opening brief proven undelivered released the seat (🎯T387).
	SourceUnbriefed Source = "unbriefed"
	// SourceExit: the process was found not alive by a sweep; the harness
	// reported no exit status or signal, so the reason is unknown.
	SourceExit Source = "exit"
)

// Record is one seat stop.
type Record struct {
	Seat   string
	At     time.Time
	Source Source
	// Reason is the sentence agent_list and the rehydrate message repeat.
	Reason string
	Actor  string
	Detail string
}

// Unknown is the reason recorded for a stop nothing explained.
const Unknown = "unknown: process exited and no reason was recorded"

// Ledger is the in-memory record of recent stops, one entry per seat (the
// latest), plus the ordered history a mass-stop reading needs.
type Ledger struct {
	mu      sync.Mutex
	latest  map[string]Record
	history []Record
	now     func() time.Time
}

// New returns an empty ledger.
func New() *Ledger {
	return &Ledger{latest: map[string]Record{}, now: time.Now}
}

// SetClock overrides the clock (tests).
func (l *Ledger) SetClock(now func() time.Time) {
	if l == nil || now == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now = now
}

// Note records a stop. A zero At is stamped now. An empty reason is
// recorded as Unknown, never as silence.
func (l *Ledger) Note(r Record) Record {
	if l == nil || strings.TrimSpace(r.Seat) == "" {
		return r
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if r.At.IsZero() {
		r.At = l.now()
	}
	if strings.TrimSpace(r.Reason) == "" {
		r.Reason = Unknown
	}
	if r.Source == "" {
		r.Source = SourceExit
	}
	l.latest[r.Seat] = r
	l.history = append(l.history, r)
	// Bound the history: a day of stops is plenty for a one-minute window.
	if len(l.history) > 4096 {
		l.history = append([]Record(nil), l.history[len(l.history)-4096:]...)
	}
	return r
}

// Last returns the latest recorded stop for seat.
func (l *Ledger) Last(seat string) (Record, bool) {
	if l == nil {
		return Record{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.latest[seat]
	return r, ok
}

// Forget drops seat's latest record — the seat started again.
func (l *Ledger) Forget(seat string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.latest, seat)
}

// Recent returns the stops recorded at or after now-window, oldest first.
func (l *Ledger) Recent(now time.Time, window time.Duration) []Record {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-window)
	var out []Record
	for _, r := range l.history {
		if !r.At.Before(cutoff) {
			out = append(out, r)
		}
	}
	return out
}

// Alert is a mass stop: several seats stopped within one window with no
// daemon restart inside it.
type Alert struct {
	Seats  []string
	From   time.Time
	To     time.Time
	Window time.Duration
	// Shared is the reason every seat in the burst carries, or the
	// "unknown: N seats, no reason recorded" form, or "mixed: …".
	Shared string
}

// DefaultWindow is the burst width a mass stop is measured over.
const DefaultWindow = time.Minute

// DefaultMinSeats is how many distinct seats within DefaultWindow make a
// mass stop rather than a coincidence.
const DefaultMinSeats = 3

// MassStop finds the densest window-wide burst in records. bootAt is the
// daemon's own start: a burst that straddles it is a restart, not a mass
// stop, and is not reported here (the restart path has its own notice).
func MassStop(records []Record, window time.Duration, minSeats int, bootAt time.Time) (Alert, bool) {
	if window <= 0 {
		window = DefaultWindow
	}
	if minSeats <= 0 {
		minSeats = DefaultMinSeats
	}
	rs := append([]Record(nil), records...)
	sort.Slice(rs, func(i, j int) bool { return rs[i].At.Before(rs[j].At) })
	var best Alert
	bestN := 0
	for i := range rs {
		from := rs[i].At
		to := from.Add(window)
		seats := map[string]Record{}
		for j := i; j < len(rs) && !rs[j].At.After(to); j++ {
			// Latest record per seat inside the window.
			seats[rs[j].Seat] = rs[j]
		}
		if len(seats) < minSeats || len(seats) <= bestN {
			continue
		}
		last := from
		for _, r := range seats {
			if r.At.After(last) {
				last = r.At
			}
		}
		// A daemon boot within one window either side of the burst's start is
		// a restart story: seats found dead by the first sweep after a boot,
		// or stopped just before one, are the restart path's to explain.
		if !bootAt.IsZero() && !bootAt.Before(from.Add(-window)) && !bootAt.After(from.Add(window)) {
			continue
		}
		names := make([]string, 0, len(seats))
		for n := range seats {
			names = append(names, n)
		}
		sort.Strings(names)
		bestN = len(seats)
		best = Alert{Seats: names, From: from, To: last, Window: window, Shared: sharedReason(seats)}
	}
	return best, bestN > 0
}

func sharedReason(seats map[string]Record) string {
	unknown := 0
	reasons := map[string]int{}
	for _, r := range seats {
		if r.Reason == Unknown || strings.HasPrefix(r.Reason, "unknown") {
			unknown++
			continue
		}
		reasons[r.Reason]++
	}
	if len(reasons) == 0 {
		return fmt.Sprintf("unknown: %d seats, no reason recorded", len(seats))
	}
	if len(reasons) == 1 && unknown == 0 {
		for r := range reasons {
			return r
		}
	}
	keys := make([]string, 0, len(reasons))
	for r := range reasons {
		keys = append(keys, r)
	}
	sort.Slice(keys, func(i, j int) bool {
		if reasons[keys[i]] != reasons[keys[j]] {
			return reasons[keys[i]] > reasons[keys[j]]
		}
		return keys[i] < keys[j]
	})
	parts := make([]string, 0, len(keys)+1)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s ×%d", k, reasons[k]))
	}
	if unknown > 0 {
		parts = append(parts, fmt.Sprintf("unknown ×%d", unknown))
	}
	return "mixed: " + strings.Join(parts, "; ")
}

// FormatAlert is the one line agent_list, the RHS and the overseer see.
func FormatAlert(a Alert) string {
	if len(a.Seats) == 0 {
		return ""
	}
	return fmt.Sprintf("MASS STOP (🎯T662): %d seats stopped within %s (%s–%s) with no daemon restart in the window — %s. Seats: %s.",
		len(a.Seats), a.Window, a.From.Local().Format("15:04:05"), a.To.Local().Format("15:04:05"),
		a.Shared, strings.Join(a.Seats, ", "))
}

// Key identifies a burst for once-only delivery: the burst's first stop.
func (a Alert) Key() string {
	return a.From.UTC().Format(time.RFC3339Nano)
}
