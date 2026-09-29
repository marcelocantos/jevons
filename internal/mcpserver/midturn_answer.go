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
)

// 🎯T902: a busy agent that takes a steered question answers it mid-turn,
// then carries on with its work. That answer used to reach the asker only
// inside the turn-end report, minutes later. Now the answer is captured from
// the seat's absorb of the message to its next tool call, and relayed to the
// asker at once. If the turn simply ends instead, the turn-end report already
// carries the answer and nothing extra is sent.

// midTurnTTL bounds how long a steered question waits to be absorbed.
const midTurnTTL = 30 * time.Minute

type midTurnAsk struct {
	asker string
	text  string
	at    time.Time
}

type midTurnCapture struct {
	asker string
	buf   strings.Builder
}

type midTurnAnswers struct {
	mu        sync.Mutex
	pending   map[string][]midTurnAsk // agent -> steered questions not yet absorbed
	capturing map[string]*midTurnCapture
	now       func() time.Time
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
	if ev.Type == "progress" && ev.ProgressType == claudia.ProgressDeliveryAbsorbed {
		for i, a := range m.pending[agent] {
			if a.text == ev.Text {
				m.pending[agent] = append(m.pending[agent][:i], m.pending[agent][i+1:]...)
				m.capturing[agent] = &midTurnCapture{asker: a.asker}
				break
			}
		}
		return "", "", false
	}
	c := m.capturing[agent]
	if c == nil {
		return "", "", false
	}
	if ev.IsTerminalStop() {
		// The turn-end report carries the answer; do not send it twice.
		delete(m.capturing, agent)
		return "", "", false
	}
	// An assistant message that ends in a tool call carries its text too.
	if ev.Type == "assistant" && ev.Text != "" {
		c.buf.WriteString(ev.Text)
	}
	if !turnCountedTool(ev) {
		return "", "", false
	}
	answer := strings.TrimSpace(c.buf.String())
	if answer == "" {
		return "", "", false // still working toward the answer
	}
	delete(m.capturing, agent)
	return c.asker, answer, true
}

func (s *Server) midTurn() *midTurnAnswers {
	s.midTurnOnce.Do(func() { s.midTurnAnswers = &midTurnAnswers{} })
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
