// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/planusage"
)

// handoverLedger is the extra migrator surface the 🎯T418 sweep needs.
// *fleet.Claudia implements it; test doubles may not.
type handoverLedger interface {
	PendingHandovers() ([]handover.Pending, error)
	SeedSuccessor(name string) (handover.Pending, bool, error)
	ClearHandover(name string) error
}

// SweepHandovers retries or surfaces pending seeds. "Pending for the
// next launch" is not a resting place (🎯T418 clause 5).
func (s *Server) SweepHandovers() {
	if s == nil {
		return
	}
	led, ok := s.migrator.(handoverLedger)
	if !ok || led == nil {
		slog.Info("🎯T418 handover sweep skipped", "migrator_wired", s.migrator != nil)
		return
	}
	pending, err := led.PendingHandovers()
	if err != nil {
		slog.Error("🎯T418 handover sweep: store unreadable", "err", err)
		// List still returns the readable records alongside the error.
	}
	slog.Info("🎯T418 handover sweep", "pending", len(pending))
	now := time.Now()
	overseers, byName := s.planAgentIndex()
	for _, p := range pending {
		if ref, ok := byName[p.Agent]; ok && planusage.PlanMigrateExempt(ref, overseers) {
			if err := led.ClearHandover(p.Agent); err != nil {
				slog.Error("🎯T517 control-plane handover clear failed", "agent", p.Agent, "err", err)
			} else {
				slog.Info("🎯T517 handover reaped", "agent", p.Agent, "reason", "control-plane seat is not force-migrated")
			}
			continue
		}
		inReg := s.agentIsRegistered(p.Agent)
		_, alive := s.liveSender(p.Agent)
		act, reason := handover.ClassifyHandover(p, now, inReg, alive)
		slog.Info("🎯T418 handover classify",
			"agent", p.Agent, "action", string(act), "reason", reason,
			"alive", alive, "registered", inReg)
		switch act {
		case handover.HandoverRetry:
			if _, _, err := led.SeedSuccessor(p.Agent); err != nil {
				slog.Warn("🎯T418 handover retry failed", "agent", p.Agent, "err", err)
			} else {
				slog.Info("🎯T418 handover retry", "agent", p.Agent, "reason", reason)
			}
		case handover.HandoverSurface:
			if !p.Usable() {
				// 🎯T542 defence: ClassifyHandover already reaps COLD
				// records; never surface one that cannot seed.
				if err := led.ClearHandover(p.Agent); err != nil {
					slog.Error("🎯T542 COLD handover reap failed", "agent", p.Agent, "err", err)
				} else {
					slog.Info("🎯T542 handover reaped", "agent", p.Agent, "reason", "COLD — will not surface")
				}
				break
			}
			s.surfacePendingHandover(p, reason, now)
		case handover.HandoverReap:
			if err := led.ClearHandover(p.Agent); err != nil {
				slog.Error("🎯T418 handover reap failed", "agent", p.Agent, "err", err)
			} else {
				slog.Info("🎯T418 handover reaped", "agent", p.Agent, "reason", reason)
			}
		}
	}
}

func (s *Server) surfacePendingHandover(p handover.Pending, reason string, now time.Time) {
	msg := fmt.Sprintf(
		"UNDELIVERED HANDOVER: %s is still pending (%s). %s. The daemon will not wait for a launch that may never come.",
		p.Describe(), p.DescribeAge(now), reason)
	slog.Error("🎯T418 pending handover surfaced",
		"agent", p.Agent, "reason", reason, "age", p.DescribeAge(now))
	s.notifyFleetHealth(msg)
}

// reportFleetMuteIfNeeded is 🎯T418 clause 6: if every registered agent
// is stuck and queued work exists, say so instead of leaving senders
// with perpetual queued/sent.
func (s *Server) reportFleetMuteIfNeeded() {
	q := s.sendQueue()
	backlogs, err := q.Backlogs()
	if err != nil {
		return
	}
	queued := 0
	for _, b := range backlogs {
		queued += b.Depth
	}
	registered := 0
	live := 0
	if s.registry != nil {
		for _, d := range s.registry.List() {
			registered++
			if _, ok := s.liveSender(d.Name); ok {
				live++
			}
		}
	}
	mute, reason := ClassifyFleetMute(registered, live, queued)
	if !mute {
		return
	}
	slog.Error("🎯T418 fleet mute", "reason", reason)
	s.notifyFleetHealth(reason)
}
