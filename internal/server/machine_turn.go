// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// noticeKindMachineTurn marks an agent_note that bounds a notify-queue drain
// the owner did not start (🎯T1169). It is not a user turn for replay or
// statedbTailStart — only typ=user rows count there.
const noticeKindMachineTurn = "machine-turn"

// machineTurnSourceNames returns deduped sender labels for a fleet batch, in
// drain order. Agent-responded and idle seat facts name the seat; anything
// else is grouped as fleet (same rule as correspondentForBatch).
func machineTurnSourceNames(batch []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, note := range batch {
		name, ok := notifyAgentRespondedName(note)
		if !ok || name == "" {
			if n, ok := openingSeatFact(note, envelope.IntentIdle); ok && n != "" {
				name = n
			} else {
				name = CorrespondentFleet
			}
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func machineTurnSummary(batch []string) string {
	n := len(batch)
	if n == 0 {
		return ""
	}
	names := machineTurnSourceNames(batch)
	if len(names) == 0 {
		return fmt.Sprintf("%d machine report(s)", n)
	}
	return fmt.Sprintf("%d machine report(s): %s", n, strings.Join(names, ", "))
}

// journalMachineTurnBoundary persists one agent_note before a fleet notify
// drain hands the overseer a provider turn (🎯T1169). Owner batches are
// unchanged — their boundary remains the turn_origin owner user frame.
func (s *Server) journalMachineTurnBoundary(batch []string) {
	if s == nil || len(batch) == 0 {
		return
	}
	text := machineTurnSummary(batch)
	if text == "" {
		return
	}
	line, err := json.Marshal(map[string]any{
		"type":      "agent_note",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"text":      text,
		"notice": map[string]string{
			"kind": noticeKindMachineTurn,
		},
	})
	if err != nil {
		slog.Error("machine-turn boundary marshal failed", "err", err)
		return
	}
	if !s.persistChatLine(string(line)) {
		slog.Error("machine-turn boundary not durable")
	}
}
