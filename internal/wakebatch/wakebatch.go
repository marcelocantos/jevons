// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package wakebatch coalesces machine-generated events into one digest
// per recipient instead of one wake apiece (🎯T392.2).
//
// A coordinator wake is not cheap. In the 🎯T392 baseline jevons-po
// carried 235k of context per model call and the overseer 192k, so
// delivering one line of "a worker went idle" bought roughly a million
// input tokens. Of jevons-po's recorded prompts, 27% were idle nudges,
// 13% daemon-restart notices and 12% bare system-reminders: over half of
// what woke the fleet's most expensive agent was neither the owner nor a
// worker result.
//
// The information content of those events is additive and order-free —
// "these four workers are idle" says everything four separate wakes said
// — so batching divides the cost by the batch size at no loss. That is
// only true for machine-generated events. Owner turns and worker replies
// are never batched: an owner waiting on a reply and a worker reporting a
// result both need the coordinator now, and delay there is the failure
// 🎯T378 exists to prevent.
//
// This package is pure. It holds events and formats digests; it never
// sends anything and owns no timer.
package wakebatch

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Event is one machine-generated wake waiting to be delivered.
type Event struct {
	// Recipient is the agent that would have been woken.
	Recipient string
	// Kind groups like events within a digest ("worker-idle", …).
	Kind string
	// Subject is what the event is about, usually an agent name. Repeats
	// of the same (Kind, Subject) collapse: a worker that goes idle three
	// times before the flush is still one idle worker.
	Subject string
	// Detail is the human-readable line for the digest.
	Detail string
	At     time.Time
}

// Batcher accumulates events per recipient between flushes.
type Batcher struct {
	// Window is how long events accumulate before a flush is due. Zero
	// disables batching: Add reports the event as immediately due, which
	// is the pre-🎯T392.2 behaviour and the safe fallback.
	Window time.Duration

	pending map[string][]Event
	firstAt map[string]time.Time
}

// New returns a Batcher with the given window.
func New(window time.Duration) *Batcher {
	return &Batcher{
		Window:  window,
		pending: map[string][]Event{},
		firstAt: map[string]time.Time{},
	}
}

// Add queues an event. It returns true when the caller should deliver
// immediately — because batching is off — rather than wait for a flush.
//
// Duplicate (Kind, Subject) pairs within a window collapse onto the most
// recent, so a worker that flaps idle does not inflate the digest.
func (b *Batcher) Add(ev Event) (deliverNow bool) {
	if b == nil || b.Window <= 0 {
		return true
	}
	if b.pending == nil {
		b.pending = map[string][]Event{}
		b.firstAt = map[string]time.Time{}
	}
	list := b.pending[ev.Recipient]
	for i, existing := range list {
		if existing.Kind == ev.Kind && existing.Subject == ev.Subject {
			list[i] = ev
			b.pending[ev.Recipient] = list
			return false
		}
	}
	if len(list) == 0 {
		b.firstAt[ev.Recipient] = ev.At
	}
	b.pending[ev.Recipient] = append(list, ev)
	return false
}

// Due returns the recipients whose oldest pending event has aged past the
// window. Recipients are sorted so delivery order is stable.
func (b *Batcher) Due(now time.Time) []string {
	if b == nil {
		return nil
	}
	var out []string
	for recipient, first := range b.firstAt {
		if len(b.pending[recipient]) > 0 && !now.Before(first.Add(b.Window)) {
			out = append(out, recipient)
		}
	}
	sort.Strings(out)
	return out
}

// Take removes and returns a recipient's pending events.
func (b *Batcher) Take(recipient string) []Event {
	if b == nil {
		return nil
	}
	evs := b.pending[recipient]
	delete(b.pending, recipient)
	delete(b.firstAt, recipient)
	return evs
}

// Pending reports how many events are queued for a recipient.
func (b *Batcher) Pending(recipient string) int {
	if b == nil {
		return 0
	}
	return len(b.pending[recipient])
}

// FormatDigest renders events as one message. A single event renders as
// itself: wrapping one line in digest ceremony would make the common case
// longer than what it replaced.
func FormatDigest(evs []Event) string {
	if len(evs) == 0 {
		return ""
	}
	if len(evs) == 1 {
		return evs[0].Detail
	}
	byKind := map[string][]Event{}
	var kinds []string
	for _, ev := range evs {
		if _, seen := byKind[ev.Kind]; !seen {
			kinds = append(kinds, ev.Kind)
		}
		byKind[ev.Kind] = append(byKind[ev.Kind], ev)
	}
	sort.Strings(kinds)

	var b strings.Builder
	fmt.Fprintf(&b, "[digest: %d events while you were working]\n", len(evs))
	for _, kind := range kinds {
		group := byKind[kind]
		fmt.Fprintf(&b, "\n%s (%d):\n", kind, len(group))
		sort.Slice(group, func(i, j int) bool { return group[i].Subject < group[j].Subject })
		for _, ev := range group {
			fmt.Fprintf(&b, "  - %s\n", strings.TrimSpace(ev.Detail))
		}
	}
	b.WriteString("\nThese were coalesced to spend one turn instead of " +
		fmt.Sprint(len(evs)) + ". Handle them together.")
	return b.String()
}
