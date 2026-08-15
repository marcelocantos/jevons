// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package hostload reads the host's own saturation — CPU run-queue length and
// swap occupancy — so background admission control can see the resource that
// actually runs out first (🎯T463).
//
// On 2026-08-15 the fleet drove a 16-core host to a 1-minute load average of
// 247 with swap pinned at 36.3/37.9 GB, while admission control concurrently
// reported pressure "normal" and admitted every class at tier "full". Cost and
// tokens were fine the whole time; they were simply not the saturated
// dimension. A controller blind to the saturated resource is not a controller,
// so this package supplies the missing reading.
//
// The reading lives here, in the snapshot-producing layer, rather than in
// internal/capacity: the policy stays pure and hermetically testable against a
// synthetic Sample, which is exactly what the target's acceptance asks for.
package hostload

import (
	"runtime"
	"sync"
	"time"
)

// Sample is one reading of host saturation. Every quantity is optional: an
// unavailable reading is reported as zero with Err set, never as a fabricated
// healthy value — "unknown" and "fine" are different statements, and the whole
// point of this package is that conflating them is how the fleet melted a host
// while reporting normal.
type Sample struct {
	// Load1 is the 1-minute load average; 0 means unknown.
	Load1 float64 `json:"load1,omitempty"`
	// Load5 and Load15 are carried for the owner's benefit; policy uses Load1,
	// which is the one that reacts fast enough to shed a spike.
	Load5  float64 `json:"load5,omitempty"`
	Load15 float64 `json:"load15,omitempty"`
	// Cores is the CPU count the load average should be judged against. Read
	// at runtime — never hardcoded, because the same binary runs on a 16-core
	// laptop and on whatever CI gives it.
	Cores int `json:"cores,omitempty"`
	// SwapUsedBytes / SwapTotalBytes describe swap occupancy. A zero total
	// means unknown (or a host with no swap configured), not a full swap.
	SwapUsedBytes  int64 `json:"swap_used_bytes,omitempty"`
	SwapTotalBytes int64 `json:"swap_total_bytes,omitempty"`
	// Source names how the reading was obtained ("darwin sysctl",
	// "linux procfs"), so a surprising number can be traced.
	Source string `json:"source,omitempty"`
	// Err explains why a quantity is missing. It is a string rather than an
	// error so the sample stays JSON-serialisable into the capacity snapshot.
	Err string `json:"err,omitempty"`
	// At is when the reading was taken.
	At time.Time `json:"at,omitzero"`
}

// Known reports whether the sample carries a usable load reading.
func (s Sample) Known() bool { return s.Load1 > 0 && s.Cores > 0 }

// SwapKnown reports whether the sample carries a usable swap reading.
func (s Sample) SwapKnown() bool { return s.SwapTotalBytes > 0 }

// Read takes one reading. It is the platform-dispatched entry point; the
// per-OS implementations live in hostload_<goos>.go.
func Read() Sample {
	s := readPlatform()
	if s.Cores == 0 {
		s.Cores = runtime.NumCPU()
	}
	if s.At.IsZero() {
		s.At = time.Now()
	}
	return s
}

// DefaultTTL is how long a cached reading stays fresh. Admission asks for a
// snapshot on every ambient tick and on every status call, and each reading
// costs a sysctl (a subprocess on darwin) — but a load average that is five
// seconds stale is still the load average that matters, since the kernel's own
// 1-minute figure moves on a far longer timescale than that.
const DefaultTTL = 5 * time.Second

// Cached returns a Read that reuses a reading for ttl. A non-positive ttl
// falls back to DefaultTTL. The returned function is safe for concurrent use.
func Cached(ttl time.Duration) func() Sample {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	var (
		mu   sync.Mutex
		last Sample
	)
	return func() Sample {
		mu.Lock()
		defer mu.Unlock()
		if !last.At.IsZero() && time.Since(last.At) < ttl {
			return last
		}
		last = Read()
		return last
	}
}
