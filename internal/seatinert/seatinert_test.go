// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatinert

import (
	"testing"
	"time"
)

func TestClassifyInertWhenQueuedAndAgeOnlyClimbs(t *testing.T) {
	// 🎯T754 hermetic clause: fixture seat that accepts sends and never
	// advances is classified inert after the observation bound.
	move := time.Date(2026, 9, 21, 5, 35, 0, 0, time.UTC)
	prev := Sample{
		AgeKnown: true,
		Age:      5 * time.Minute,
		LastMove: move,
		Queued:   2,
		InFlight: true,
	}
	curr := Sample{
		AgeKnown: true,
		Age:      8 * time.Minute,
		LastMove: move, // frozen
		Queued:   3,    // still holding (and accumulating)
		InFlight: true,
	}
	if got := Classify(prev, curr); got != Inert {
		t.Fatalf("queued + climbing age = %s, want inert", got)
	}
}

func TestClassifyLongToolCallIsNotInert(t *testing.T) {
	// 🎯T754 hermetic clause: a seat inside one genuinely long tool call
	// is NOT inert — in-flight with climbing age, but no queued prompts.
	move := time.Date(2026, 9, 21, 5, 35, 0, 0, time.UTC)
	prev := Sample{
		AgeKnown: true,
		Age:      5 * time.Minute,
		LastMove: move,
		Queued:   0,
		InFlight: true,
	}
	curr := Sample{
		AgeKnown: true,
		Age:      12 * time.Minute,
		LastMove: move,
		Queued:   0,
		InFlight: true,
	}
	if got := Classify(prev, curr); got != Working {
		t.Fatalf("long tool call (no queue) = %s, want working", got)
	}
}

func TestClassifyAgeBelowFloorIsWorking(t *testing.T) {
	move := time.Date(2026, 9, 21, 5, 35, 0, 0, time.UTC)
	prev := Sample{AgeKnown: true, Age: 30 * time.Second, LastMove: move, Queued: 1}
	curr := Sample{AgeKnown: true, Age: 90 * time.Second, LastMove: move, Queued: 1}
	if got := Classify(prev, curr); got != Working {
		t.Fatalf("below SilenceFloor = %s, want working", got)
	}
}

func TestClassifyTranscriptAdvancedIsWorking(t *testing.T) {
	prevMove := time.Date(2026, 9, 21, 5, 35, 0, 0, time.UTC)
	currMove := prevMove.Add(3 * time.Minute)
	prev := Sample{AgeKnown: true, Age: 5 * time.Minute, LastMove: prevMove, Queued: 2}
	curr := Sample{AgeKnown: true, Age: 10 * time.Second, LastMove: currMove, Queued: 1}
	if got := Classify(prev, curr); got != Working {
		t.Fatalf("transcript advanced = %s, want working", got)
	}
}

func TestClassifyUnknownMeterIsUnknown(t *testing.T) {
	prev := Sample{AgeKnown: false, Queued: 1}
	curr := Sample{AgeKnown: true, Age: 10 * time.Minute, Queued: 1}
	if got := Classify(prev, curr); got != Unknown {
		t.Fatalf("unknown prev = %s, want unknown", got)
	}
}

func TestClassifyNeedsTwoQueuedSamples(t *testing.T) {
	// First observation that holds a queue is not yet inert — need the
	// second climbing sample while still holding.
	move := time.Date(2026, 9, 21, 5, 35, 0, 0, time.UTC)
	prev := Sample{AgeKnown: true, Age: 5 * time.Minute, LastMove: move, Queued: 0, InFlight: true}
	curr := Sample{AgeKnown: true, Age: 8 * time.Minute, LastMove: move, Queued: 2, InFlight: true}
	if got := Classify(prev, curr); got != Working {
		t.Fatalf("first queued sample = %s, want working", got)
	}
}

func TestClassifyFlatFrozenTranscriptPastFloorIsInert(t *testing.T) {
	// Two observations of the exact same frozen reading past the floor,
	// with a queue held throughout — the specimen shape where age stops
	// climbing only because the clock and the mtime are both stuck relative
	// to each other across a short poll interval.
	move := time.Date(2026, 9, 21, 5, 35, 0, 0, time.UTC)
	prev := Sample{AgeKnown: true, Age: 10 * time.Minute, LastMove: move, Queued: 1}
	curr := Sample{AgeKnown: true, Age: 10 * time.Minute, LastMove: move, Queued: 1}
	if got := Classify(prev, curr); got != Inert {
		t.Fatalf("flat frozen + queued = %s, want inert", got)
	}
}
