// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"regexp"
	"strings"
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
//   - [event: worker-idle] … Worker: <name>  → idle:<name>
//   - [Agent <name> responded] …             → agent:<name>
func notifyCoalesceKey(text string) string {
	t := strings.TrimSpace(text)
	if t == "" || isOwnerNotifyText(t) {
		return ""
	}
	// Worker-idle (and same-class enter-idle) event bodies name the worker.
	if strings.HasPrefix(t, "[event:") {
		if worker := notifyWorkerLine(t); worker != "" {
			// Only idle-class events coalesce per worker; other events keep order.
			if eventName, ok := notifyEventName(t); ok {
				switch eventName {
				case "worker-idle", "idle-nudge", "idle-nudge-brief", "post-restart-wake":
					return "idle:" + worker
				}
			}
		}
		return ""
	}
	// Agent turn-complete notify: latest body wins for the same agent.
	if name, ok := notifyAgentRespondedName(t); ok {
		return "agent:" + name
	}
	return ""
}

var (
	notifyEventNameRe = regexp.MustCompile(`(?i)^\[event:\s*([^\]]+)\]`)
	notifyWorkerRe    = regexp.MustCompile(`(?m)^Worker:\s*(\S+)\s*$`)
	notifyAgentRe     = regexp.MustCompile(`(?i)^\[Agent\s+([^\]]+?)\s+responded\]`)
)

func notifyEventName(text string) (string, bool) {
	m := notifyEventNameRe.FindStringSubmatch(strings.TrimSpace(text))
	if len(m) < 2 {
		return "", false
	}
	return strings.ToLower(strings.TrimSpace(m[1])), true
}

func notifyWorkerLine(text string) string {
	m := notifyWorkerRe.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func notifyAgentRespondedName(text string) (string, bool) {
	m := notifyAgentRe.FindStringSubmatch(strings.TrimSpace(text))
	if len(m) < 2 {
		return "", false
	}
	name := strings.TrimSpace(m[1])
	if name == "" {
		return "", false
	}
	return name, true
}

// isOwnerNotifyText reports whether the queue item is a genuine owner turn
// (userTurnPrefix). Owner items never coalesce and always drain first.
func isOwnerNotifyText(text string) bool {
	return strings.HasPrefix(text, userTurnPrefix)
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
