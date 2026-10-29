// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatinert

import (
	"testing"
	"time"
)

func TestInertRequiresClimbingAgeAndQueue(t *testing.T) {
	r := NewRegistry()
	t0 := time.Date(2026, 9, 21, 5, 35, 0, 0, time.UTC)
	base := Input{
		Now: t0, TranscriptAgeSec: 60, QueueDepth: 2,
		Alive: true,
	}
	if inert, fresh := r.Observe("po", base); inert || fresh {
		t.Fatalf("first sample must not classify inert")
	}
	t1 := t0.Add(MinObservationGap)
	if inert, _ := r.Observe("po", Input{
		Now: t1, TranscriptAgeSec: 80, QueueDepth: 2, Alive: true,
	}); inert {
		t.Fatalf("one climbing window is not enough")
	}
	t2 := t1.Add(MinObservationGap)
	inert, fresh := r.Observe("po", Input{
		Now: t2, TranscriptAgeSec: 110, QueueDepth: 3, Alive: true,
	})
	if !inert || !fresh {
		t.Fatalf("two climbing windows with queue want inert,fresh got %v,%v", inert, fresh)
	}
	inert, fresh = r.Observe("po", Input{
		Now: t2.Add(MinObservationGap), TranscriptAgeSec: 140, QueueDepth: 3, Alive: true,
	})
	if !inert || fresh {
		t.Fatalf("steady inert must not re-announce fresh")
	}
}

func TestLongToolCallWithProviderMotionIsNotInert(t *testing.T) {
	r := NewRegistry()
	t0 := time.Date(2026, 9, 21, 6, 0, 0, 0, time.UTC)
	transcriptStale := 300.0
	motion := t0.Add(-30 * time.Second)
	for i := 0; i < 3; i++ {
		at := t0.Add(time.Duration(i) * MinObservationGap)
		inert, _ := r.Observe("worker", Input{
			Now: at, TranscriptAgeSec: transcriptStale + float64(i*25),
			QueueDepth: 1, Alive: true,
			ProviderMotionAt: motion,
		})
		if inert {
			t.Fatalf("long tool call sample %d classified inert", i)
		}
	}
}

func TestEmptyQueueResetsInert(t *testing.T) {
	r := NewRegistry()
	t0 := time.Date(2026, 9, 21, 6, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		r.Observe("seat", Input{
			Now: t0.Add(time.Duration(i)*MinObservationGap),
			TranscriptAgeSec: 10 + float64(i*30), QueueDepth: 1, Alive: true,
		})
	}
	st, ok := r.Get("seat")
	if !ok || !st.Inert {
		t.Fatal("want inert before drain")
	}
	inert, _ := r.Observe("seat", Input{
		Now: t0.Add(3*MinObservationGap), TranscriptAgeSec: 100, QueueDepth: 0, Alive: true,
	})
	if inert {
		t.Fatal("drained queue clears inert")
	}
}
