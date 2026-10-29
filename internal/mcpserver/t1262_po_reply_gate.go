// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T1262: a product owner's routine reply does not wake the overseer.
//
// 🎯T1229 stopped copies of worker reports reaching the overseer when the
// worker's parent PO holds them. The PO's own turn replies still did: every
// "Acknowledged, no action needed" became an "agent-responded" prompt to the
// overseer. On 2026-10-28 that was 73 of the overseer's 121 prompts in an
// hour, each re-reading a ~320k-token context, about 70M cache-read tokens
// an hour of Claude.
//
// A PO reply now reaches the overseer when it answers something the
// overseer sent that PO, when it opens with a typed envelope other than an
// ack (an escalation, an owner-reply, a finish-report, ...), or when it
// carries a false-green flag. The kind decides, never the wording. Anything
// else is stored, as every report is, and is not delivered.

// poReplyOwedFor bounds how long an overseer send keeps the PO's next reply
// owed: an answer that takes longer than this is no longer that answer.
const poReplyOwedFor = 2 * time.Hour

type poReplyOwed struct {
	mu sync.Mutex
	at map[string]time.Time
}

// noteOverseerSend records that the overseer sent name something, so name's
// next reply answers it.
func (p *poReplyOwed) noteOverseerSend(name string, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.at == nil {
		p.at = map[string]time.Time{}
	}
	p.at[name] = now
}

// take reports whether name owes the overseer a reply, and clears it.
func (p *poReplyOwed) take(name string, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	at, ok := p.at[name]
	delete(p.at, name)
	return ok && now.Sub(at) <= poReplyOwedFor
}

// typedForOverseer reports whether text opens with an envelope whose kind is
// meant for its reader: anything typed except an ack.
func typedForOverseer(text string) bool {
	msg, err := envelope.Parse(text)
	return err == nil && msg != nil && msg.Kind != "" && msg.Kind != envelope.KindAck
}

// noteSendForPOReply is called after a successful agent_send.
func (s *Server) noteSendForPOReply(actor, name string) {
	if s.isOverseerAgent(actor) && s.parentIsProductOwner(name) {
		s.poReplies.noteOverseerSend(name, time.Now())
	}
}

// poReplyHeld reports whether agentName is a product owner whose reply should
// not wake the overseer. flagged is true when the reply carries a false-green
// banner, which always goes through.
func (s *Server) poReplyHeld(agentName, text string, flagged bool) bool {
	if !s.parentIsProductOwner(agentName) {
		return false
	}
	owed := s.poReplies.take(agentName, time.Now())
	return !owed && !flagged && !typedForOverseer(text)
}
