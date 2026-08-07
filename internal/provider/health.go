// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import "time"

// Phase is the observable lifecycle state of one supervised provider
// (🎯T27.3). It is the unit of "provider health is observable": every
// desired provider always has exactly one phase, and transitions are
// reported through Lifecycle.OnPhase and GET /api/providers.
type Phase string

const (
	// PhaseStarting means the runner is spawning (launch) or dialling
	// (connect) and has not yet observed a healthy provider.
	PhaseStarting Phase = "starting"
	// PhaseRunning means the process is alive (launch) or the endpoint
	// answered its last probe (connect).
	PhaseRunning Phase = "running"
	// PhaseBackoff means the provider died or failed a probe and the
	// runner is waiting out the restart delay.
	PhaseBackoff Phase = "backoff"
	// PhaseStopped means the runner was torn down deliberately — the
	// provider was disabled, removed from config, or the daemon is
	// shutting down. Never the result of a crash.
	PhaseStopped Phase = "stopped"
	// PhaseFailed means the provider is unrunnable as declared (bad argv,
	// missing exec, malformed URL) and no restart will help.
	PhaseFailed Phase = "failed"
)

// Health is a snapshot of one supervised provider, safe to serialise to
// the owner-visible surface. Zero PID / empty Endpoint simply means the
// field does not apply to that transport.
//
// Phase is the only field that describes the present. LastError is
// historical and sticky — the most recent failure, which stays visible
// after recovery so a provider that keeps dying and respawning does not
// read as healthy in the gaps. Restarts is the companion signal.
type Health struct {
	ID        string    `json:"id"`
	Transport string    `json:"transport"` // launch | connect
	Phase     Phase     `json:"phase"`
	Restarts  int       `json:"restarts"`
	PID       int       `json:"pid,omitempty"`      // launch only, when running
	Endpoint  string    `json:"endpoint,omitempty"` // connect only
	LastError string    `json:"last_error,omitempty"`
	Since     time.Time `json:"since"` // when the current phase began
}

// Backoff is the restart delay policy for a crashed or unreachable
// provider: Base, then Base*Factor, Base*Factor², … capped at Max.
// No jitter — a handful of local providers do not need herd protection,
// and determinism keeps the restart oracle honest.
type Backoff struct {
	Base   time.Duration
	Max    time.Duration
	Factor float64
}

// DefaultBackoff is the policy used when Lifecycle is given a zero Backoff.
var DefaultBackoff = Backoff{
	Base:   500 * time.Millisecond,
	Max:    30 * time.Second,
	Factor: 2,
}

// withDefaults fills unset fields from DefaultBackoff so a partially
// specified policy (common in tests: Base only) stays sane.
func (b Backoff) withDefaults() Backoff {
	if b.Base <= 0 {
		b.Base = DefaultBackoff.Base
	}
	if b.Max <= 0 {
		b.Max = DefaultBackoff.Max
	}
	if b.Factor < 1 {
		b.Factor = DefaultBackoff.Factor
	}
	return b
}

// Delay returns the wait before restart attempt n (n starts at 1).
func (b Backoff) Delay(n int) time.Duration {
	b = b.withDefaults()
	if n < 1 {
		n = 1
	}
	d := float64(b.Base)
	for i := 1; i < n; i++ {
		d *= b.Factor
		if d >= float64(b.Max) {
			return b.Max
		}
	}
	if d >= float64(b.Max) {
		return b.Max
	}
	return time.Duration(d)
}
