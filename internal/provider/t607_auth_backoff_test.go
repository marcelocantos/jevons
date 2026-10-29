// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
)

// 🎯T607: auth launch backoff grows and keeps spawn count bounded in a fixed window.
func TestT607AuthLaunchBackoffGrowsAndCaps(t *testing.T) {
	b := AuthLaunchBackoff
	prev := time.Duration(0)
	for attempt := 1; attempt <= 6; attempt++ {
		got := b.Delay(attempt)
		if attempt > 1 && got < prev {
			t.Fatalf("Delay(%d)=%v shrank from %v", attempt, got, prev)
		}
		prev = got
	}
	if b.Delay(6) != b.Max {
		t.Fatalf("Delay(6)=%v want cap %v", b.Delay(6), b.Max)
	}
}

func TestT607AuthLaunchBackoffBoundedSpawnsInWindow(t *testing.T) {
	const window = 10 * time.Second
	elapsed := time.Duration(0)
	spawns := 0
	for attempt := 1; elapsed < window; attempt++ {
		spawns++
		elapsed += AuthLaunchBackoff.Delay(attempt)
	}
	if spawns > 4 {
		t.Fatalf("would spawn %d times in %s; want a small bounded count", spawns, window)
	}
	if spawns < 2 {
		t.Fatalf("backoff too aggressive: only %d spawns in %s", spawns, window)
	}
}

func TestT607RunnerUsesAuthBackoffOnAuthExit(t *testing.T) {
	l, lch, _ := testLifecycle(t)
	defer func() {
		ctx, cancel := contextWithTimeout(t, 2*time.Second)
		defer cancel()
		_ = l.Shutdown(ctx)
	}()

	decl := config.ProviderDecl{
		ID: "grok", Transport: config.ProviderTransportLaunch,
		Params: map[string]any{"exec": "echo", "argv": []string{"ok"}},
	}
	if err := l.Reconcile([]config.ProviderDecl{decl}); err != nil {
		t.Fatal(err)
	}
	waitForPhase(t, l, "grok", PhaseRunning)
	proc := lch.latest("grok")
	if proc == nil {
		t.Fatal("no process started")
	}
	proc.crash(errors.New("acp session/new: Authentication required"))
	time.Sleep(200 * time.Millisecond)
	if n := lch.startCount("grok"); n > 1 {
		t.Fatalf("startCount=%d within 200ms; auth backoff should not respawn immediately", n)
	}
}

func contextWithTimeout(t *testing.T, d time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	return ctx, cancel
}
