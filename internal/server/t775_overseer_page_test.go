// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"
	"time"
)

// 🎯T775: the owner is paged once per outage after the threshold.
func TestT775PageOncePerOutageAfterThreshold(t *testing.T) {
	s := &Server{}
	var pages []string
	var recovered int
	s.SetOverseerPager(func(subject, body, key string, rec bool) {
		if rec {
			recovered++
			return
		}
		pages = append(pages, body)
	})
	t0 := time.Now()
	s.SetOverseerDownReason("launch failed")
	s.overseerDownSince = t0

	s.reconcileOverseerPage(t0.Add(DefaultOverseerPageAfter - time.Second))
	if len(pages) != 0 {
		t.Fatalf("paged before threshold: %v", pages)
	}
	s.reconcileOverseerPage(t0.Add(DefaultOverseerPageAfter))
	s.reconcileOverseerPage(t0.Add(time.Hour))
	if len(pages) != 1 || pages[0] != "launch failed" {
		t.Fatalf("want exactly one page carrying the reason, got %v", pages)
	}
	s.SetOverseerDownReason("")
	s.reconcileOverseerPage(t0.Add(2 * time.Hour))
	if recovered != 1 {
		t.Fatalf("want one recovery page, got %d", recovered)
	}
	// A second outage pages again.
	s.SetOverseerDownReason("again")
	s.overseerDownSince = t0.Add(3 * time.Hour)
	s.reconcileOverseerPage(t0.Add(3*time.Hour + DefaultOverseerPageAfter))
	if len(pages) != 2 {
		t.Fatalf("second outage should page: %v", pages)
	}
}

// 🎯T775: a give-up re-arms when its cause clears.
func TestT775GiveUpRearmsWhenCauseClears(t *testing.T) {
	now := time.Now()
	reg := cockpitObs{Registered: true}
	max := DefaultCockpitMaxAttempts

	// Launch-exhausted: waits, then re-arms.
	if cockpitShouldRearm(reg, max, max, now, false, now.Add(time.Minute), DefaultCockpitRearmAfter) {
		t.Fatal("re-armed too early")
	}
	if !cockpitShouldRearm(reg, max, max, now, false, now.Add(DefaultCockpitRearmAfter), DefaultCockpitRearmAfter) {
		t.Fatal("launch-exhausted give-up never re-arms")
	}
	// Resume-denied: still latched → stays; latch cleared → immediate.
	latched := cockpitObs{Registered: true, ResumeDenied: true}
	if cockpitShouldRearm(latched, max, max, now, true, now.Add(time.Hour), time.Minute) {
		t.Fatal("re-armed while latch still set")
	}
	if !cockpitShouldRearm(reg, max, max, now, true, now, DefaultCockpitRearmAfter) {
		t.Fatal("cleared latch did not re-arm")
	}
	if cockpitShouldRearm(reg, max-1, max, now, false, now.Add(time.Hour), time.Minute) {
		t.Fatal("re-arm fired without a give-up")
	}
}
