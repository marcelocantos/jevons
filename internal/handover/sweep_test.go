// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package handover_test

import (
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/handover"
)

func TestT418ClassifyHandoverRetryWhenAlive(t *testing.T) {
	p := handover.Pending{
		Agent: "jv", TranscriptPath: "/t.jsonl",
		CreatedAt: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339),
	}
	got, _ := handover.ClassifyHandover(p, time.Now(), true, true)
	if got != handover.HandoverRetry {
		t.Fatalf("alive usable = %s; want retry", got)
	}
}

func TestT418ClassifyHandoverSurfacesStalePending(t *testing.T) {
	p := handover.Pending{
		Agent: "jv", TranscriptPath: "/t.jsonl",
		CreatedAt: time.Now().UTC().Add(-20 * time.Minute).Format(time.RFC3339),
	}
	got, reason := handover.ClassifyHandover(p, time.Now(), true, false)
	if got != handover.HandoverSurface {
		t.Fatalf("stale no-process = %s (%s); want surface", got, reason)
	}
}

func TestT418ClassifyHandoverReapsGoneAgents(t *testing.T) {
	p := handover.Pending{
		Agent: "gone", TranscriptPath: "/t.jsonl",
		CreatedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	}
	got, _ := handover.ClassifyHandover(p, time.Now(), false, false)
	if got != handover.HandoverReap {
		t.Fatalf("left registry = %s; want reap", got)
	}
}

func TestT418ClassifyHandoverReapsDelivered(t *testing.T) {
	p := handover.Pending{Agent: "jv", TranscriptPath: "/t.jsonl", Delivered: true}
	got, _ := handover.ClassifyHandover(p, time.Now(), true, true)
	if got != handover.HandoverReap {
		t.Fatalf("delivered = %s; want reap", got)
	}
}

func TestT418ClassifyHandoverUnknownAgeIsNotWait(t *testing.T) {
	p := handover.Pending{Agent: "jv", TranscriptPath: "/t.jsonl"}
	got, _ := handover.ClassifyHandover(p, time.Now(), true, false)
	if got != handover.HandoverSurface {
		t.Fatalf("no created_at = %s; want surface, not wait on a zero clock", got)
	}
}
