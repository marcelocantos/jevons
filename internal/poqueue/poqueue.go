// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package poqueue classifies a product owner's inbound send queue against the
// responsiveness budget in 🎯T1182 acceptance (directs reach the PO within
// minutes; baseline 2026-10-22 showed 13m+ while the PO ran gates).
package poqueue

import (
	"fmt"
	"time"
)

// MaxDirectAge is how long the oldest queued direct may wait before the PO is
// considered to be blocking its own queue (🎯T1182 live criterion).
const MaxDirectAge = 5 * time.Minute

// Obs is one PO's queue read at a sentinel tick.
type Obs struct {
	Name         string
	Depth        int
	OldestAge    time.Duration
	InFlight     bool
	GraceElapsed bool
}

// Verdict classifies one observation.
type Verdict string

const (
	VerdictOK       Verdict = "ok"
	VerdictBacklog  Verdict = "backlog"
	VerdictInFlight Verdict = "in_flight"
)

// Classify applies the T1182 queue-age rule. An idle PO with a stale queue is
// an immediate fault — nothing is draining it. A PO mid-turn gets grace before
// the same age threshold applies.
func Classify(o Obs) (Verdict, string) {
	if o.Depth <= 0 {
		return VerdictOK, ""
	}
	if !o.InFlight && o.OldestAge >= MaxDirectAge {
		return VerdictBacklog, fmt.Sprintf("po=%s idle depth=%d oldest=%s exceeds %s",
			o.Name, o.Depth, o.OldestAge.Round(time.Second), MaxDirectAge)
	}
	if o.InFlight && !o.GraceElapsed {
		return VerdictInFlight, fmt.Sprintf("po=%s depth=%d oldest=%s in_flight grace",
			o.Name, o.Depth, o.OldestAge.Round(time.Second))
	}
	if o.OldestAge >= MaxDirectAge {
		return VerdictBacklog, fmt.Sprintf("po=%s depth=%d oldest=%s exceeds %s",
			o.Name, o.Depth, o.OldestAge.Round(time.Second), MaxDirectAge)
	}
	return VerdictOK, ""
}

// IsFault reports whether staffops should surface a typed stall.
func IsFault(v Verdict) bool { return v == VerdictBacklog }
