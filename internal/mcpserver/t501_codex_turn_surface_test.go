// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

// 🎯T501 — codex work-agent mints died as unbriefed_seat because the
// turn-begin watch took the durable-transcript branch on a codex agent.
// claudia v0.23.0 advertises a Claude-shaped JSONLPath for every
// provider, so the watch waited 45s on ~/.claude/projects/….jsonl — a
// file a codex-app-server backend never writes — while the agent's real
// evidence surface, its live event stream, went unwatched.
//
// The oracle pair here replays that failure and proves the fix at the
// same seam: an observer whose JSONLPath points at a transcript that can
// never exist (the specimen), watched raw (the defect: session events
// ignored, window burned, "no transcript was ever created") and watched
// through liveStreamObserver (the fix: the event settles the watch).

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// t501Stub is a live agent process with a phantom Claude-shaped
// transcript path — the shape claudia v0.23.0 gives every codex and grok
// agent. Events published to subscribers are its only real activity.
type t501Stub struct {
	path string

	mu   sync.Mutex
	subs map[int64]claudia.EventFunc
	next int64
}

func (o *t501Stub) SubscribeEvents(fn claudia.EventFunc) int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.subs == nil {
		o.subs = map[int64]claudia.EventFunc{}
	}
	o.next++
	o.subs[o.next] = fn
	return o.next
}

func (o *t501Stub) UnsubscribeEvents(token int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.subs, token)
}

func (o *t501Stub) JSONLPath() string { return o.path }
func (o *t501Stub) Alive() bool       { return true }

func (o *t501Stub) publish(ev claudia.Event) {
	o.mu.Lock()
	fns := make([]claudia.EventFunc, 0, len(o.subs))
	for _, fn := range o.subs {
		fns = append(fns, fn)
	}
	o.mu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}

// TestT501LiveStreamObserverLetsSessionEventsConfirmTheTurn: wrapped, the
// codex agent's event stream is the evidence surface, and the first event
// settles the watch well inside the window.
func TestT501LiveStreamObserverLetsSessionEventsConfirmTheTurn(t *testing.T) {
	stub := &t501Stub{path: filepath.Join(t.TempDir(), "phantom-session.jsonl")}
	watch := observeTurnFor(liveStreamObserver{stub}, "", 5*time.Second)

	go func() {
		time.Sleep(50 * time.Millisecond)
		stub.publish(claudia.Event{Type: "agent_message_delta"})
	}()

	start := time.Now()
	ev := watch()
	if !ev.SessionEvent {
		t.Fatalf("session event must confirm the turn on a live-stream surface, got %+v", ev)
	}
	if !ev.Positive() {
		t.Fatal("a session event is positive evidence")
	}
	if ev.Durable {
		t.Fatal("a live-stream watch must not claim a durable transcript")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("the watch must settle on the event, not wait out the window (took %s)", elapsed)
	}
}

// TestT501RawCodexObserverReplaysTheUnbriefedSeatFailure: the same agent
// watched without the wrapper is the T501 specimen — events are published
// and ignored, the window burns down, and the verdict is the exact
// "no transcript was ever created" that reaped every codex mint. This
// control keeps the wrapper honest: if the durable branch ever starts
// accepting events, the fix is no longer load-bearing and this fails.
func TestT501RawCodexObserverReplaysTheUnbriefedSeatFailure(t *testing.T) {
	stub := &t501Stub{path: filepath.Join(t.TempDir(), "phantom-session.jsonl")}
	watch := observeTurnFor(stub, "", 400*time.Millisecond)

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				stub.publish(claudia.Event{Type: "agent_message_delta"})
			}
		}
	}()

	ev := watch()
	if ev.Positive() {
		t.Fatalf("durable branch must not be settled by session events, got %+v", ev)
	}
	if !strings.Contains(ev.Detail, "no transcript was ever created") {
		t.Fatalf("expected the T501 specimen verdict, got %q", ev.Detail)
	}
}

// TestT501ProviderSurfaceClassification: only Claude-shaped agents keep
// the durable ~/.claude transcript; codex and grok are live-stream.
func TestT501ProviderSurfaceClassification(t *testing.T) {
	for provider, durable := range map[claudia.Provider]bool{
		"":                     true,
		claudia.ProviderClaude: true,
		claudia.ProviderCodex:  false,
		claudia.ProviderGrok:   false,
	} {
		if got := providerKeepsClaudeTranscript(provider); got != durable {
			t.Errorf("providerKeepsClaudeTranscript(%q) = %v, want %v", provider, got, durable)
		}
	}
}
