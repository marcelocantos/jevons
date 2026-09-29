// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// 🎯T902: an agent's mid-turn answer to a steered question reaches the asker
// when it is given, not batched to the end of the agent's turn.
//
// T899.1 (2026-09-29) showed escalation working — jevons-po answered the
// overseer's steered status request about a second after absorbing it — but
// the answer only reached the overseer with jevons-po's turn-end report,
// four minutes later. escalateIfBusy already knows who is waiting and when
// the steer landed; this file gives the event sink a place to hand that
// sender the first text the agent produces afterwards, instead of letting it
// fall into the ordinary end-of-turn accumulation alone.

// midTurnAskState is the pending-answer slot for one busy agent. sender=""
// means nothing is pending. Guarded by its own mutex so the event sink
// (agentEventSink, one goroutine per agent) and escalateIfBusy (any sender's
// goroutine) never block each other's unrelated agents.
type midTurnAskState struct {
	mu     sync.Mutex
	sender string
	buf    strings.Builder
}

// midTurnAskState returns the (lazily created) pending-answer slot for name.
func (s *Server) midTurnAskState(name string) *midTurnAskState {
	s.midTurnAsksMu.Lock()
	defer s.midTurnAsksMu.Unlock()
	if s.midTurnAsks == nil {
		s.midTurnAsks = make(map[string]*midTurnAskState)
	}
	st, ok := s.midTurnAsks[name]
	if !ok {
		st = &midTurnAskState{}
		s.midTurnAsks[name] = st
	}
	return st
}

// registerMidTurnAsk records that sender is now waiting on name's very next
// text after a steered message landed mid-turn. A later steer from the same
// or another sender before the first is flushed simply replaces it — only
// one outstanding mid-turn ask is tracked per agent, matching the ladder
// (delivery escalation) which itself only tracks one in-flight urgent send.
func (s *Server) registerMidTurnAsk(name, sender string) {
	if name == "" || sender == "" || sender == name {
		return
	}
	st := s.midTurnAskState(name)
	st.mu.Lock()
	st.sender = sender
	st.buf.Reset()
	st.mu.Unlock()
}

// noteMidTurnAskText appends a text chunk to any pending mid-turn ask on
// name. Cheap no-op when nothing is pending.
func (s *Server) noteMidTurnAskText(name, text string) {
	if text == "" {
		return
	}
	st := s.midTurnAskState(name)
	st.mu.Lock()
	if st.sender != "" {
		st.buf.WriteString(text)
	}
	st.mu.Unlock()
}

// flushMidTurnAsk delivers whatever text has accumulated since a pending ask
// was registered on name, to that ask's sender, and clears the pending
// state. A no-op when nothing is pending or nothing has been said yet (the
// answer has not started; the caller — a tool_use pause or the turn's
// terminal stop — will not be the only chance to flush, since a stop also
// calls this).
func (s *Server) flushMidTurnAsk(name string) {
	st := s.midTurnAskState(name)
	st.mu.Lock()
	sender := st.sender
	text := strings.TrimSpace(st.buf.String())
	st.sender = ""
	st.buf.Reset()
	st.mu.Unlock()
	if sender == "" || text == "" {
		return
	}
	msg := fmt.Sprintf("[Agent %s answered mid-turn]\n%s", name, text)
	if _, err := s.deliverByName(sender, msg, OriginAgent, false); err != nil {
		slog.Error("🎯T902 mid-turn answer relay failed",
			"agent", name, "sender", sender, "err", err)
		return
	}
	slog.Info("🎯T902 mid-turn answer relayed",
		"agent", name, "sender", sender, "len", len(text))
}
