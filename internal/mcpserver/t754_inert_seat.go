// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/seatinert"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// AgentStatusInert is the agent_list phase for a live seat that accepts
// queued sends while its transcript age only climbs (🎯T754).
const AgentStatusInert = "inert"

// 🎯T754 — a seat whose send returns queued / prompt in flight but which
// produces no transcript movement across successive observations is called
// inert, distinct from dead (send refused) and born-stuck (startup_stall).

func (s *Server) ensureInertRegistry() *seatinert.Registry {
	if s == nil {
		return seatinert.NewRegistry()
	}
	s.inertOnce.Do(func() {
		if s.inert == nil {
			s.inert = seatinert.NewRegistry()
		}
	})
	if s.inert == nil {
		s.inert = seatinert.NewRegistry()
	}
	return s.inert
}

func (s *Server) inertClock() time.Time {
	if s != nil {
		return s.sweepClock()
	}
	return time.Now()
}

// observeInertSeats samples transcript age and queue depth for every live
// seat. It runs at the end of the slow transcript observation pass.
func (s *Server) observeInertSeats(now time.Time) {
	if s == nil || s.registry == nil {
		return
	}
	reg := s.ensureInertRegistry()
	for _, d := range s.registry.List() {
		name := strings.TrimSpace(d.Name)
		if name == "" {
			continue
		}
		if !s.seatAlive(name) {
			reg.Forget(name)
			continue
		}
		s.observeQueue(name)
		st := s.seatState(name)
		ageSec := -1.0
		if !st.LastActivity.IsZero() {
			ageSec = now.Sub(st.LastActivity).Seconds()
		}
		depth := st.QueueDepth
		if depth == seatstate.QueueUnknown {
			depth = seatinert.QueueUnknown
		}
		inert, fresh := reg.Observe(name, seatinert.Input{
			Now:              now,
			TranscriptAgeSec: ageSec,
			QueueDepth:       depth,
			Alive:            true,
			BornStuck:        st.BornStuck == seatstate.Yes,
			ProviderMotionAt: s.wedges.lastMotion(name),
		})
		if fresh && inert {
			s.notifyInertSeat(d, reg, now)
		}
	}
}

func (s *Server) notifyInertSeat(d claudia.AgentDef, reg *seatinert.Registry, now time.Time) {
	st, ok := reg.Get(d.Name)
	if !ok || !st.Inert {
		return
	}
	line := seatinert.FormatLine(d.Name, st, now)
	key := "inert:" + strings.TrimSpace(d.Name) + ":" + strings.TrimSpace(d.SessionID)
	s.notifyFleetHealth(noticeAct, key+"@"+now.UTC().Format(time.RFC3339Nano), line)
	parent := strings.TrimSpace(d.Parent)
	if parent == "" {
		parent = s.overseerName()
	}
	if parent == "" || parent == d.Name {
		return
	}
	text := fmt.Sprintf("%s Parent %q holds %d queued prompt(s) for this seat.",
		line, parent, st.QueuePeak)
	res, err := s.deliverByName(parent, text, OriginAgent, false)
	if noticeSubmitted(res.Status, err) {
		return
	}
	if err == nil {
		err = fmt.Errorf("notice status %q is not submitted", res.Status)
	}
	slog.Info("inert notice unsubmitted; fleet-health act path fired",
		"agent", d.Name, "parent", parent, "status", res.Status, "err", err)
}

// sweepInert re-samples seats on the reconcile path (same classifier as the
// transcript observation tick).
func (s *Server) sweepInert() {
	s.observeInertSeats(s.inertClock())
}

// InertSeat answers /api/agents and agent_list for a classified inert seat.
func (s *Server) InertSeat(name string) (string, bool) {
	if s == nil {
		return "", false
	}
	reg := s.ensureInertRegistry()
	st, ok := reg.Get(name)
	if !ok || !st.Inert {
		return "", false
	}
	return seatinert.FormatLine(name, st, s.inertClock()), true
}

// QueuedPromptsForAPI exposes waiting sends when the authority knows depth.
func (s *Server) QueuedPromptsForAPI(name string) (depth int, ageSec float64, ok bool) {
	if s == nil {
		return 0, 0, false
	}
	s.observeQueue(name)
	st := s.seatState(name)
	if st.QueueDepth <= 0 || st.QueueDepth == seatstate.QueueUnknown {
		return 0, 0, false
	}
	depth = st.QueueDepth
	if !st.QueueOldest.IsZero() {
		ageSec = s.inertClock().Sub(st.QueueOldest).Seconds()
	}
	return depth, ageSec, true
}
