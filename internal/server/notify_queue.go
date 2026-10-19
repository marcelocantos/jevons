// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T291: overseer notify queue must not starve owner chat under fleet churn.
//
// Pure helpers here keep coalesce keys, owner/fleet partition, and flood
// bounding hermetically testable without a live ACP process.

// notifyCoalesceKey returns a stable key for notes that may replace an
// earlier pending copy of the same logical event. Empty means "do not
// coalesce" — keep every copy in order (owner turns, unique worker replies
// that are not agent-responded shape, one-off alerts).
//
// Coalesced shapes (🎯T291 acceptance 2):
//   - a note that opens with an idle seat fact → idle:<name>
//   - a note that opens with the agent-responded prefix and a route fact → agent:<name>
//
// A rendered [event:] line or [Agent … responded] line is not a key.
func notifyCoalesceKey(text string) string {
	t := strings.TrimSpace(text)
	if t == "" || isOwnerNotifyText(t) {
		return ""
	}
	if name, ok := openingSeatFact(t, envelope.IntentIdle); ok {
		return "idle:" + name
	}
	if name, ok := notifyAgentRespondedName(t); ok {
		return "agent:" + name
	}
	return ""
}

// openingSeatFact reads "open-intent <kind> <name>" from the first line.
func openingSeatFact(text, kind string) (string, bool) {
	line, _, _ := strings.Cut(text, "\n")
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 3 || fields[0] != envelope.IntentOpen || fields[1] != kind || fields[2] == "" {
		return "", false
	}
	return fields[2], true
}

// notifyAgentRespondedName reads the route fact on the line after the
// agent-responded prefix. A rendered [Agent … responded] banner is not a name.
func notifyAgentRespondedName(text string) (string, bool) {
	line, rest, found := strings.Cut(strings.TrimSpace(text), "\n")
	if !found || strings.TrimSpace(line) != envelope.PrefixAgentResponded {
		return "", false
	}
	return openingSeatFact(rest, envelope.IntentRoute)
}

// isOwnerNotifyText reports whether the queue item is a genuine owner turn.
// The decision is the owner-turn fact on line 1. A rendered "[user]" line
// is not an owner turn. Owner items never coalesce and always drain first.
func isOwnerNotifyText(text string) bool {
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line) == envelope.PrefixOwnerTurn
}

// ownerQueueFact prepends the owner-turn fact to a row from the owner-queue
// file that was written before the fact existed. The file stores only owner
// turns, so membership is the declaration. The display bytes are left as they
// are. A row that already opens with the fact is unchanged.
func ownerQueueFact(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	if strings.TrimSpace(line) == envelope.PrefixOwnerTurn {
		return text
	}
	return envelope.PrefixOwnerTurn + "\n" + text
}

// coalesceNotifyEnqueue appends note to queue, replacing any earlier item
// that shares the same coalesce key (same worker idle / same agent reply).
// Non-coalescable notes always append. Order of unrelated items is preserved;
// a replacement keeps the original index so older siblings stay ahead.
func coalesceNotifyEnqueue(queue []string, note string) []string {
	key := notifyCoalesceKey(note)
	if key == "" {
		return append(queue, note)
	}
	for i, existing := range queue {
		if notifyCoalesceKey(existing) == key {
			out := make([]string, len(queue))
			copy(out, queue)
			out[i] = note
			return out
		}
	}
	return append(queue, note)
}

// takeNotifyDrainBatch peels the next delivery batch from queue.
//
// Owner priority (🎯T291 acceptance 3): if any owner turn is pending, take
// exactly one owner note (never batch owner speech with fleet noise).
// Otherwise take the whole fleet batch so a flood drains in one prompt.
//
// Returns (batch, rest, ownerBatch).
func takeNotifyDrainBatch(queue []string) (batch, rest []string, ownerBatch bool) {
	if len(queue) == 0 {
		return nil, nil, false
	}
	var owners, fleet []string
	for _, n := range queue {
		if isOwnerNotifyText(n) {
			owners = append(owners, n)
		} else {
			fleet = append(fleet, n)
		}
	}
	if len(owners) > 0 {
		return []string{owners[0]}, append(owners[1:], fleet...), true
	}
	return fleet, nil, false
}

// queueHasOwner reports whether any owner turn is still pending.
func queueHasOwner(queue []string) bool {
	for _, n := range queue {
		if isOwnerNotifyText(n) {
			return true
		}
	}
	return false
}

// requeueNotifyFront puts batch back at the front, then re-coalesces so a
// busy-defer + flood of same-worker idles still bounds depth (🎯T291).
func requeueNotifyFront(batch, queue []string) []string {
	// Preserve batch order first, then any notes that arrived during send.
	merged := make([]string, 0, len(batch)+len(queue))
	merged = append(merged, batch...)
	merged = append(merged, queue...)
	// Rebuild with coalesce so same-key items collapse after a defer.
	var out []string
	for _, n := range merged {
		out = coalesceNotifyEnqueue(out, n)
	}
	return out
}
