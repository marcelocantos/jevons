// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/roles"
)

// AgentStatusAwaitingParentDirection is the jevons_agent_list status for a
// child whose blocked/needs-direction finish-report woke a PO turn that
// ended without a jevons_agent_send back to that child (🎯T1233).
const AgentStatusAwaitingParentDirection = "awaiting_parent_direction"

const undeliveredDirectionPrefix = "[daemon 🎯T1233] "

type blockedChildAwait struct {
	ReportID    string
	DeliveredAt time.Time
	WokeTurn    bool
	Flagged     bool
}

func formatUndeliveredDirectionFlag(child string) string {
	return undeliveredDirectionPrefix + "your direction to " + child + " was not delivered"
}

func (s *Server) parentIsPOSeat(parent string) bool {
	parent = strings.TrimSpace(parent)
	if s == nil || parent == "" || s.registry == nil {
		return false
	}
	d := s.registry.Def(parent)
	if d == nil {
		return false
	}
	return roles.OfSeat(s.seatRoleOf(*d), "") == roles.ProductOwner
}

// notePOTurnBegan records that a product-owner turn started (🎯T1233).
// The per-turn send log is reset here so only sends in THIS turn count.
func (s *Server) notePOTurnBegan(name string) {
	name = strings.TrimSpace(name)
	if s == nil || name == "" || !s.parentIsPOSeat(name) {
		return
	}
	now := s.deliveryNow()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poTurnStartedAt == nil {
		s.poTurnStartedAt = map[string]time.Time{}
	}
	s.poTurnStartedAt[name] = now
	if s.poTurnSends != nil {
		delete(s.poTurnSends, name)
	}
}

// notePOSendToChild records a jevons_agent_send from actor to dest on the
// per-turn send log (🎯T1233). The MCP call is the observation; delivery
// fate is not re-classified from prose.
func (s *Server) notePOSendToChild(actor, dest string) {
	actor = strings.TrimSpace(actor)
	dest = strings.TrimSpace(dest)
	if s == nil || actor == "" || dest == "" || actor == dest {
		return
	}
	s.mu.Lock()
	if s.poTurnSends == nil {
		s.poTurnSends = map[string]map[string]struct{}{}
	}
	if s.poTurnSends[actor] == nil {
		s.poTurnSends[actor] = map[string]struct{}{}
	}
	s.poTurnSends[actor][dest] = struct{}{}
	s.mu.Unlock()
	s.clearBlockedChildAwait(actor, dest)
}

func deliveryStartedTurn(status string) bool {
	switch strings.TrimSpace(status) {
	case "sent", "delivered_unconfirmed":
		return true
	default:
		return false
	}
}

func finishReportNeedsDirection(text string) bool {
	m, _ := envelope.Parse(text)
	if m == nil || m.Kind != envelope.KindFinishReport {
		return false
	}
	return m.Status == envelope.ProgressBlocked || m.IsBlocked()
}

// noteBlockedChildAwaitingDirection records that a blocked/needs-direction
// finish-report from child was delivered to its product-owner parent.
func (s *Server) noteBlockedChildAwaitingDirection(child, parent, status string) {
	child = strings.TrimSpace(child)
	parent = strings.TrimSpace(parent)
	if s == nil || child == "" || parent == "" || !s.parentIsPOSeat(parent) {
		return
	}
	rec, err := agentreport.Latest(s.agentReportStateDir(), child)
	if err != nil || strings.TrimSpace(rec.Text) == "" {
		return
	}
	if !finishReportNeedsDirection(rec.Text) {
		return
	}
	now := s.deliveryNow()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poBlockedChild == nil {
		s.poBlockedChild = map[string]map[string]blockedChildAwait{}
	}
	if s.poBlockedChild[parent] == nil {
		s.poBlockedChild[parent] = map[string]blockedChildAwait{}
	}
	s.poBlockedChild[parent][child] = blockedChildAwait{
		ReportID:    rec.ID,
		DeliveredAt: now,
		WokeTurn:    deliveryStartedTurn(status),
	}
}

func (s *Server) markUndeliveredDirectionFlagged(po, child string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if await, ok := s.poBlockedChild[po][child]; ok {
		await.Flagged = true
		s.poBlockedChild[po][child] = await
	}
}

func (s *Server) clearBlockedChildAwait(po, child string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poBlockedChild == nil || s.poBlockedChild[po] == nil {
		return
	}
	delete(s.poBlockedChild[po], child)
	if len(s.poBlockedChild[po]) == 0 {
		delete(s.poBlockedChild, po)
	}
}

// undeliveredDirectionChildren returns children whose blocked report woke
// this PO turn and who received no jevons_agent_send during it.
func (s *Server) undeliveredDirectionChildren(po string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	started := s.poTurnStartedAt[po]
	sends := s.poTurnSends[po]
	awaits := s.poBlockedChild[po]
	var out []string
	for child, await := range awaits {
		if await.Flagged {
			continue
		}
		if _, sent := sends[child]; sent {
			continue
		}
		// This turn was woken by the report (delivery started it) or the
		// report was already waiting when the turn began. A report that
		// arrived mid-turn (queued behind an in-flight turn) waits for the
		// next turn.
		if await.WokeTurn || !started.IsZero() && !await.DeliveredAt.After(started) {
			out = append(out, child)
		}
	}
	return out
}

// notePOTurnEndedForUndeliveredDirection flags the PO once in-band when a
// turn woken by a blocked child report ends without a send to that child.
func (s *Server) notePOTurnEndedForUndeliveredDirection(name string) {
	name = strings.TrimSpace(name)
	if s == nil || name == "" || !s.parentIsPOSeat(name) {
		return
	}
	for _, child := range s.undeliveredDirectionChildren(name) {
		msg := formatUndeliveredDirectionFlag(child)
		if _, err := s.deliverByName(name, msg, OriginAgent, false); err != nil {
			slog.Warn("T1233 undelivered-direction flag failed",
				"po", name, "child", child, "err", err)
			continue
		}
		s.markUndeliveredDirectionFlagged(name, child)
		s.logLifecycle(compAgentLifecycle, "undelivered_direction", "flagged", map[string]any{
			"po": name, "child": child,
		})
	}
}

// AwaitingParentDirection reports whether name is a child whose parent
// direction was flagged undelivered (🎯T1233).
func (s *Server) AwaitingParentDirection(name string) (line string, ok bool) {
	name = strings.TrimSpace(name)
	if s == nil || name == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, awaits := range s.poBlockedChild {
		if await, hit := awaits[name]; hit && await.Flagged {
			return "awaiting parent direction", true
		}
	}
	return "", false
}
