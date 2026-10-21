// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/noopwedge"
	"github.com/marcelocantos/jevons/internal/relayroute"
)

// Finish recognition is a declared envelope (🎯T766.4). A sentence is not a
// finish and not a bare claim clause. Reap and notify read
// envelope.IsDoneReport. finishClauseDelimiter remains for the 🎯T750 quote
// mask and the local-done clause split. It is not a finish decision.

// finishClauseDelimiter splits a report into clauses for the 🎯T750 mask
// and the local-done split. It does not decide a finish.
func finishClauseDelimiter(r rune) bool {
	switch r {
	case '.', ',', ';', ':', '!', '?', '\n', '(', ')', '—', '–':
		return true
	}
	return false
}

// hasFinishShape is not a finish decision (🎯T766.4). A lowered sentence is
// not a declared done report. Reap and notify read envelope.IsDoneReport.
func hasFinishShape(string) bool { return false }

// bareAckTurnReport reports whether a turn's terminal text is a content-free
// acknowledgement that should stay routine rather than reach the overseer
// (🎯T502).
//
// Observed live twice on 2026-08-18: jv-t445-finish-guard and jv-t399-web-green
// each ended a turn with the 22-byte "No response requested." and the notify
// seam delivered it to the overseer as owner-decision traffic — the T392.7
// content router only guards the fleet-send path, so nothing between the event
// sink and the overseer ever read the text. The ack is a turn-boundary
// artefact (🎯T402: the model's answer to a message that reads as
// informational), not a report; escalating it spends an overseer turn on
// nothing.
//
// Recognition is declared, never a completion sentence (🎯T766.4): a valid
// ack envelope is an ack; noopwedge.IsExactAck is 🎯T402's exact-match
// vocabulary (an ack with a payload after it is a report, not an ack); a
// declared done report (envelope.IsDoneReport) is not suppressed; and
// relayroute.Classify (🎯T392.7) keeps a direct overseer route escalating.
// A sentence that says the work is done is not that decision.
func bareAckTurnReport(text string) bool {
	if m, err := envelope.Parse(text); m != nil && err == nil && m.Kind == envelope.KindAck {
		return true
	}
	if envelope.IsDoneReport(text) {
		return false
	}
	if !noopwedge.IsExactAck(text) {
		return false
	}
	return relayroute.Classify(text) != relayroute.RouteOverseer
}
