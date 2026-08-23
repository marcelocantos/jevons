// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package muxwin is the 🎯T537.1.3 window math: client names a half-open
// coalesced-event window plus halo (margin). That subscription stays live
// until the client submits a new window — the server streams every change
// whose index falls inside it. Hi=0 is exclusive EOF (following). Identity
// (event id) is not used for range arithmetic.
package muxwin

import "fmt"

// HaloProse is the default subscribed halo beyond each open visible edge,
// counting only prose events (user / assistant). Steps and status do not
// spend this budget.
const HaloProse = 100

// HaloMaxExtra caps how many events (including steps) a halo walk may
// add. Steps are free in the prose count, so an MCP-heavy journal would
// otherwise subscribe tens of thousands of rows and mux hydrate never
// reaches meta — empty transcript.
const HaloMaxExtra = 200

// Kind is a coalesced display event class.
type Kind int

const (
	KindOther Kind = iota
	KindUser
	KindAssistant
	KindSteps
	KindStatus
)

// CountsTowardHalo reports whether an event spends the prose halo budget.
func CountsTowardHalo(k Kind) bool {
	return k == KindUser || k == KindAssistant
}

// Resolved is a server-side window. Lo is 1-based inclusive. Hi is exclusive:
// 0 means EOF (following). Otherwise Hi is 1-based exclusive (n+1 is past the
// last event when the window includes the current last event but is frozen).
type Resolved struct {
	Lo, Hi    int
	Following bool
}

// Following reports whether a client-sent hi is exclusive EOF.
func Following(hi int) bool { return hi == 0 }

// Resolve maps a client [lo, hi) onto n coalesced events (indices 1..n).
// Negative bounds are legal only when hi == 0. 0 is exclusive EOF, never start.
func Resolve(lo, hi, n int) (Resolved, error) {
	if n < 0 {
		return Resolved{}, fmt.Errorf("muxwin: n=%d", n)
	}
	if hi < 0 || lo < 0 {
		if hi != 0 {
			return Resolved{}, fmt.Errorf("muxwin: negative indices only when following (hi=0), got [%d, %d)", lo, hi)
		}
	}
	if hi > 0 && lo > hi {
		return Resolved{}, fmt.Errorf("muxwin: empty inverted window [%d, %d)", lo, hi)
	}
	if hi > 0 && lo == 0 {
		return Resolved{}, fmt.Errorf("muxwin: 0 is exclusive EOF, not a start index")
	}

	r := Resolved{Following: hi == 0}
	if n == 0 {
		if r.Following {
			return Resolved{Lo: 1, Hi: 0, Following: true}, nil
		}
		return Resolved{Lo: 1, Hi: 1, Following: false}, nil
	}

	resolveBound := func(b int, isHi bool) (int, error) {
		if b == 0 {
			if isHi {
				return 0, nil
			}
			return 0, fmt.Errorf("muxwin: 0 is exclusive EOF, not a start index")
		}
		if b > 0 {
			if b > n+1 {
				b = n + 1
			}
			if !isHi && b > n {
				b = n
			}
			if !isHi && b < 1 {
				b = 1
			}
			return b, nil
		}
		// negative: from EOF. [-k, 0) is the last k events → lo = n-k+1.
		abs := -b
		if isHi {
			return 0, fmt.Errorf("muxwin: negative hi is not following")
		}
		if abs >= n {
			return 1, nil
		}
		return n - abs + 1, nil
	}

	var err error
	r.Lo, err = resolveBound(lo, false)
	if err != nil {
		return Resolved{}, err
	}
	if r.Following {
		r.Hi = 0
		return r, nil
	}
	r.Hi, err = resolveBound(hi, true)
	if err != nil {
		return Resolved{}, err
	}
	if r.Hi < r.Lo {
		return Resolved{}, fmt.Errorf("muxwin: empty inverted window [%d, %d)", r.Lo, r.Hi)
	}
	return r, nil
}

// Freeze turns a following window into an absolute snapshot of the current
// last n events (hi becomes n+1). A window that already excludes EOF is
// returned unchanged.
func Freeze(r Resolved, n int) Resolved {
	if !r.Following {
		return r
	}
	if n <= 0 {
		return Resolved{Lo: 1, Hi: 1, Following: false}
	}
	hi := n + 1
	lo := r.Lo
	if lo < 1 {
		lo = 1
	}
	if lo > hi {
		lo = hi
	}
	return Resolved{Lo: lo, Hi: hi, Following: false}
}

// Subscribe dilates a visible resolved window by halo prose events on each
// open edge. kinds is 1-based (index i at kinds[i-1]). Following visible
// windows keep Hi=0 (live newer is the socket, not a fetch).
func Subscribe(visible Resolved, kinds []Kind, halo int) Resolved {
	n := len(kinds)
	if halo < 0 {
		halo = 0
	}
	lo := visible.Lo
	if lo < 1 {
		lo = 1
	}
	// Walk older.
	prose := 0
	extra := 0
	for i := lo - 1; i >= 1 && prose < halo && extra < HaloMaxExtra; i-- {
		if CountsTowardHalo(kinds[i-1]) {
			prose++
		}
		lo = i
		extra++
	}
	if visible.Following {
		return Resolved{Lo: lo, Hi: 0, Following: true}
	}
	hi := visible.Hi
	if hi < 1 {
		hi = n + 1
	}
	if hi > n+1 {
		hi = n + 1
	}
	prose = 0
	extra = 0
	for i := hi; i <= n && prose < halo && extra < HaloMaxExtra; i++ {
		if CountsTowardHalo(kinds[i-1]) {
			prose++
		}
		hi = i + 1
		extra++
	}
	return Resolved{Lo: lo, Hi: hi, Following: false}
}

// DilateOlder extends a window only toward older events. Page-up uses this
// so a frozen cursor+count does not pull live-end events into the halo.
func DilateOlder(r Resolved, kinds []Kind, halo int) Resolved {
	lo := r.Lo
	if lo < 1 {
		lo = 1
	}
	if halo < 0 {
		halo = 0
	}
	prose := 0
	extra := 0
	for i := lo - 1; i >= 1 && prose < halo && extra < HaloMaxExtra; i-- {
		if CountsTowardHalo(kinds[i-1]) {
			prose++
		}
		lo = i
		extra++
	}
	return Resolved{Lo: lo, Hi: r.Hi, Following: r.Following}
}

// Contains reports whether a 1-based coalesced index is inside the
// subscribed window. Following (hi=0) has no exclusive end: a new EOF
// event is in-window when index >= Lo. A frozen window does not see
// events at or past Hi, but still sees appends to indices already in
// [Lo, Hi).
func Contains(r Resolved, index int) bool {
	if index < 1 {
		return false
	}
	lo := r.Lo
	if lo < 1 {
		lo = 1
	}
	if index < lo {
		return false
	}
	if r.Following {
		return true
	}
	if r.Hi <= 0 {
		return false
	}
	return index < r.Hi
}

// Need reports 1-based indices in resolved that are not in have.
func Need(r Resolved, n int, have map[int]struct{}) []int {
	if n <= 0 {
		return nil
	}
	lo := r.Lo
	if lo < 1 {
		lo = 1
	}
	hi := r.Hi
	if r.Following || hi == 0 {
		hi = n + 1
	}
	if hi > n+1 {
		hi = n + 1
	}
	var out []int
	for i := lo; i < hi; i++ {
		if _, ok := have[i]; !ok {
			out = append(out, i)
		}
	}
	return out
}
