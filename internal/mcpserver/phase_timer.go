// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"time"
)

// agentListSlow is when jevons_agent_list logs where its time went (🎯T804).
const agentListSlow = 2 * time.Second

// phaseTimer records how long each named step of a request took, so a slow
// call can say where its time went (🎯T804).
type phaseTimer struct {
	start, lap time.Time
	phases     []any
}

func newPhaseTimer() *phaseTimer {
	now := time.Now()
	return &phaseTimer{start: now, lap: now}
}

func (t *phaseTimer) mark(name string) {
	now := time.Now()
	t.phases = append(t.phases, name+"_ms", now.Sub(t.lap).Milliseconds())
	t.lap = now
}

func (t *phaseTimer) logIfSlow(msg string, threshold time.Duration) {
	total := time.Since(t.start)
	if total < threshold {
		return
	}
	slog.Warn(msg, append([]any{"total_ms", total.Milliseconds()}, t.phases...)...)
}
