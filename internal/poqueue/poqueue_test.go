// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package poqueue_test

import (
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/poqueue"
)

func TestClassifyEmptyQueueOK(t *testing.T) {
	v, _ := poqueue.Classify(poqueue.Obs{Name: "jevons-po"})
	if v != poqueue.VerdictOK {
		t.Fatalf("verdict=%q want ok", v)
	}
}

func TestClassifyYoungQueueOK(t *testing.T) {
	v, _ := poqueue.Classify(poqueue.Obs{
		Name: "jevons-po", Depth: 3, OldestAge: 2 * time.Minute,
	})
	if v != poqueue.VerdictOK {
		t.Fatalf("verdict=%q want ok", v)
	}
}

func TestClassifyStaleQueueBacklog(t *testing.T) {
	v, detail := poqueue.Classify(poqueue.Obs{
		Name: "jevons-po", Depth: 15, OldestAge: 13*time.Minute + 43*time.Second,
		GraceElapsed: true,
	})
	if v != poqueue.VerdictBacklog || !poqueue.IsFault(v) {
		t.Fatalf("verdict=%q detail=%q want backlog fault", v, detail)
	}
	if detail == "" {
		t.Fatal("expected detail")
	}
}

func TestClassifyInFlightWithinGraceNotBacklog(t *testing.T) {
	v, _ := poqueue.Classify(poqueue.Obs{
		Name: "jevons-po", Depth: 2, OldestAge: 10 * time.Minute, InFlight: true,
	})
	if v != poqueue.VerdictInFlight {
		t.Fatalf("verdict=%q want in_flight", v)
	}
	if poqueue.IsFault(v) {
		t.Fatal("in-flight within grace is not a backlog fault")
	}
}

func TestClassifyIdleStaleQueueBacklog(t *testing.T) {
	v, _ := poqueue.Classify(poqueue.Obs{
		Name: "jevons-po", Depth: 15, OldestAge: 13 * time.Minute,
	})
	if v != poqueue.VerdictBacklog {
		t.Fatalf("verdict=%q want backlog for idle PO", v)
	}
}
