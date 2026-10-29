// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package reportcollapse implements 🎯T826: at most one pending non-terminal
// worker report per sender in a receiver's sendq (and the same rule inside
// 🎯T774 digests). Terminal reports, broker refusals, and messages carrying
// oracle or ask slots are never dropped.
package reportcollapse

import (
	"strings"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// RouteSender reads the worker named on an open-intent route fact line.
func RouteSender(text string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != envelope.IntentOpen || fields[1] != envelope.IntentRoute {
			continue
		}
		if sender := strings.TrimSpace(fields[2]); sender != "" {
			return sender, true
		}
	}
	return "", false
}

// IsTerminalReport is true for typed finish-report or scout-report envelopes.
func IsTerminalReport(text string) bool {
	m, err := envelope.Parse(text)
	if err != nil || m == nil {
		return false
	}
	switch m.Kind {
	case envelope.KindFinishReport, envelope.KindScoutReport:
		return true
	default:
		return false
	}
}

// NeverDrop reports payloads that must not be removed by collapse (🎯T826).
func NeverDrop(text string) bool {
	if IsTerminalReport(text) {
		return true
	}
	if strings.Contains(text, "broker protocol: agent_failed:") {
		return true
	}
	if strings.Contains(text, "jevons: gate-id=") || strings.Contains(text, "jevons: oracle sha=") {
		return true
	}
	m, _ := envelope.Parse(text)
	if m == nil {
		return false
	}
	if m.HasOracle() {
		return true
	}
	switch m.Kind {
	case envelope.KindEscalation, envelope.KindTargetFileRequest:
		return true
	default:
		return false
	}
}

// PendingSupersededBy reports whether a pending admitted entry should be
// removed when incoming is appended to the same receiver queue.
func PendingSupersededBy(incoming, old string) bool {
	inSender, ok := RouteSender(incoming)
	if !ok {
		return false
	}
	oldSender, ok := RouteSender(old)
	if !ok || oldSender != inSender {
		return false
	}
	if NeverDrop(old) || IsTerminalReport(old) {
		return false
	}
	if IsTerminalReport(incoming) {
		return true
	}
	// Incoming oracle/ask/refusal replaces stale progress; protected payloads
	// are only never dropped as the *old* entry (NeverDrop(old) above).
	return true
}
