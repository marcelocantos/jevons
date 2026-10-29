// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package seatinert classifies a live seat that accepts queued sends but
// produces no transcript movement across successive observations (🎯T754).
package seatinert

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// QueueUnknown means the observer did not report queue depth.
const QueueUnknown = -1

// MinObservationGap is the minimum time between two samples that count as
// separate observation windows (reconcile and seat-observation ticks are 30s).
const MinObservationGap = 20 * time.Second

// RequiredClimbingWindows is how many consecutive climbing samples are
// required before a seat is called inert.
const RequiredClimbingWindows = 2

// Input is one observation of a seat's queue and transcript recency.
type Input struct {
	Now time.Time
	// TranscriptAgeSec is seconds since the transcript last moved; negative
	// when unknown.
	TranscriptAgeSec float64
	// QueueDepth is waiting sends in the daemon queue; QueueUnknown means no
	// claim. Inert requires a positive depth — a frozen seat that still
	// accepts sends accumulates here.
	QueueDepth int
	Alive      bool
	BornStuck  bool
	// ProviderMotionAt is the last ACP/provider motion the daemon saw for
	// this seat (tool progress, assistant chunks, …). Zero means none yet.
	// A long tool call moves the provider without advancing the transcript
	// mtime; that motion excludes inert.
	ProviderMotionAt time.Time
}

// Seat tracks consecutive climbing transcript-age samples for one name.
type Seat struct {
	Prev      Sample
	HasPrev   bool
	Climbing  int
	Inert     bool
	QueuedAt  time.Time // when queue depth first went positive this episode
	QueuePeak int
}

// Sample is one transcript-age reading at an observation time.
type Sample struct {
	At               time.Time
	TranscriptAgeSec float64
}

// Registry holds per-seat inert classifiers.
type Registry struct {
	mu    sync.Mutex
	seats map[string]*Seat
}

// NewRegistry returns an empty classifier registry.
func NewRegistry() *Registry {
	return &Registry{seats: map[string]*Seat{}}
}

// Observe feeds one sample and returns whether the seat is inert and whether
// it newly became inert this call.
func (r *Registry) Observe(name string, in Input) (inert bool, fresh bool) {
	name = strings.TrimSpace(name)
	if r == nil || name == "" {
		return false, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.seats[name]
	if st == nil {
		st = &Seat{}
		r.seats[name] = st
	}
	prevInert := st.Inert
	r.observeSeat(st, in)
	return st.Inert, st.Inert && !prevInert
}

// Forget drops bookkeeping for a removed or reminted seat.
func (r *Registry) Forget(name string) {
	if r == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	r.mu.Lock()
	delete(r.seats, name)
	r.mu.Unlock()
}

// Get returns the current classification for name.
func (r *Registry) Get(name string) (Seat, bool) {
	if r == nil {
		return Seat{}, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Seat{}, false
	}
	r.mu.Lock()
	st, ok := r.seats[name]
	if !ok {
		r.mu.Unlock()
		return Seat{}, false
	}
	out := *st
	r.mu.Unlock()
	return out, true
}

func (r *Registry) observeSeat(st *Seat, in Input) {
	if st == nil {
		return
	}
	if !in.Alive || in.BornStuck || in.TranscriptAgeSec < 0 || in.QueueDepth <= 0 {
		st.resetEpisode()
		return
	}
	if workingToolCall(in) {
		st.resetEpisode()
		return
	}
	if in.QueueDepth > st.QueuePeak {
		st.QueuePeak = in.QueueDepth
	}
	if st.QueuedAt.IsZero() {
		st.QueuedAt = in.Now
	}

	age := in.TranscriptAgeSec
	if st.HasPrev && in.Now.Sub(st.Prev.At) >= MinObservationGap {
		if age > st.Prev.TranscriptAgeSec+0.25 {
			st.Climbing++
		} else {
			st.Climbing = 0
		}
		st.Prev = Sample{At: in.Now, TranscriptAgeSec: age}
	} else if !st.HasPrev {
		st.Prev = Sample{At: in.Now, TranscriptAgeSec: age}
		st.HasPrev = true
	}

	st.Inert = st.Climbing >= RequiredClimbingWindows
}

func (st *Seat) resetEpisode() {
	st.HasPrev = false
	st.Climbing = 0
	st.Inert = false
	st.QueuedAt = time.Time{}
	st.QueuePeak = 0
}

// workingToolCall reports whether provider motion outran the stale transcript
// clock — a seat inside a long tool call, not frozen.
func workingToolCall(in Input) bool {
	if in.ProviderMotionAt.IsZero() || in.TranscriptAgeSec < 0 {
		return false
	}
	transcriptAt := in.Now.Add(-time.Duration(in.TranscriptAgeSec * float64(time.Second)))
	return in.ProviderMotionAt.After(transcriptAt)
}

// FormatLine is the operator-facing inert account for agent_list and /api/agents.
func FormatLine(name string, st Seat, now time.Time) string {
	wait := ""
	if !st.QueuedAt.IsZero() {
		wait = fmt.Sprintf("; %d prompt(s) waiting up to %s",
			st.QueuePeak, now.Sub(st.QueuedAt).Round(time.Second))
	}
	return fmt.Sprintf("INERT %s: transcript_age_seconds has only climbed across %d observation windows with queued sends accepted and no provider output%s (🎯T754).",
		name, RequiredClimbingWindows, wait)
}
