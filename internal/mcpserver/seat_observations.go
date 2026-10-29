// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/seatactivity"
	"github.com/marcelocantos/jevons/internal/seatprovider"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/spool"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// Observation feeds run independently of the cheap control reads.
func (s *Server) observeRegistryLiveness() {
	if s == nil || s.registry == nil {
		return
	}
	s.Seats().ObserveRegistry(s.registry)
	s.mu.Lock()
	fn := s.seatAliveFn
	s.mu.Unlock()
	if fn != nil {
		for _, d := range s.registry.List() {
			s.Seats().Observe(seatstate.Observation{Name: d.Name, Alive: seatstate.TriOf(fn(d.Name)), QueueDepth: seatstate.QueueUnknown, Source: "injected.observer"})
		}
	}
}

func (s *Server) observeSeatTranscripts(observers ...func(claudia.AgentDef)) {
	if s == nil || s.registry == nil {
		return
	}
	// One roots reading per pass (🎯T1084): each one checks the temp dir for
	// exclusive Grok homes, and a pass asked several times per seat. A pass
	// already reads one registry snapshot; it reads one roots snapshot too.
	roots := s.transcriptRoots()
	for _, d := range s.registry.List() {
		s.observeSeatTranscriptWith(d, roots)
		for _, observe := range observers {
			observe(d)
		}
	}
	s.observeInertSeats(time.Now())
}

func (s *Server) diagnoseBirth(d claudia.AgentDef, now time.Time) birthDiagnosis {
	return s.diagnoseBirthWith(d, now, s.transcriptRoots())
}

func (s *Server) diagnoseBirthWith(d claudia.AgentDef, now time.Time, roots discovery.Roots) birthDiagnosis {
	var out birthDiagnosis
	sid := strings.TrimSpace(d.SessionID)
	if sid == "" || strings.TrimSpace(d.Name) == "" {
		return out
	}
	l := s.births()
	l.mu.Lock()
	rec, ok := l.births[birthKey(d.Name, sid)]
	l.mu.Unlock()
	if !ok {
		return out
	}
	out.Accepted = true
	out.Birth = rec
	if now.IsZero() {
		now = s.birthClock()
	}
	if !rec.AcceptedAt.IsZero() && !now.Before(rec.AcceptedAt) {
		out.Elapsed = now.Sub(rec.AcceptedAt)
	}
	out.PastGrace = out.Elapsed >= BornStuckGrace
	out.Existence = LookupTranscriptExistence(TranscriptExistenceQuery{
		Name: d.Name, Provider: d.Provider, SessionID: sid, WorkDir: d.WorkDir,
		Roots: roots,
	})
	switch out.Existence.Verdict {
	case ExistencePresent:
		s.observeBornStuck(d.Name, out)
		return out
	case ExistenceUnobservable:
		out.Unknown = true
		s.observeBornStuck(d.Name, out)
		return out
	case ExistenceAbsent:
		if out.PastGrace {
			out.Stuck = true
		}
	}
	s.observeBornStuck(d.Name, out)
	return out
}

// observeBornStuck folds a real born-stuck diagnosis into the shared
// authority (🎯T766.2, census derivation 7). Stuck is Yes only when
// diagnoseBirth already required accepted prompt + located-absent
// transcript + past grace. Present transcript is a positive No. Unknown
// (no birth, unobservable lookup, still inside grace) writes nothing.
func (s *Server) observeBornStuck(name string, diag birthDiagnosis) {
	if s == nil || strings.TrimSpace(name) == "" {
		return
	}
	if diag.Unknown {
		return
	}
	var stuck seatstate.Tri
	switch {
	case diag.Stuck:
		stuck = seatstate.Yes
	case diag.Accepted && diag.Existence.Verdict == ExistencePresent:
		stuck = seatstate.No
	case !diag.Accepted:
		stuck = seatstate.No
	case diag.Accepted && !diag.PastGrace:
		return
	default:
		return
	}
	s.Seats().Observe(seatstate.Observation{
		Name: name, ForSession: diag.Birth.SessionID, BornStuck: stuck,
		QueueDepth: seatstate.QueueUnknown,
		Source:     "born-stuck.claim", At: time.Now(),
	})
}

func (s *Server) observeQueue(name string) { _, _ = s.sendQueue().Depth(name) }

