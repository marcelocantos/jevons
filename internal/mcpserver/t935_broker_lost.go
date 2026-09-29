// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/fleet"
)

// 🎯T935 — a seat the broker took down comes back, or somebody is told.
//
// 🎯T925 relaunches a seat lost to a broker restart on its own conversation.
// On 2026-09-30 jv-t928-mcp-attach, a sidecar seat with no spool records, had
// no conversation to relaunch on: every attempt was refused the same way for
// twelve minutes, the seat sat stopped, and nothing said so until the owner
// looked and killed and reminted it by hand. Two changes close that:
//
//   - fleet.LaunchRecovering mints a fresh session when the refusal says the
//     history is absent (fleet.HistoryAbsent). The seat comes back, and its
//     parent is told it remembers nothing, so the brief is re-sent.
//   - a seat still down fleet.brokerLostFlagAfter after its first failed
//     relaunch is reported to its parent and the overseer once, with the
//     refusal, instead of retrying in silence.

// FormatBrokerLostFresh is the notice for a seat relaunched on a fresh
// session after a broker restart.
func FormatBrokerLostFresh(l fleet.LostSession) string {
	return fmt.Sprintf("Seat %s went down with the Claudia broker and is running again (🎯T935). %s "+
		"If jevons_agent_send refuses the re-brief on recent workdir activity, pass force_rebrief=true: "+
		"that activity is the previous session's.", l.Name, l.Describe())
}

// FormatBrokerLostStuck is the notice for a seat lost to a broker restart
// that is still down.
func FormatBrokerLostStuck(st fleet.BrokerLostStuck, now time.Time) string {
	return fmt.Sprintf("Seat %s went down with the Claudia broker and has not come back: %d relaunch attempts over %s "+
		"all failed (🎯T935). Last error: %s. The daemon keeps retrying every few minutes. "+
		"To recover now: jevons_agent_kill name=%s force=true, then jevons_agent_start name=%s with its brief.",
		st.Name, st.Attempts, now.Sub(st.Since).Round(time.Second), strings.TrimSpace(st.Err), st.Name, st.Name)
}

// noteBrokerLostOutcome delivers the reattach loop's fresh and stuck reports.
func (s *Server) noteBrokerLostOutcome(res fleet.ReattachResult, now time.Time) {
	for _, l := range res.Fresh {
		s.noticeBrokerLost(l.Parent, "t935-fresh:"+l.Name+":"+l.NewSession, FormatBrokerLostFresh(l))
	}
	for _, st := range res.Stuck {
		s.noticeBrokerLost(st.Parent, "t935-stuck:"+st.Name+":"+st.Since.UTC().Format(time.RFC3339), FormatBrokerLostStuck(st, now))
	}
}

// noticeBrokerLost tells the seat's parent, and the overseer through the
// fleet health channel. A seat whose parent is the overseer is told once.
func (s *Server) noticeBrokerLost(parent, occurrence, text string) {
	if s == nil {
		return
	}
	s.logLifecycle(compAgentLifecycle, "broker_lost_notice", "ok", map[string]any{
		"parent": parent, "occurrence": occurrence,
	})
	parent = strings.TrimSpace(parent)
	if parent != "" && parent != s.overseerName() {
		if _, err := s.deliverByName(parent, "[Fleet health] "+text, OriginAgent, false); err != nil {
			slog.Warn("broker-lost notice to parent undelivered", "parent", parent, "occurrence", occurrence, "err", err)
		}
	}
	s.notifyFleetHealth(occurrence, text)
}
