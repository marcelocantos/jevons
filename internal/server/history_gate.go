// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import "sync/atomic"

// historyHydrateGate serialises concurrent GET /api/history work so a large
// chatlog progressive hydrate cannot burn multiple cores (🎯T259). Capacity
// is the max concurrent full-file range reads; excess waiters queue.
type historyHydrateGate struct {
	sem chan struct{}
	in  atomic.Int32
	max atomic.Int32 // peak in-flight (hermetic oracle)
}

func newHistoryHydrateGate(maxConcurrent int) *historyHydrateGate {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &historyHydrateGate{sem: make(chan struct{}, maxConcurrent)}
}

func (g *historyHydrateGate) acquire() {
	if g == nil {
		return
	}
	g.sem <- struct{}{}
	cur := g.in.Add(1)
	for {
		old := g.max.Load()
		if cur <= old || g.max.CompareAndSwap(old, cur) {
			break
		}
	}
}

func (g *historyHydrateGate) release() {
	if g == nil {
		return
	}
	g.in.Add(-1)
	<-g.sem
}

// maxHistoryHydrateConcurrent caps concurrent /api/history handlers (🎯T259).
const maxHistoryHydrateConcurrent = 1

// historyInfoEvery samples successful hydrate pages at Info; the rest Debug.
const historyInfoEvery = 25