// SetAuthority connects the provider event tracker feed, preserving historical
// activity times. It is configured at construction, before events are consumed.
func (t *IdleActivityTracker) SetAuthority(a *seatstate.Authority) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seats = a
	for name, act := range t.by {
		t.observeActivity(name, act)
	}
}
func (t *IdleActivityTracker) observeActivity(name string, act IdleActivity) {
	if t.seats == nil {
		return
	}
	phase := turnev.PhaseUnknown
	if act.Phase == "working" {
		phase = turnev.PhaseWorking
	}
	if act.Phase == "idle" {
		phase = turnev.PhaseIdle
	}
	t.seats.Observe(seatstate.Observation{Name: name, Phase: phase, LastActivity: act.Updated, QueueDepth: seatstate.QueueUnknown, Source: "provider.activity"})
}

func (s *Server) observeSeatTranscript(d claudia.AgentDef) {
	s.observeSeatTranscriptWith(d, s.transcriptRoots())
}

func (s *Server) observeSeatTranscriptWith(d claudia.AgentDef, roots discovery.Roots) {
	at := time.Now()
	activity := seatactivity.Lookup(seatactivity.Query{Name: d.Name, Provider: d.Provider, SessionID: d.SessionID, WorkDir: d.WorkDir, Roots: roots, Now: at})
	if activity.Verdict == seatactivity.VerdictKnown {
		s.Seats().Observe(seatstate.Observation{Name: d.Name, ForSession: d.SessionID, LastActivity: activity.LastMove, QueueDepth: seatstate.QueueUnknown, Source: "transcript.activity", At: at})
	}
	phase := classifyAgentSessionPhase(d, roots)
	s.Seats().Observe(seatstate.Observation{Name: d.Name, ForSession: d.SessionID, Phase: phase, QueueDepth: seatstate.QueueUnknown, Source: "transcript.fold", At: at})
	st := s.seatState(d.Name)
	if st.Alive.Known() {
		ev := ReadSessionEvidence(d.Provider, d.SessionID, d.WorkDir)
		if seatprovider.Resolve(d.Provider).Sidecar && spool.SeatHasHistory(spool.Dir(), d.Name) {
			ev = SessionEvidencePresent
		}
		// The activity lookup above just found this session's records, so
		// they are present even when ReadSessionEvidence cannot locate the
		// provider's own store (a Cursor sidecar seat read phase_unknown for
		// its whole life while running and working, 2026-10-22).
		if activity.Verdict == seatactivity.VerdictKnown && ev == SessionEvidenceUnknown {
			ev = SessionEvidencePresent
		}
		status := classifySeatListPhase(d.Purpose, st.Alive == seatstate.Yes, s.agentHasTurnBegan(d.Name), d.Materialized, ev)
		s.Seats().Observe(seatstate.Observation{Name: d.Name, ForSession: d.SessionID, Status: status, QueueDepth: seatstate.QueueUnknown, Source: "transcript.lifecycle", At: at})
	}
	s.diagnoseBirthWith(d, s.birthClock(), roots)
	_, _ = s.sendQueue().Depth(d.Name)
	// 🎯T1186: sidecar turn_end in the dated spool is the turn boundary when
	// the wired sink never sees IsTerminalStop.
	s.observeSidecarSpoolTurnEnd(d)
}

func (s *Server) observeBrokerSeats(broker map[string]claudia.BrokerSeat) map[string]seatstate.State {
	if broker == nil {
		return nil
	}
	out := make(map[string]seatstate.State, len(broker))
	for name, raw := range broker {
		s.Seats().Observe(seatstate.Observation{Name: name, BrokerAlive: seatstate.TriOf(raw.Alive), BrokerOwned: seatstate.TriOf(raw.Owned), QueueDepth: seatstate.QueueUnknown, Source: "broker.seats"})
		out[name] = s.seatState(name)
	}
	// 🎯T1157: a successful listing is the broker's whole seat set, so a
	// registered seat missing from it is an observation — the broker does not
	// run it — not a failure to look. Without this its aliveness decays to
	// Unknown and the row reads phase_unknown forever. A live local process
	// still wins the fold, so a seat this daemon holds is not marked dead.
	for _, name := range s.allRegisteredSeatNames() {
		if _, listed := broker[name]; listed {
			continue
		}
		s.Seats().Observe(seatstate.Observation{Name: name, BrokerAlive: seatstate.No, BrokerOwned: seatstate.No, QueueDepth: seatstate.QueueUnknown, Source: "broker.seats.absent"})
	}
	return out
}

