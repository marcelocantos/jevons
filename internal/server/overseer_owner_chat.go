// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/silentresponse"
)

// classifyOverseerOwnerStream is the 🎯T1140 owner-wire classifier composed
// with the literal [silent] marker (🎯T238). Provider transport failures are
// always owner-visible (fault). Owner-triggered turns are always visible
// except [silent] (🎯T1120 / owner fix spec (a)).
func classifyOverseerOwnerStream(ownerTurn bool, acc string, terminal bool, ev claudia.Event) silentresponse.StreamClass {
	if ev.IsError || isRPCErrorFrame(ev.Raw) || classifyAssistantEvent(ev).IsFailure() {
		return silentresponse.Visible
	}
	if ownerTurn {
		return silentresponse.Classify(acc)
	}
	body, _ := envelope.StripPrefixes(acc)
	if !strings.HasPrefix(strings.TrimLeft(body, " \t"), "```") {
		// T1140 applies to jevons owner-reply fences on the overseer wire.
		// Sidecar seats, "[" prefix holds, and other unmarked prose use T240.
		return silentresponse.Classify(acc)
	}
	term := terminal || (ev.Type == "assistant" && ev.StopReason == "tool_use")
	switch envelope.ClassifyOverseerOwnerStream(acc, term) {
	case envelope.OverseerOwnerSuppress:
		return silentresponse.Silent
	case envelope.OverseerOwnerShow:
		if term {
			return silentresponse.Visible
		}
		return silentresponse.Pending
	default:
		return silentresponse.Pending
	}
}

func overseerOwnerWireSuppress(text string, terminal bool, ev claudia.Event, ownerTurn bool) bool {
	if ev.IsError || isRPCErrorFrame(ev.Raw) || classifyAssistantEvent(ev).IsFailure() {
		return false
	}
	if ownerTurn {
		return silentresponse.Is(text)
	}
	sealed := terminal || ev.IsTerminalStop()
	if !sealed && strings.TrimSpace(text) != "" {
		body, _ := envelope.StripPrefixes(text)
		if !strings.HasPrefix(strings.TrimLeft(body, " \t"), "```") {
			sealed = true
		}
	}
	classTerminal := sealed
	// chatWireLine maps one event; a complete owner-reply is sealed like end_turn.
	if !classTerminal && envelope.ClassifyOverseerOwnerStream(text, true) == envelope.OverseerOwnerShow {
		classTerminal = true
	}
	switch envelope.ClassifyOverseerOwnerStream(text, classTerminal) {
	case envelope.OverseerOwnerSuppress, envelope.OverseerOwnerPending:
		return true
	default:
		return false
	}
}
