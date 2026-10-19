// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/seatactivity"
	"github.com/marcelocantos/jevons/internal/seatinert"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T754 — a seat that accepts sends while executing nothing is detected,
// not read as busy.
//
// THE SPECIMEN. 2026-09-21 05:34–06:12: jevons-po was rescued onto grok after
// two born-stuck claude mints. Sends at 05:57 and 06:12 returned
// "queued, prompt in flight", which every available signal read as mid-turn.
// The transcript had not moved since 05:35. Refill orders produced zero seats.
// Forty minutes of product work were lost while the PO looked fine.
//
// 🎯T751 / 🎯T751.1 settle alive-vs-dead and born-stuck. Neither settles
// working-vs-inert, because a frozen seat queues exactly like a busy one.
// The decidable signal is the conjunction over two observations: queued
// prompts held, transcript_age only climbing, past seatinert.SilenceFloor.
//
// Detection and reporting only. Remint / reprovider / wait stay with the
// overseer (🎯T751.1).

// AgentStatusInert is the agent_list phase for a live seat that holds queued
// prompts while its transcript age only climbs (🎯T754).
const AgentStatusInert = "inert"

// inertObservation is one sample kept so the next list/sweep can classify.
type inertObservation struct {
	Sample seatinert.Sample
	At     time.Time
}

// inertState is the per-seat bookkeeping behind InertSeat.
type inertState struct {
	mu      sync.Mutex
	prev    map[string]inertObservation
	inert   map[string]InertSeatInfo
	noticed map[string]time.Time // name → when parent was told
}

// InertSeatInfo is what the daemon knows about one inert seat.
type InertSeatInfo struct {
	Since    time.Time
	Age      time.Duration
	Queued   int
	InFlight bool
	LastMove time.Time
}

