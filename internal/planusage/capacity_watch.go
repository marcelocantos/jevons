// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"sort"
	"sync"
	"time"
)

// CapacityWatch notices a subscription plan becoming admissible again after
// it was not (🎯T977): an exhausted, low, hot or ahead plan whose window has
// reset or whose headroom has returned.
//
// On 2026-10-01 the product owner wrote "will continue once capacity allows"
// and ended its turn, and nothing ever said capacity was back. The fleet sat
// quiet for six hours with headroom on the plan. A restoration is the event
// that wakes agents which stood work down for capacity.
//
// Admissible is the mint verdict the rest of the policy uses (!MintIneligible,
// an owner override included). A plan whose reading is not available is
// unknown, not inadmissible: it neither arms nor fires, so a plan that merely
// became readable again is not announced as restored.
type CapacityWatch struct {
	mu       sync.Mutex
	blocked  map[string]bool // provider -> last readable verdict was inadmissible
	observed map[string]bool // provider -> has a readable verdict
}

// Observe folds one snapshot in and returns the plans that have just become
// admissible again, sorted by provider. The first readable verdict for a
// plan only records it: a daemon that boots onto a healthy plan announces
// nothing.
func (w *CapacityWatch) Observe(s Snapshot, now time.Time, th Thresholds) []Backend {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.blocked == nil {
		w.blocked = map[string]bool{}
		w.observed = map[string]bool{}
	}
	var restored []Backend
	for _, be := range s.Backends {
		if be.Provider == "" || be.Status != StatusAvailable {
			continue
		}
		// 🎯T842: a stale reading is unknown, not a verdict — composing with
		// the doctrine already applied to a failed read (🎯T677: unreadable
		// is unknown, never exhausted). A stale backend neither arms the
		// watch (it must not be announced as the plan going bad) nor
		// disarms it (it must not be misread as "restored" and wake a
		// fleet that stood work down for capacity on a live-looking but
		// outdated snapshot). Skip it entirely: the next readable verdict,
		// whenever it lands, decides.
		if be.Stale {
			continue
		}
		inadmissible := MintIneligible(be, now, th)
		if w.observed[be.Provider] && w.blocked[be.Provider] && !inadmissible {
			restored = append(restored, be)
		}
		w.observed[be.Provider] = true
		w.blocked[be.Provider] = inadmissible
	}
	sort.Slice(restored, func(i, j int) bool { return restored[i].Provider < restored[j].Provider })
	return restored
}
