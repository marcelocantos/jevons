// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package seatinert decides whether a live seat that still accepts sends is
// actually working, or merely queuing while nothing moves (🎯T754).
//
// On 2026-09-21 the product-owner seat answered every send with
// "queued, prompt in flight" for forty minutes while its transcript sat
// still. Dead-seat detection (🎯T751) and born-stuck detection (🎯T751.1 /
// 🎯T679.2) both said the seat was fine: a send settled, the process was
// alive, a transcript existed. The missing third state is inert — a seat
// whose sends queue, whose age only climbs, and which produces nothing.
//
// THE DISCRIMINATOR is the conjunction of two consecutive observations:
//
//   - the seat holds queued prompts (daemon backlog depth > 0); and
//   - transcript_age only climbs across the two samples; and
//   - the current age is past SilenceFloor.
//
// A seat legitimately inside one long tool call is NOT inert: it may be
// in-flight with a climbing age, but it does not hold queued prompts. A
// seat whose transcript advanced between samples is not inert either —
// age falling (or last-move advancing) is the working signal.
//
// Detection and reporting only. Whether to remint, reprovider, or wait
// stays with the overseer (🎯T751.1 remint discipline).
package seatinert

import "time"

// SilenceFloor is how long a transcript may sit, while the seat holds
// queued prompts, before two climbing samples may call it inert. Shorter
// than the 🎯T927 wedged-turn bound (15m) so the climbing-age signal is
// visible before the wedge path; longer than a normal mid-tool pause so a
// healthy in-flight seat is not flagged on one quiet sample.
const SilenceFloor = 2 * time.Minute

// Sample is one observation of a seat's transcript age and queue hold.
type Sample struct {
	// AgeKnown is false when the transcript meter could not locate a
	// reading (🎯T677): unknown is never treated as climbing zero.
	AgeKnown bool
	// Age is the transcript's distance from the observation clock.
	Age time.Duration
	// LastMove is the transcript mtime; zero when unknown.
	LastMove time.Time
	// Queued is the daemon backlog depth for this seat. Zero means the
	// seat holds no undelivered/unstarted prompts — the long-tool-call
	// case, even when InFlight is true.
	Queued int
	// InFlight is whether a turn is believed running. Informational only:
	// in-flight without a queue is never inert.
	InFlight bool
}

// HoldsQueued reports whether the seat has undelivered or unstarted prompts.
func (s Sample) HoldsQueued() bool { return s.Queued > 0 }

// Verdict is what two consecutive samples decide.
type Verdict int

const (
	// Working: not inert — age moved, no queue, or not yet past the floor.
	Working Verdict = iota
	// Unknown: at least one sample could not be read honestly.
	Unknown
	// Inert: queued prompts held, age only climbed, past SilenceFloor.
	Inert
)

// String renders a verdict for logs and wire reports.
func (v Verdict) String() string {
	switch v {
	case Inert:
		return "inert"
	case Unknown:
		return "unknown"
	default:
		return "working"
	}
}

// Classify decides inert-vs-working from two consecutive observations of
// the same seat. prev is the earlier sample; curr is the later one.
//
// Rules, in order:
//  1. Either sample unknown → Unknown (never invent climbing from a
//     missing meter).
//  2. Current sample holds no queued prompts → Working (long tool call
//     with climbing age stays working).
//  3. Current age below SilenceFloor → Working (too early to decide).
//  4. Transcript advanced (last-move later, or age fell) → Working.
//  5. Age only climbed (or stayed at/above the floor with the same
//     last-move) while both samples held a queue → Inert.
//  6. Otherwise Working (first sample with a queue, age flat below a
//     meaningful climb, …).
func Classify(prev, curr Sample) Verdict {
	if !prev.AgeKnown || !curr.AgeKnown {
		return Unknown
	}
	if !curr.HoldsQueued() {
		return Working
	}
	if curr.Age < SilenceFloor {
		return Working
	}
	if transcriptAdvanced(prev, curr) {
		return Working
	}
	if !prev.HoldsQueued() {
		// First sample that holds a queue: need a second climb observation.
		return Working
	}
	if ageOnlyClimbed(prev, curr) {
		return Inert
	}
	return Working
}

// transcriptAdvanced reports a seat that produced work between samples.
func transcriptAdvanced(prev, curr Sample) bool {
	if !prev.LastMove.IsZero() && !curr.LastMove.IsZero() && curr.LastMove.After(prev.LastMove) {
		return true
	}
	// Age falling means the transcript moved closer to now.
	if curr.Age+time.Second < prev.Age {
		return true
	}
	return false
}

// ageOnlyClimbed reports monotonically non-decreasing age with no last-move
// advance. A flat age at the same last-move also counts once past the floor:
// two observations of the same frozen transcript are the specimen.
func ageOnlyClimbed(prev, curr Sample) bool {
	if curr.Age+time.Second < prev.Age {
		return false
	}
	if !prev.LastMove.IsZero() && !curr.LastMove.IsZero() && curr.LastMove.After(prev.LastMove) {
		return false
	}
	return true
}
