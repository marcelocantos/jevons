// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"

	"github.com/marcelocantos/jevons/internal/converge"
)

// PostmortemSink adapts the pure converge.PostmortemSink seam onto the
// unified overseer deliver arm (DeliverToOverseerAs). Closed impatience
// incidents (🎯T319) land owner-visible in main chat through the same path
// workers and fleet notifies use — journalling and the notify queue included.
//
// Origin is agent: this is a root/system report, not owner speech. Agent
// origin injects without an owner bubble or userTurnPrefix (see
// DeliverToOverseerAs), which is the correct framing for a daemon-authored
// mini-postmortem.
//
// Wired from cmd/jevonsd/main.go into ImpatienceEngineArgs.Postmortem; this
// package only exposes the constructor.
type PostmortemSink struct {
	deliver func(text, origin string) error
}

// Compile-time check: this type is the daemon side of the pure seam.
var _ converge.PostmortemSink = (*PostmortemSink)(nil)

// NewPostmortemSink binds sink delivery to s.DeliverToOverseerAs. A nil
// server yields a nil sink so the converge loop can leave the seam unwired
// honestly (PostmortemJournal.Flush treats nil as leave-pending).
func NewPostmortemSink(s *Server) *PostmortemSink {
	if s == nil {
		return nil
	}
	return &PostmortemSink{deliver: s.DeliverToOverseerAs}
}

// DeliverPostmortem implements converge.PostmortemSink.
func (p *PostmortemSink) DeliverPostmortem(text string) error {
	if p == nil || p.deliver == nil {
		return fmt.Errorf("postmortem sink: no overseer arm wired")
	}
	return p.deliver(text, sendOriginAgent)
}
