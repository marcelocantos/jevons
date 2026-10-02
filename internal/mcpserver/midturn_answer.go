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

	"github.com/marcelocantos/jevons/internal/delivery"
)

// 🎯T902: a busy agent that takes a steered question answers it mid-turn,
// then carries on with its work. That answer used to reach the asker only
// inside the turn-end report, minutes later. Now the answer is captured from
// the seat's absorb of the message, and relayed to the asker as soon as the
// agent moves on: at its next tool call when the provider reports one, or
// once the answer's text has gone quiet while the turn stays open. If the
// turn simply ends instead, the turn-end report carries the answer and
// nothing extra is sent.

// midTurnTTL bounds how long a steered question waits to be absorbed.
const midTurnTTL = 30 * time.Minute

// midTurnQuiet is the pause after which a captured answer is taken as given.
// A seat that runs its own tools (the Oh My Pi sidecar runs Bash itself)
// tells the host nothing between the answer and the end of its next tool, so
// text that stops while the turn stays open is the only boundary it shows.
const midTurnQuiet = 4 * time.Second

type midTurnAsk struct {
	asker string
	text  string
	at    time.Time
}

type midTurnCapture struct {
	asker string
	buf   strings.Builder
	quiet *time.Timer
}

type midTurnAnswers struct {
	mu        sync.Mutex
	pending   map[string][]midTurnAsk // agent -> steered questions not yet absorbed
	capturing map[string]*midTurnCapture
	now       func() time.Time
	// quietAfter overrides midTurnQuiet (tests).
	quietAfter time.Duration
	// relay receives an answer the quiet timer completes.
	relay func(agent, asker, answer string)
}

func (m *midTurnAnswers) init() {
	if m.pending == nil {
		m.pending = map[string][]midTurnAsk{}
		m.capturing = map[string]*midTurnCapture{}
	}
	if m.now == nil {
		m.now = time.Now
	}
}

// expect records that asker's text was steered into agent's busy turn.
func (m *midTurnAnswers) expect(agent, asker, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	now := m.now()
	kept := m.pending[agent][:0]
	for _, a := range m.pending[agent] {
		if now.Sub(a.at) < midTurnTTL {
			kept = append(kept, a)
		}
	}
	m.pending[agent] = append(kept, midTurnAsk{asker: asker, text: text, at: now})
}

// forget drops a question whose send failed.
func (m *midTurnAnswers) forget(agent, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	for i, a := range m.pending[agent] {
		if a.text == text {
			m.pending[agent] = append(m.pending[agent][:i], m.pending[agent][i+1:]...)
			return
		}
	}
}

// observe folds one of agent's events in. It returns (asker, answer) when a
// mid-turn answer is complete and should be relayed now.
func (m *midTurnAnswers) observe(agent string, ev claudia.Event) (string, string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	if ev.Type == "progress" && ev.ProgressType == delivery.ProgressDeliveryAbsorbed {
		matched := false
		for i, a := range m.pending[agent] {
			if a.text == ev.Text {
				m.pending[agent] = append(m.pending[agent][:i], m.pending[agent][i+1:]...)
				m.capturing[agent] = &midTurnCapture{asker: a.asker}
				matched = true
				break
			}
		}
		slog.Info("🎯T902 seat absorbed a message", "agent", agent, "awaited", matched, "len", len(ev.Text))
		return "", "", false
	}
	c := m.capturing[agent]
	if c == nil {
		return "", "", false
	}
	if ev.IsTerminalStop() {
		// The turn-end report carries the answer; do not send it twice.
		m.dropLocked(agent, c)
		return "", "", false
	}
	// An assistant message that ends in a tool call carries its text too.
	if ev.Type == "assistant" && ev.Text != "" {
		c.buf.WriteString(ev.Text)
		m.armQuietLocked(agent, c)
	}
	if !turnCountedTool(ev) {
		return "", "", false
	}
	answer := strings.TrimSpace(c.buf.String())
	if answer == "" {
		return "", "", false // still working toward the answer
	}
	m.dropLocked(agent, c)
	return c.asker, answer, true
}

// armQuietLocked (re)starts c's quiet timer: each new piece of the answer
// pushes the boundary back.
func (m *midTurnAnswers) armQuietLocked(agent string, c *midTurnCapture) {
	if c.quiet != nil {
		c.quiet.Stop()
	}
	after := m.quietAfter
	if after <= 0 {
		after = midTurnQuiet
	}
	c.quiet = time.AfterFunc(after, func() { m.quietElapsed(agent, c) })
}

// quietElapsed relays c's answer if it is still the capture in progress.
func (m *midTurnAnswers) quietElapsed(agent string, c *midTurnCapture) {
	m.mu.Lock()
	if m.capturing[agent] != c {
		m.mu.Unlock()
		return
	}
	answer := strings.TrimSpace(c.buf.String())
	if answer == "" {
		m.mu.Unlock()
		return
	}
	delete(m.capturing, agent)
	relay := m.relay
	m.mu.Unlock()
	if relay != nil {
		relay(agent, c.asker, answer)
	}
}

func (m *midTurnAnswers) dropLocked(agent string, c *midTurnCapture) {
	if c.quiet != nil {
		c.quiet.Stop()
	}
	delete(m.capturing, agent)
}

func (s *Server) midTurn() *midTurnAnswers {
	s.midTurnOnce.Do(func() {
		s.midTurnAnswers = &midTurnAnswers{relay: s.relayMidTurnAnswer}
	})
	return s.midTurnAnswers
}

// relayMidTurnAnswer hands a captured mid-turn answer to its asker.
func (s *Server) relayMidTurnAnswer(agent, asker, answer string) {
	msg := fmt.Sprintf("[Agent %s answered your message mid-turn, and is still working]\n%s", agent, answer)
	res, err := s.deliverByName(asker, msg, OriginAgent, false)
	if err != nil {
		slog.Warn("🎯T902 mid-turn answer relay failed", "agent", agent, "asker", asker, "err", err)
		return
	}
	slog.Info("🎯T902 relayed a mid-turn answer", "agent", agent, "asker", asker, "len", len(answer), "status", res.Status)
}
