// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"fmt"
	"sync"
	"time"
)

// DefaultWindow is one chatter cycle: identical kind+slots inside this
// window are duplicates; rate caps count inside it too.
const DefaultWindow = time.Minute

// DefaultCaps is the per-kind rate cap inside DefaultWindow. Load-bearing
// kinds are uncapped — a finish-report retry must not vanish. Status-ping
// and ack are the blowout class.
var DefaultCaps = map[Kind]int{
	KindStatusPing: 3,
	KindAck:        8,
}

// Action is what the daemon should do with an enveloped message after
// chatter policy.
type Action int

const (
	// ActionDeliver: send the message.
	ActionDeliver Action = iota
	// ActionDuplicate: identical kind+slots already delivered in this cycle.
	ActionDuplicate
	// ActionRateLimited: this kind has hit its per-cycle cap.
	ActionRateLimited
)

func (a Action) String() string {
	switch a {
	case ActionDeliver:
		return "deliver"
	case ActionDuplicate:
		return "duplicate"
	case ActionRateLimited:
		return "rate_limited"
	default:
		return "unknown"
	}
}

// Observe is one tracker decision.
type Observe struct {
	Action Action
	Kind   Kind
	Count  int // deliveries of this kind from this actor in the window
	Notice string
}

// Tracker is an in-memory chatter ledger. Pure policy plus a clock: no I/O.
// A nil Tracker delivers everything (tests / unenveloped path).
type Tracker struct {
	mu      sync.Mutex
	window  time.Duration
	caps    map[Kind]int
	byActor map[string]*actorWindow
}

type actorWindow struct {
	reset time.Time
	// kindCounts counts deliveries (not drops) per kind in the window.
	kindCounts map[Kind]int
	// seen fingerprints already delivered in the window.
	seen map[string]bool
	// noticed fingerprints that already produced a drop notice, so the
	// notice itself does not become a second chatter loop.
	noticed map[string]bool
}

// NewTracker returns a chatter tracker with DefaultWindow and DefaultCaps.
func NewTracker() *Tracker {
	caps := make(map[Kind]int, len(DefaultCaps))
	for k, n := range DefaultCaps {
		caps[k] = n
	}
	return &Tracker{
		window:  DefaultWindow,
		caps:    caps,
		byActor: map[string]*actorWindow{},
	}
}

// Check records m from actor at now and returns the chatter decision.
// Unenveloped messages (m == nil) and kinds that are not chatter-capped
// always deliver. Load-bearing kinds are never dropped.
func (t *Tracker) Check(actor string, m *Message, now time.Time) Observe {
	if t == nil || m == nil {
		return Observe{Action: ActionDeliver}
	}
	if !m.Kind.ChatterCapped() {
		return Observe{Action: ActionDeliver, Kind: m.Kind}
	}
	actor = stringsOrAnon(actor)
	fp := m.Kind.String() + "\n" + m.SlotsFingerprint()

	t.mu.Lock()
	defer t.mu.Unlock()

	w := t.byActor[actor]
	if w == nil || now.Sub(w.reset) >= t.window {
		w = &actorWindow{
			reset:      now,
			kindCounts: map[Kind]int{},
			seen:       map[string]bool{},
			noticed:    map[string]bool{},
		}
		t.byActor[actor] = w
	}

	count := w.kindCounts[m.Kind]
	if w.seen[fp] {
		notice := ""
		if !w.noticed[fp] {
			w.noticed[fp] = true
			notice = fmt.Sprintf("[chatter] dropped duplicate %s envelope (identical kind+slots within %s)", m.Kind, t.window)
		}
		return Observe{Action: ActionDuplicate, Kind: m.Kind, Count: count, Notice: notice}
	}
	capN, capped := t.caps[m.Kind]
	if capped && capN > 0 && count >= capN {
		notice := ""
		key := "rate:" + string(m.Kind)
		if !w.noticed[key] {
			w.noticed[key] = true
			notice = fmt.Sprintf("[chatter] rate-capped %s (%d/%s)", m.Kind, capN, t.window)
		}
		return Observe{Action: ActionRateLimited, Kind: m.Kind, Count: count, Notice: notice}
	}

	w.seen[fp] = true
	w.kindCounts[m.Kind] = count + 1
	return Observe{Action: ActionDeliver, Kind: m.Kind, Count: count + 1}
}

func stringsOrAnon(s string) string {
	if s == "" {
		return "(anon)"
	}
	return s
}
