// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 🎯T592: the store the resume path reads must be the store recording
// turns.
//
// 🎯T548.2 made SQLite the live mux store and demoted state_dir/chatlog/
// <overseer>.jsonl to import-once history, so persistChatLine stopped
// appending once statedb had rows. Tool stamps kept arriving on the
// ungated ObserveMCPToolCall path, so the file went on growing with
// type=progress while the last type=user and type=assistant records
// froze at 2026-08-26T08:44. That was invisible in the cockpit — the UI
// paints from statedb — but the 🎯T328 owner-intent-resume path read
// that JSONL, so every daemon bounce on 08-29/30/31 recovered the same
// five-day-old Cursor-cycle instruction and re-issued it as MANDATORY
// open owner work.
//
// The fix is not to make the JSONL write again — the turns live in
// statedb, so 🎯T328 reads statedb (open_intent.go's
// loadOpenIntentDialogueStateDB) and refuses a MANDATORY resume from a
// degraded store. What remains here is the alarm: if whichever store is
// live stops recording turns while frames keep arriving, the daemon
// says so instead of five silent days. The alarm re-arms only on a
// SUCCESSFUL append — a turn whose write failed is the defect, not a
// reset.

// turnGapProgressFrames is how many non-turn frames may be journaled
// after the last recorded turn before the gap is loud. One owner
// exchange is a handful of frames; this is comfortably more than one.
const turnGapProgressFrames = 120

// turnGapWindow is how long a turn-free stretch may last before the gap
// is loud. It matches the 🎯T328 staleness window so the daemon warns
// about exactly the condition that makes the store unusable for resume.
const turnGapWindow = time.Hour

// chatTurnLine reports whether a chat wire line is a real conversation
// turn — an owner message or an overseer reply carrying text. Progress,
// tool stamps, status chrome, agent notes and system frames are not.
func chatTurnLine(line string) (kind, text string, ok bool) {
	var d struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(line)), &d) != nil {
		return "", "", false
	}
	switch strings.ToLower(strings.TrimSpace(d.Type)) {
	case "user", "assistant":
		kind = strings.ToLower(strings.TrimSpace(d.Type))
	default:
		return "", "", false
	}
	text = chatTurnText(d.Message.Content)
	if text == "" {
		text = strings.TrimSpace(d.Text)
	}
	if text == "" {
		return "", "", false
	}
	return kind, text, true
}

func chatTurnText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if strings.TrimSpace(p.Text) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimSpace(p.Text))
	}
	return strings.TrimSpace(b.String())
}

// chatTurnGap watches the journal for the failure 🎯T592 exists to kill:
// frames keep arriving while no turn is recorded. It is pure — the
// caller supplies the clock and decides how to shout — so the alarm is
// testable without a daemon.
type chatTurnGap struct {
	mu sync.Mutex

	lastTurnAt   time.Time
	lastTurnKind string
	lastTurnText string
	seen         int
	warned       bool
	started      time.Time
}

// observe folds one journaled line in and returns a warning when the gap
// first becomes loud. Only a DURABLY recorded turn resets the gap and
// re-arms the alarm (🎯T592): a turn whose append failed reached no store
// the resume path can read, so it counts toward the gap like any other
// unrecorded frame.
func (g *chatTurnGap) observe(line string, now time.Time, durable bool) string {
	if g == nil {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.started.IsZero() {
		g.started = now
	}
	if kind, text, ok := chatTurnLine(line); ok && durable {
		g.lastTurnAt = now
		g.lastTurnKind = kind
		g.lastTurnText = text
		g.seen = 0
		g.warned = false
		return ""
	}
	g.seen++
	if g.warned || g.seen < turnGapProgressFrames {
		return ""
	}
	since := g.lastTurnAt
	if since.IsZero() {
		since = g.started
	}
	if now.Sub(since) < turnGapWindow {
		return ""
	}
	g.warned = true
	if g.lastTurnAt.IsZero() {
		return "chat: no owner or overseer turn has ever been journaled" +
			" while " + strconv.Itoa(g.seen) + " frames were recorded (🎯T592)"
	}
	return "chat: " + strconv.Itoa(g.seen) + " frames journaled with no turn since the last " +
		g.lastTurnKind + " turn at " + g.lastTurnAt.Format(time.RFC3339) +
		" (" + truncTurn(g.lastTurnText, 120) + ") — the 🎯T328 resume path reads this store (🎯T592)"
}

// lastTurn reports the newest journaled turn, for tests and diagnostics.
func (g *chatTurnGap) lastTurn() (time.Time, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lastTurnAt, g.lastTurnKind
}

func truncTurn(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// observeChatTurnGap folds a journaled line into the turn-gap watch and
// shouts once when frames keep arriving with no durably recorded turn
// behind them (🎯T592). Loud on purpose: the whole defect was five
// silent days. durable is the append's own result — pass false for a
// failed write so it cannot masquerade as recorded.
func (s *Server) observeChatTurnGap(line string, durable bool) {
	if s == nil {
		return
	}
	s.turnGapOnce.Do(func() { s.turnGap = &chatTurnGap{} })
	if warning := s.turnGap.observe(line, time.Now(), durable); warning != "" {
		slog.Error("chat: DURABILITY GAP — owner chatlog is recording no turns",
			"warning", warning)
	}
}
