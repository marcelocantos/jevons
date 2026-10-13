// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "log/slog"

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
	s.notifyFleetHealth("mute", reason)
}
