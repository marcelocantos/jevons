// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package silentresponse classifies fleet-ops replies that must not surface
// to the owner (🎯T238 overseer→owner; 🎯T240 whole-stream seal;
// mcpserver worker→overseer notify).
package silentresponse

import "strings"

// Prefix marks filterable ops replies. Case-insensitive on the filter.
const Prefix = "[silent]"

// StreamClass is the progressive classification of an accumulating assistant
// stream (🎯T240). ACP delivers token deltas; a single fragment rarely holds
// the full [silent] marker.
type StreamClass int

const (
	// Pending — not enough text yet to decide (empty or incomplete prefix).
	Pending StreamClass = iota
	// Silent — sealed or partial text is owner-filterable.
	Silent
	// Visible — cannot become silent (owner-visible prose).
	Visible
)

// Is reports whether agent completion text is owner-filterable (starts with
// [silent], optional leading whitespace / short multi-line lead-in).
func Is(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	lower := strings.ToLower(t)
	prefix := strings.ToLower(Prefix)
	if strings.HasPrefix(lower, prefix) {
		return true
	}
	// Marker on its own line within a short lead-in (first 80 runes).
	head := t
	if len(head) > 80 {
		head = head[:80]
	}
	for _, line := range strings.Split(head, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), prefix) {
			return true
		}
	}
	return false
}

// IsFiltered reports whether a COMPLETE (sealed) text should be hidden from
// the owner: either the explicit [silent] marker (Is), or the 🎯T1054
// heuristic routine-noise classifier (IsRoutineNoise).
//
// This is deliberately a different entry point from Is()/Classify(): those
// two drive the live per-fragment ACP stream (🎯T240), where only the
// literal "[silent]" prefix is safe to commit on early — a growing partial
// accumulation cannot yet rule out an escape word (a safety/incident
// mention) that has not streamed in yet. IsFiltered is for call sites that
// already hold the whole, final text — history/replay rendering and
// one-shot (non-streamed) report bodies — where there is no "later
// fragment" that could still change the verdict.
func IsFiltered(text string) bool {
	return Is(text) || IsRoutineNoise(text)
}

// NoiseBufferRunes bounds how long the live overseer ACP stream may hold
// while deciding whether accumulated text could still turn out to be
// 🎯T1054 routine noise. It sits just above IsRoutineNoise's own length cap
// (240 runes) so a genuinely routine turn always resolves inside this
// buffer, while any longer, substantive answer commits to Visible quickly
// instead of holding up the owner's live view of it.
const NoiseBufferRunes = 256

// ClassifyLive is Classify extended with a bounded hold for the routine-
// noise heuristic (🎯T1054). terminal marks the final fragment of the turn
// (claudia.Event.IsTerminalStop()).
//
// The literal "[silent]" marker (Is/Classify) is safe to commit on a
// partial prefix because it is an explicit, unambiguous opt-in: a prefix of
// "[silent]" can only ever complete to "[silent]" or fail to. IsRoutineNoise
// is a shape heuristic that can be invalidated by an escape word that has
// not streamed in yet (a safety/incident mention after a routine-sounding
// lead-in), so it is only ever consulted once the whole turn is known
// (terminal=true) or once the accumulated text has grown past its own
// length cap (in which case it cannot be routine noise regardless of what
// follows, and there is nothing left to wait for).
//
// NOT YET WIRED into the live overseer ACP stream (internal/server/chat.go):
// doing so naively regressed TestChatWireRoundTripOverWebSocket — the
// existing Pending branch in deliverOverseerEventAdmitted returns early and
// skips the ObserveAgentProgress/NotifyAgentsChanged call that publishes
// the fleet-refresh control frame, which was previously only reachable for
// the rare "[" partial-marker case. Buffering ordinary short messages (this
// function's whole point) hits that branch far more often and silently
// drops fleet-progress notifications for every short held turn. Wiring this
// in needs that gap closed first (call ObserveAgentProgress from the
// Pending branch too, with a care pass over what else assumes "Pending is
// rare and marker-only") — not done in this pass. Kept as a tested,
// ready-to-wire building block.
func ClassifyLive(acc string, terminal bool) StreamClass {
	if Is(acc) {
		return Silent
	}
	t := strings.TrimSpace(acc)
	if t == "" {
		if terminal {
			return Visible
		}
		return Pending
	}
	if terminal {
		if IsRoutineNoise(t) {
			return Silent
		}
		return Visible
	}
	lower := strings.ToLower(t)
	pref := strings.ToLower(Prefix)
	// Incomplete literal marker: "[" / "[s" / "[silent" still Pending.
	if strings.HasPrefix(pref, lower) {
		return Pending
	}
	if len([]rune(t)) <= NoiseBufferRunes {
		return Pending
	}
	return Visible
}

// Classify reports whether accumulating stream text is silent, still pending
// a complete [silent] prefix, or definitely owner-visible (🎯T240).
func Classify(acc string) StreamClass {
	if Is(acc) {
		return Silent
	}
	t := strings.TrimSpace(acc)
	if t == "" {
		return Pending
	}
	lower := strings.ToLower(t)
	pref := strings.ToLower(Prefix)
	// Incomplete marker: "[" / "[s" / "[silent" still Pending.
	if strings.HasPrefix(pref, lower) {
		return Pending
	}
	return Visible
}
