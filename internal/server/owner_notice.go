// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"time"
)

// Deterministic owner notices (🎯T415).
//
// Every existing fleet-health signal is delivered TO THE OVERSEER
// (mcpserver.notifyFleetHealth), which means any failure that stops the
// overseer also stops the report of it. On 2026-08-10 the overseer was
// down for hours: the sentinel detected overseer:down every two minutes
// and logged outcome=ok, and the owner found out by looking at the UI.
//
// The alarm must not depend on the thing it is alarming about. This path
// is daemon code writing to the owner's own journal — no agent, no
// delivery, nothing that can be stuck. It is deliberately the dumbest
// thing that could work, because it is the one that has to work when
// everything cleverer has failed.

// OwnerNotice is one deterministic report to the owner.
type OwnerNotice struct {
	// Subject is what the notice is about, usually an agent name. Repeats
	// of the same subject and kind strengthen the notice rather than
	// filing a new one.
	Subject string
	// Kind groups notices ("convergence-exhausted", …).
	Kind string
	// Text is what the owner reads.
	Text string
}

// NotifyOwner writes a notice straight to the owner's chat journal.
//
// Returns false only when there is no journal to write to, which is a
// misconfigured daemon rather than a runtime condition. It never depends
// on an agent being alive, never waits on delivery, and never fails
// because the fleet is unwell — that is the entire point.
func (s *Server) NotifyOwner(n OwnerNotice) bool {
	if s == nil {
		return false
	}
	text := n.Text
	if text == "" {
		return false
	}
	line, err := json.Marshal(map[string]any{
		"type":      "agent_note",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"text":      text,
		"notice": map[string]string{
			"kind":    n.Kind,
			"subject": n.Subject,
		},
	})
	if err != nil {
		slog.Error("owner notice marshal failed", "kind", n.Kind, "subject", n.Subject, "err", err)
		return false
	}
	s.BroadcastChat(string(line))
	slog.Warn("owner notified", "kind", n.Kind, "subject", n.Subject)
	return true
}

// NotifyOwner satisfies mcpserver.OwnerNotifier. The positional form
// keeps mcpserver free of a dependency on this package's types.
func (s *Server) NotifyOwnerNote(subject, kind, text string) bool {
	return s.NotifyOwner(OwnerNotice{Subject: subject, Kind: kind, Text: text})
}
