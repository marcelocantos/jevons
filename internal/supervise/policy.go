// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package supervise decides whether the daily daemon needs bringing back
// (🎯T405).
//
// Until this existed, nothing outside the daemon's own process tree was
// responsible for it being up. Restart mechanics were elaborate — thrash
// policy (🎯T218), lock serialisation (🎯T392.5), build snapshots
// (🎯T254.2) — and not one line of it watched whether the result was
// serving. Every restart was a trapeze act with no net, and any failure
// anywhere in that path left the fleet down until the owner happened to
// open the cockpit. That happened on 2026-08-09 and again on 2026-08-10,
// when a worker's restart killed the daemon whose shutdown then killed
// the worker, and with it the script that would have started the
// replacement.
//
// The policy is pure so the decision can be tested without a port, a
// daemon, or a clock. Effects — probing, restarting, notifying — live in
// cmd/jevons-watchdog, which launchd runs on an interval and which is
// therefore outside every process tree this is meant to survive.
package supervise

import (
	"fmt"
	"time"
)

// Action is what the supervisor should do about what it just saw.
type Action int

const (
	// ActionNone covers both "healthy" and "down, but not yet mine to
	// act on" — the reason string separates them for the log.
	ActionNone Action = iota
	// ActionRestart invokes the restart script.
	ActionRestart
	// ActionRecovered means serving resumed after an outage: close the
	// record so the owner is told what happened.
	ActionRecovered
)

func (a Action) String() string {
	switch a {
	case ActionRestart:
		return "restart"
	case ActionRecovered:
		return "recovered"
	default:
		return "none"
	}
}

// Notify is the severity of an out-of-band notice to the owner, matching
// blurter's vocabulary. Empty means say nothing.
type Notify string

const (
	NotifyNone    Notify = ""
	NotifyProblem Notify = "problem"
	NotifyOK      Notify = "ok"
)

// Config bounds the window between the daemon stopping and the
// supervisor acting.
type Config struct {
	// Grace is how long the port may be unserved before this is an
	// outage rather than a bounce in progress. A legitimate restart
	// rebuilds from a HEAD worktree and then waits up to 90s for health,
	// so a grace shorter than that would fire a second restart into the
	// middle of the first every time. Serialisation would make that
	// survivable, not useful.
	Grace time.Duration
	// RetryInterval is the gap between the first attempts.
	RetryInterval time.Duration
	// MaxAttempts is how many attempts happen at RetryInterval before
	// the supervisor concludes recovery is not taking and escalates.
	MaxAttempts int
	// BackoffInterval paces attempts after that. Deliberately not
	// "give up": a daemon that cannot start now because HEAD does not
	// build starts fine an hour later when someone lands the fix, and a
	// supervisor that stopped trying would leave that outage standing.
	BackoffInterval time.Duration
}

// DefaultConfig is what the launchd job runs with.
func DefaultConfig() Config {
	return Config{
		Grace:           90 * time.Second,
		RetryInterval:   2 * time.Minute,
		MaxAttempts:     3,
		BackoffInterval: 10 * time.Minute,
	}
}

// State is what the supervisor remembers between runs. launchd starts a
// fresh process every interval, so nothing survives in memory.
type State struct {
	// DownSince is when the port was first seen unserved. Zero means
	// the last observation was healthy.
	DownSince time.Time `json:"down_since,omitzero"`
	// Attempts counts restarts invoked for the current outage.
	Attempts int `json:"attempts,omitempty"`
	// LastAttempt paces them.
	LastAttempt time.Time `json:"last_attempt,omitzero"`
	// Notified records that the owner has been told about this outage,
	// so a five-minute outage costs one notice rather than ten.
	Notified bool `json:"notified,omitempty"`
	// Escalated records that the owner has been told recovery is not
	// taking — the second and last notice of any single outage.
	Escalated bool `json:"escalated,omitempty"`
}

// Down reports whether an outage is currently open.
func (s State) Down() bool { return !s.DownSince.IsZero() }

// Decision is one supervisor verdict.
type Decision struct {
	Action Action
	Notify Notify
	// Escalated marks the notice as "recovery is not taking" rather
	// than "the daemon is down".
	Escalated bool
	// Reason is written to the log verbatim; it is the only place a
	// human reads why a cycle did nothing.
	Reason string
	// Downtime is how long the port had been unserved at decision time.
	Downtime time.Duration
}

// Decide is the whole policy: what the probe saw plus what we remember,
// in; a verdict and the state to persist, out.
func Decide(cfg Config, st State, serving bool, now time.Time) (Decision, State) {
	if serving {
		if !st.Down() {
			return Decision{Reason: "serving"}, State{}
		}
		down := now.Sub(st.DownSince)
		d := Decision{
			Action:   ActionRecovered,
			Reason:   fmt.Sprintf("serving again after %s and %d restart attempt(s)", down.Round(time.Second), st.Attempts),
			Downtime: down,
		}
		// Only report recovery from an outage the owner heard about.
		// A bounce that resolved inside the grace window is not news.
		if st.Notified {
			d.Notify = NotifyOK
		}
		return d, State{}
	}

	if !st.Down() {
		st.DownSince = now
		return Decision{Reason: "not serving; first observation, inside the restart window"}, st
	}

	down := now.Sub(st.DownSince)
	if down < cfg.Grace {
		return Decision{
			Reason:   fmt.Sprintf("not serving for %s; a restart in flight has until %s", down.Round(time.Second), cfg.Grace),
			Downtime: down,
		}, st
	}

	interval := cfg.RetryInterval
	if st.Attempts >= cfg.MaxAttempts {
		interval = cfg.BackoffInterval
	}
	if !st.LastAttempt.IsZero() && now.Sub(st.LastAttempt) < interval {
		return Decision{
			Reason:   fmt.Sprintf("not serving for %s; attempt %d was %s ago, next in %s", down.Round(time.Second), st.Attempts, now.Sub(st.LastAttempt).Round(time.Second), interval),
			Downtime: down,
		}, st
	}

	priorAttempts := st.Attempts
	st.Attempts++
	st.LastAttempt = now
	d := Decision{
		Action:   ActionRestart,
		Reason:   fmt.Sprintf("not serving for %s; restart attempt %d", down.Round(time.Second), st.Attempts),
		Downtime: down,
	}
	switch {
	case !st.Notified:
		d.Notify = NotifyProblem
		st.Notified = true
	case priorAttempts >= cfg.MaxAttempts && !st.Escalated:
		d.Notify = NotifyProblem
		d.Escalated = true
		st.Escalated = true
	}
	return d, st
}
