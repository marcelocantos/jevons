// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agentreport"
)

// 🎯T744 — a turn that ends while the daemon holds no event sink is recovered
// from the seat's transcript, not lost.
//
// Specimen (2026-09-21): jv-t746-report-collision survived a daemon bounce.
// Fleet resume is serial (03:44:45 → 03:47:20) and the wiring pass runs only
// after it, so four terminal stops (17:45:20Z … 17:47:06Z, one of them a
// scout-report) landed on a seat with no sink. claudia's SubscribeEvents
// delivers future events only, and agentEventSink is the sole path that
// stores a report, tells the parent, and feeds the idle tracker — so those
// turns never happened as far as the daemon, the parent, and 🎯T597's
// phaseAdvanceHandoff (which reads agentreport.Latest) were concerned. The
// parent could not tell "went mute" from "nothing to say".
//
// The sink cannot be attached to the past, so the past is read back: when a
// sink attaches to a seat that already existed, its transcript is scanned for
// terminal stops newer than the seat's latest stored report and older than
// the attach, and the most recent one goes through the ordinary notify path
// (stored verbatim, so Latest() sees the envelope the seat wrote). The parent
// additionally gets a notice saying the turn was recovered, so a bounce is
// audible instead of inferred (🎯T405).

// MissedTurnLookback bounds how far back a recovery reaches. A seat with no
// stored report at all would otherwise replay whatever its transcript ends
// with, however old.
const MissedTurnLookback = 6 * time.Hour

// missedTurn is one terminal stop the daemon never observed.
type missedTurn struct {
	At   time.Time
	Text string
}

// scanMissedTurns reads a Claude-shaped transcript and returns terminal stops
// with At in (after, before), oldest first. Text accumulates across assistant
// lines and resets at each terminal stop, mirroring agentEventSink. A stop
// with no text is not a report and is skipped, as the sink skips it.
func scanMissedTurns(path string, after, before time.Time) ([]missedTurn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []missedTurn
	var text strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var rec struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Message   struct {
				StopReason string `json:"stop_reason"`
				Content    any    `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) != nil || rec.Type != "assistant" {
			continue
		}
		if blocks, ok := rec.Message.Content.([]any); ok {
			for _, b := range blocks {
				if m, ok := b.(map[string]any); ok && m["type"] == "text" {
					if t, _ := m["text"].(string); t != "" {
						text.WriteString(t)
					}
				}
			}
		}
		if !(claudia.Event{Type: "assistant", StopReason: rec.Message.StopReason}).IsTerminalStop() {
			continue
		}
		body := text.String()
		text.Reset()
		at, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
		if err != nil || body == "" || !at.After(after) || !at.Before(before) {
			continue
		}
		out = append(out, missedTurn{At: at, Text: body})
	}
	return out, sc.Err()
}

// recoverMissedTurns delivers the newest terminal stop the daemon missed for
// name, if any, and reports how many were missed. attachedAt is the moment
// the live sink took over: anything after it belongs to the sink.
func (s *Server) recoverMissedTurns(name, transcriptPath string, attachedAt time.Time) int {
	if s == nil || strings.TrimSpace(transcriptPath) == "" || s.agentReportStateDir() == "" {
		return 0
	}
	after := attachedAt.Add(-MissedTurnLookback)
	if rec, err := agentreport.Latest(s.agentReportStateDir(), name); err == nil && rec.At.After(after) {
		after = rec.At
	}
	missed, err := scanMissedTurns(transcriptPath, after, attachedAt)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("🎯T744 could not scan transcript for missed turns",
				"agent", name, "path", transcriptPath, "err", err)
		}
		return 0
	}
	if len(missed) == 0 {
		return 0
	}
	last := missed[len(missed)-1]
	slog.Warn("🎯T744 recovering a turn that ended while the event stream was dark",
		"agent", name, "missed_turns", len(missed), "turn_ended", last.At.Format(time.RFC3339))
	s.logLifecycle(compAgentLifecycle, "missed_turn", "recovered", map[string]any{
		"agent": name, "missed_turns": len(missed), "turn_ended": last.At.Format(time.RFC3339),
	})

	// Verbatim: the stored report must be the envelope the seat wrote.
	s.notify(name, last.Text)

	if parent := s.registryParent(name); parent != "" && !s.isOverseerAgent(parent) {
		note := fmt.Sprintf("[🎯T744 seat %s went unobserved across a daemon bounce or relaunch: %d turn(s) "+
			"ended with no event stream attached. The newest (ended %s) was recovered from its transcript "+
			"and stored/delivered just above; earlier ones are in its transcript only. Silence from this "+
			"seat before now did not mean it was idle.]",
			name, len(missed), last.At.Format(time.RFC3339))
		if _, err := s.deliverByName(parent, note, OriginAgent, false); err != nil {
			slog.Error("🎯T744 missed-turn notice to parent failed",
				"agent", name, "parent", parent, "err", err)
		}
	}
	return len(missed)
}