func (s *Server) inertBook() *inertState {
	if s == nil {
		return &inertState{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inertSeats == nil {
		s.inertSeats = &inertState{
			prev:    map[string]inertObservation{},
			inert:   map[string]InertSeatInfo{},
			noticed: map[string]time.Time{},
		}
	}
	return s.inertSeats
}

// sampleInertSeat builds one observation from the live authority + activity
// meter + sendq. Hermetic tests inject roots and clock via the birth seams.
func (s *Server) sampleInertSeat(d claudia.AgentDef, now time.Time) seatinert.Sample {
	out := seatinert.Sample{}
	if s == nil {
		return out
	}
	st, _ := s.Seats().Get(d.Name)
	out.InFlight = st.InFlight == seatstate.Yes
	if st.QueueDepth > 0 {
		out.Queued = st.QueueDepth
	} else if n := s.pendingAgentSends(d.Name); n > 0 {
		out.Queued = n
	}

	// Prefer the authority's last-activity when present (fed by decorate /
	// birth / turn sinks); otherwise one stat via seatactivity.
	if !st.LastActivity.IsZero() && (st.SessionID == "" || st.SessionID == d.SessionID) {
		out.AgeKnown = true
		out.LastMove = st.LastActivity
		out.Age = now.Sub(st.LastActivity)
		if out.Age < 0 {
			out.Age = 0
		}
		return out
	}

	q := seatactivity.Query{
		Name:      d.Name,
		Provider:  d.Provider,
		SessionID: d.SessionID,
		WorkDir:   d.WorkDir,
		Roots:     s.transcriptRoots(),
		Now:       now,
	}
	r := seatactivity.Lookup(q)
	if r.Verdict != seatactivity.VerdictKnown {
		return out
	}
	out.AgeKnown = true
	out.LastMove = r.LastMove
	out.Age = r.Age
	return out
}

// observeInertSeat folds one sample into the book and returns the current
// verdict. A Working/Unknown verdict clears a prior inert mark so a seat that
// starts moving is not stuck on the panel.
func (s *Server) observeInertSeat(d claudia.AgentDef, now time.Time) seatinert.Verdict {
	if s == nil || strings.TrimSpace(d.Name) == "" {
		return seatinert.Working
	}
	if !s.seatAlive(d.Name) {
		s.clearInert(d.Name)
		return seatinert.Working
	}
	// Born-stuck owns the "no transcript" class; do not double-label.
	if s.bornStuck(d) {
		s.clearInert(d.Name)
		return seatinert.Working
	}

	curr := s.sampleInertSeat(d, now)
	book := s.inertBook()
	book.mu.Lock()
	defer book.mu.Unlock()

	prev, had := book.prev[d.Name]
	book.prev[d.Name] = inertObservation{Sample: curr, At: now}
	if !had {
		return seatinert.Working
	}

	v := seatinert.Classify(prev.Sample, curr)
	switch v {
	case seatinert.Inert:
		info := InertSeatInfo{
			Since:    prev.At,
			Age:      curr.Age,
			Queued:   curr.Queued,
			InFlight: curr.InFlight,
			LastMove: curr.LastMove,
		}
		if existing, ok := book.inert[d.Name]; ok && !existing.Since.IsZero() {
			info.Since = existing.Since
		}
		book.inert[d.Name] = info
	default:
		delete(book.inert, d.Name)
	}
	return v
}

func (s *Server) clearInert(name string) {
	book := s.inertBook()
	book.mu.Lock()
	defer book.mu.Unlock()
	delete(book.inert, name)
	delete(book.prev, name)
	delete(book.noticed, name)
}

// inertSeat reports whether name is currently classified inert.
func (s *Server) inertSeat(name string) (InertSeatInfo, bool) {
	book := s.inertBook()
	book.mu.Lock()
	defer book.mu.Unlock()
	info, ok := book.inert[name]
	return info, ok
}

// FormatInertLine is the operator-facing account of an inert seat, shared by
// agent_list, /api/agents and the parent notice so the three never drift.
func FormatInertLine(name string, info InertSeatInfo, now time.Time) string {
	age := info.Age
	if age <= 0 && !info.LastMove.IsZero() {
		age = now.Sub(info.LastMove)
	}
	flight := "idle"
	if info.InFlight {
		flight = "prompt in flight"
	}
	return fmt.Sprintf(
		"inert: %s holds %d queued prompt(s) (%s) while its transcript has not moved for %s — "+
			"sends still return queued, but nothing is executing (🎯T754). "+
			"Detection only; remint / interrupt / wait is the overseer's call.",
		name, info.Queued, flight, age.Round(time.Second))
}

// InertSeat answers /api/agents: the inert line for name, if currently inert.
func (s *Server) InertSeat(name string) (string, bool) {
	info, ok := s.inertSeat(name)
	if !ok {
		return "", false
	}
	return FormatInertLine(name, info, s.birthClock()), true
}

// notifyInertIfDue tells the parent once when a seat first becomes inert.
func (s *Server) notifyInertIfDue(d claudia.AgentDef, info InertSeatInfo, now time.Time) {
	book := s.inertBook()
	book.mu.Lock()
	if _, told := book.noticed[d.Name]; told {
		book.mu.Unlock()
		return
	}
	book.noticed[d.Name] = now
	book.mu.Unlock()

	parent := strings.TrimSpace(d.Parent)
	if parent == "" {
		parent = s.overseerName()
	}
	if parent == "" || parent == d.Name {
		return
	}
	text := FormatInertLine(d.Name, info, now)
	res, err := s.deliverByName(parent, text, OriginAgent, false)
	if noticeSubmitted(res.Status, err) {
		slog.Info("🎯T754 inert notice delivered",
			"agent", d.Name, "parent", parent, "queued", info.Queued,
			"age", info.Age.Round(time.Second).String())
		return
	}
	// Allow a later sweep to retry.
	book.mu.Lock()
	delete(book.noticed, d.Name)
	book.mu.Unlock()
	slog.Info("🎯T754 inert notice unsubmitted; will retry",
		"agent", d.Name, "parent", parent, "status", res.Status, "err", err)
}

// SweepInert diagnoses live seats for the climbing-age + queued conjunction
// and delivers at most one parent notice per seat while it stays inert.
// Exported so GET /api/agents can sample without waiting on agent_list.
func (s *Server) SweepInert() {
	s.sweepInert()
}

// sweepInert diagnoses live seats for the climbing-age + queued conjunction
// and delivers at most one parent notice per seat while it stays inert.
func (s *Server) sweepInert() {
	if s == nil || s.registry == nil {
		return
	}
	now := s.birthClock()
	for _, d := range s.registry.List() {
		if strings.TrimSpace(d.Name) == "" {
			continue
		}
		v := s.observeInertSeat(d, now)
		if v != seatinert.Inert {
			continue
		}
		info, ok := s.inertSeat(d.Name)
		if !ok {
			continue
		}
		s.notifyInertIfDue(d, info, now)
	}
}

// Ensure discovery.Roots is used when transcriptRoots is empty (tests that
// never set birth roots still compile against the import).
var _ = discovery.Roots{}
