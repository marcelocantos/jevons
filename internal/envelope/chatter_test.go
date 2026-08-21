// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"testing"
	"time"
)

func TestChatterDedupesIdenticalStatusPing(t *testing.T) {
	tr := NewTracker()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	msg := &Message{Kind: KindStatusPing, Target: "T509", Status: ProgressInProgress}

	first := tr.Check("jv-t509-envelopes", msg, now)
	if first.Action != ActionDeliver {
		t.Fatalf("first=%s", first.Action)
	}
	second := tr.Check("jv-t509-envelopes", msg, now.Add(time.Second))
	if second.Action != ActionDuplicate {
		t.Fatalf("second=%s want duplicate", second.Action)
	}
	if second.Notice == "" {
		t.Fatal("first drop in a burst must be detectable (a notice)")
	}
	third := tr.Check("jv-t509-envelopes", msg, now.Add(2*time.Second))
	if third.Action != ActionDuplicate {
		t.Fatalf("third=%s", third.Action)
	}
	if third.Notice != "" {
		t.Fatalf("subsequent drops must not chatter a second notice: %q", third.Notice)
	}
}

func TestChatterRateCapsStatusPing(t *testing.T) {
	tr := NewTracker()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	for i := 0; i < DefaultCaps[KindStatusPing]; i++ {
		msg := &Message{
			Kind:   KindStatusPing,
			Target: "T509",
			Status: ProgressInProgress,
			SHA:    string(rune('a' + i)), // distinct slots so this is rate, not dedup
		}
		got := tr.Check("w", msg, now.Add(time.Duration(i)*time.Second))
		if got.Action != ActionDeliver {
			t.Fatalf("i=%d action=%s", i, got.Action)
		}
	}
	extra := &Message{Kind: KindStatusPing, Target: "T509", Status: ProgressInProgress, SHA: "zzzz"}
	got := tr.Check("w", extra, now.Add(30*time.Second))
	if got.Action != ActionRateLimited {
		t.Fatalf("action=%s want rate_limited", got.Action)
	}
	if got.Notice == "" {
		t.Fatal("rate cap must be detectable")
	}
}

func TestChatterDoesNotDropFinishReport(t *testing.T) {
	tr := NewTracker()
	now := time.Now()
	msg := validFinish()
	for i := 0; i < 10; i++ {
		got := tr.Check("w", msg, now.Add(time.Duration(i)*time.Second))
		if got.Action != ActionDeliver {
			t.Fatalf("i=%d action=%s — load-bearing kinds are never dropped", i, got.Action)
		}
	}
}

func TestChatterWindowResets(t *testing.T) {
	tr := NewTracker()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	msg := &Message{Kind: KindAck}
	if tr.Check("w", msg, now).Action != ActionDeliver {
		t.Fatal("first")
	}
	if tr.Check("w", msg, now.Add(time.Second)).Action != ActionDuplicate {
		t.Fatal("dup inside window")
	}
	later := tr.Check("w", msg, now.Add(DefaultWindow+time.Second))
	if later.Action != ActionDeliver {
		t.Fatalf("after window action=%s", later.Action)
	}
}

func TestNilTrackerDelivers(t *testing.T) {
	var tr *Tracker
	got := tr.Check("w", validFinish(), time.Now())
	if got.Action != ActionDeliver {
		t.Fatalf("action=%s", got.Action)
	}
}
