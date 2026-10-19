// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T952 — A STALLED-BACKLOG NOTICE REACHES THE OVERSEER ONCE PER STALL WITH
// BACKOFF, NOT ONCE PER WARN TICK FOR THE LIFE OF THE STALL.
//
// 2026-09-30 12:06–12:22 AEST: agent_send logged
// `🎯T418 backlog stalled agent=claudia-po queued=1 oldest_age=10m25s → 15m55s`
// every ~66 s, and the overseer answered each with a full turn of the shape
// "Minor time increment … no action needed; continuing to monitor." Eight
// overseer-sized turns in 22 minutes, Claude weekly at 2%, while the stall
// itself was benign (one long turn integrating three worker reports).
//
// 🎯T810 already coalesces materially unchanged fleet-health text by age band.
// That is not enough on its own when the delivery path re-offers an unconfirmed
// first copy every sweep for notifyReplayUnconfirmedGrace (10 m): the WARN tick
// still becomes an overseer turn. This file is the stall-specific gate in front
// of notifyFleetHealth — the sweep may keep logging every tick; the overseer is
// told on first stall, then on exponential backoff, and immediately on a shape
// change (queued count, stall reason, age band). Age seconds keep moving inside
// a band without restarting the cadence.
//
// THE HERMETIC BAR. Twenty simulated minutes of WARN-cadence sweeps against a
// stable stall admit ≤ 4 overseer notices, each carrying the age at the time it
// was admitted.

const (
	// StallNoticeInitialBackoff is how long a stable stall must wait after the
	// first notice before the overseer is told again.
	StallNoticeInitialBackoff = 2 * time.Minute
	// StallNoticeMaxBackoff caps the doubling so a multi-hour stall still
	// re-surfaces on a human timescale.
	StallNoticeMaxBackoff = 30 * time.Minute
)

// StallNoticeShape is the material identity of a stall. Age seconds are not
// part of it — only the T810 age band — so drift inside a band is not news.
type StallNoticeShape struct {
	Agent   string
	Depth   int
	Why     string
	AgeBand int
}

// StallNoticeState is the per-hold backoff memory.
type StallNoticeState struct {
	LastAt   time.Time
	Interval time.Duration // required quiet after LastAt; 0 means never noticed
	Shape    StallNoticeShape
	// Notices counts overseer admissions for this hold (hermetic oracle).
	Notices int
}

// StallNoticeAgeBand maps a waiting age onto the same thresholds T810 uses for
// material change, so a band crossing is one shape change shared by both gates.
func StallNoticeAgeBand(age time.Duration) int {
	band := 0
	for _, b := range noticeAgeBands {
		if age >= b {
			band++
		}
	}
	return band
}

// DecideStallNotice is the pure gate: whether this sweep may call
// notifyFleetHealth for a stalled backlog, and the state after the decision.
func DecideStallNotice(prev StallNoticeState, shape StallNoticeShape, now time.Time) (admit bool, next StallNoticeState) {
	if shape.Agent == "" || shape.Depth <= 0 {
		return false, prev
	}
	// First notice for this hold.
	if prev.LastAt.IsZero() {
		return true, StallNoticeState{
			LastAt:   now,
			Interval: StallNoticeInitialBackoff,
			Shape:    shape,
			Notices:  1,
		}
	}
	if shape != prev.Shape {
		// Queued count, reason, or age band changed — that is news, not a tick.
		return true, StallNoticeState{
			LastAt:   now,
			Interval: StallNoticeInitialBackoff,
			Shape:    shape,
			Notices:  prev.Notices + 1,
		}
	}
	if now.Before(prev.LastAt.Add(prev.Interval)) {
		return false, prev
	}
	nextInterval := prev.Interval * 2
	if nextInterval < StallNoticeInitialBackoff {
		nextInterval = StallNoticeInitialBackoff
	}
	if nextInterval > StallNoticeMaxBackoff {
		nextInterval = StallNoticeMaxBackoff
	}
	return true, StallNoticeState{
		LastAt:   now,
		Interval: nextInterval,
		Shape:    shape,
		Notices:  prev.Notices + 1,
	}
}

// stallNoticeKey is the hold identity reportStalledBacklog already uses for
// T810 coalescing — stable for the life of one hold, new when a new hold forms.
func stallNoticeKey(b sendq.Backlog) string {
	return heldBacklogKey(b)
}

// admitStalledBacklogNotice consults and updates the per-hold backoff memory.
// A false result means this sweep must not call notifyFleetHealth for the stall
// (the WARN log line may still fire).
func (s *Server) admitStalledBacklogNotice(b sendq.Backlog, now time.Time, why string) bool {
	if s == nil {
		return true
	}
	age := b.OldestAge(now)
	shape := StallNoticeShape{
		Agent:   b.Agent,
		Depth:   b.Depth,
		Why:     why,
		AgeBand: StallNoticeAgeBand(age),
	}
	key := stallNoticeKey(b)

	s.mu.Lock()
	if s.stallNotice == nil {
		s.stallNotice = map[string]StallNoticeState{}
	}
	prev := s.stallNotice[key]
	admit, next := DecideStallNotice(prev, shape, now)
	if admit {
		s.stallNotice[key] = next
	}
	s.mu.Unlock()

	if !admit {
		slog.Debug("🎯T952 stalled-backlog notice suppressed by backoff",
			"component", "agent_send",
			"agent", b.Agent,
			"queued", b.Depth,
			"oldest_age", age.Round(time.Second).String(),
			"next_in", prev.LastAt.Add(prev.Interval).Sub(now).Round(time.Second).String(),
			"notices_so_far", prev.Notices)
	}
	return admit
}

// clearStalledBacklogNotice drops backoff memory for a hold that is no longer
// stalled (drained, departed, or reaped-routed). The next stall under a new
// hold key starts fresh; clearing avoids a map that grows with every hold id.
func (s *Server) clearStalledBacklogNotice(key string) {
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	delete(s.stallNotice, key)
	s.mu.Unlock()
}

// StallNoticeCount is the admission count for a hold key (test seam).
func (s *Server) StallNoticeCount(key string) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stallNotice[key].Notices
}

// FormatStallNoticeBackoff is a debug line naming the gate; empty when idle.
func FormatStallNoticeBackoff(st StallNoticeState, now time.Time) string {
	if st.LastAt.IsZero() {
		return ""
	}
	return fmt.Sprintf("stall-notice agent=%s depth=%d band=%d notices=%d next_in=%s",
		st.Shape.Agent, st.Shape.Depth, st.Shape.AgeBand, st.Notices,
		st.LastAt.Add(st.Interval).Sub(now).Round(time.Second))
}
