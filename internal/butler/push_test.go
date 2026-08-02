// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package butler_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/butler"
)

func TestFormatEventPush(t *testing.T) {
	got := butler.FormatEventPush("ci", "green on master")
	if got != "[event: ci] green on master" {
		t.Fatalf("FormatEventPush = %q", got)
	}
	got = butler.FormatEventPush("  ", "  hello  ")
	if got != "[event: unknown] hello" {
		t.Fatalf("empty source default = %q", got)
	}
}

// TestPushEventRehydratesAndDelivers is the 🎯T34 e2e oracle: event fires
// → target (possibly stopped) receives the push after rehydrate.
func TestPushEventRehydratesAndDelivers(t *testing.T) {
	f := newFakeFleet()
	f.mintID = "eeeeeeee-ffff-4000-8000-000000000001"
	b := newLifecycleButler(t, t.TempDir(), f)

	if _, err := b.Spawn(butler.SpawnArgs{ID: "po", WorkDir: "/work/x", Description: "PO"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	f.Stop("po") // idle / GC'd

	reply, err := b.PushEvent("po", "worker-finished", "slice A landed")
	if err != nil {
		t.Fatalf("PushEvent: %v", err)
	}
	if f.launches < 2 {
		t.Fatalf("expected rehydrate launch, launches=%d", f.launches)
	}
	if !strings.Contains(reply, "[event: worker-finished]") || !strings.Contains(reply, "slice A landed") {
		t.Fatalf("reply missing event payload: %q", reply)
	}
}

func TestPushEventUnreachableTypedError(t *testing.T) {
	f := newFakeFleet()
	f.mintID = "eeeeeeee-ffff-4000-8000-000000000002"
	b := newLifecycleButler(t, t.TempDir(), f)
	if _, err := b.Spawn(butler.SpawnArgs{ID: "w", WorkDir: "/work/x"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	f.Stop("w")
	f.launchErr = errFakeUnreachable

	_, err := b.PushEvent("w", "timer", "tick")
	if err == nil {
		t.Fatal("want typed error for unreachable agent")
	}
	if !strings.Contains(err.Error(), "push") || !strings.Contains(err.Error(), "rehydrate") {
		// Direct wraps rehydrate failures; PushEvent prefixes push.
		if !strings.Contains(err.Error(), "could not rehydrate") && !strings.Contains(err.Error(), "push") {
			t.Fatalf("unexpected error shape: %v", err)
		}
	}
}

func TestPushEventMissingTarget(t *testing.T) {
	f := newFakeFleet()
	b := newLifecycleButler(t, t.TempDir(), f)
	_, err := b.PushEvent("nope", "ci", "green")
	if err == nil || !strings.Contains(err.Error(), "no thread") {
		t.Fatalf("want no-thread error, got %v", err)
	}
}

// errFakeUnreachable is a stable error so rehydrate failure is observable.
var errFakeUnreachable = errString("process unreachable")

type errString string

func (e errString) Error() string { return string(e) }
