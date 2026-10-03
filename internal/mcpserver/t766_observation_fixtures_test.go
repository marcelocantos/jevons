// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/turnev"
	"github.com/mark3labs/mcp-go/mcp"
)

// Transport fakes no longer double as the control's truth source. These
// fixtures explicitly feed the provider observation before exercising sends.
func observeSenderFixture(s *Server, name string, proc agentSender) {
	if proc == nil {
		return
	}
	known, ok := proc.(interface{ Alive() bool })
	if !ok {
		panic("sender fixture needs an explicit observation")
	}
	obs := seatstate.Observation{Name: name, Alive: seatstate.TriOf(known.Alive()), QueueDepth: seatstate.QueueUnknown, Source: "test.provider"}
	if p, ok := proc.(interface{ TurnPhase() claudia.TurnPhase }); ok {
		obs.InFlight = seatstate.TriOf(p.TurnPhase() == claudia.TurnInTurn)
	}
	s.Seats().Observe(obs)
}
func deliverObservedToSender(s *Server, name, text string, interrupt bool, proc agentSender, rehydrated bool) (agentSendResult, error) {
	observeSenderFixture(s, name, proc)
	return deliverToSender(s, name, text, interrupt, proc, rehydrated)
}
func deliverObservedToSenderWith(s *Server, name, text string, interrupt bool, proc agentSender, rehydrated bool, confirm sendConfirmation) (agentSendResult, error) {
	observeSenderFixture(s, name, proc)
	return deliverToSenderWith(s, name, text, interrupt, proc, rehydrated, confirm)
}
func deliverObservedToSenderMode(s *Server, name, text string, mode delivery.Mode, proc agentSender, rehydrated bool, confirm sendConfirmation) (agentSendResult, error) {
	observeSenderFixture(s, name, proc)
	return deliverToSenderMode(s, name, text, mode, proc, rehydrated, confirm)
}
func setObservedSenderResolver(s *Server, resolve func(string) (agentSender, bool, error)) {
	s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		proc, rehydrated, err := resolve(name)
		if err == nil {
			observeSenderFixture(s, name, proc)
		}
		return proc, rehydrated, err
	})
}

func observedAgentList(s *Server, ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.observeRegistryLiveness()
	s.observeSeatTranscripts()
	return s.handleAgentList(ctx, req)
}
func observedAgentPhase(s *Server, d claudia.AgentDef, alive bool) string {
	s.Seats().Observe(seatstate.Observation{Name: d.Name, SessionID: d.SessionID, Alive: seatstate.TriOf(alive), QueueDepth: seatstate.QueueUnknown, Source: "test.process"})
	s.observeSeatTranscript(d)
	return s.agentPhase(d, alive)
}
func sweepObservedBirths(s *Server) {
	s.observeRegistryLiveness()
	s.observeSeatTranscripts()
	s.sweepBornStuck()
}
func observedPendingSends(s *Server, name string) int {
	s.observeQueue(name)
	return s.pendingAgentSends(name)
}

func observedEscalateIfBusy(s *Server, name, text, class, asker string, proc agentSender) (agentSendResult, bool, error) {
	observeSenderFixture(s, name, proc)
	return s.escalateIfBusy(name, text, class, asker, proc)
}

func (s *Server) observeSessionPhase(name string, phase turnev.Phase) {
	if s == nil || name == "" || phase == turnev.PhaseUnknown {
		return
	}
	s.Seats().Observe(seatstate.Observation{Name: name, Phase: phase,
		QueueDepth: seatstate.QueueUnknown, Source: "transcript.fold", At: time.Now()})
}

func observedResumeLost(def *claudia.AgentDef) bool {
	if def == nil {
		return false
	}
	a := seatstate.New(seatstate.Args{})
	a.Observe(seatstate.Observation{Name: def.Name, SessionID: def.SessionID, Source: "fixture", QueueDepth: seatstate.QueueUnknown})
	a.ObserveResumeEvidence(def)
	st, _ := a.Get(def.Name)
	return st.ResumeLost == seatstate.Yes
}
