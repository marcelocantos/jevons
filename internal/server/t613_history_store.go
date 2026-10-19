// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/muxwin"
	"github.com/marcelocantos/jevons/internal/statedb"
)

// 🎯T613: every reader of conversation history must read the store that
// still records turns. Since 🎯T548.2 that store is statedb (SQLite);
// state_dir/chatlog/<overseer>.jsonl is import-once archive — progress
// frames may still land there, but user/assistant turns do not. A history
// path that keeps reading the JSONL paints six-day-old, out-of-order
// content (the 2026-09-01 owner symptom that opened this target).
//
// Pattern mirrors 🎯T592 (owner-intent resume): statedb when it has rows,
// JSONL only as the pre-statedb / empty-store fallback, and a call-site
// comment on every deliberate archive read.

// historySource is which durable store a history read came from.
type historySource string

const (
	historySourceStateDB historySource = "statedb"
	historySourceChatlog historySource = "chatlog_archive"
)

// overseerHistoryWindow returns a page of overseer conversation frames
// for GET /api/history. Prefer statedb (the live turn store). The
// chatlog JSONL is archive-only fallback when statedb has no rows.
//
// end/limit keep the 🎯T57 exclusive-end shape: the window is the last
// `limit` events with absolute index < end (clamped to total+1), and
// total is the journal length. Indexes are 1-based on statedb and
// 0-based on the JSONL archive; start is reported in the same space as
// total so existing clients keep paging.
func (s *Server) overseerHistoryWindow(end, limit int) (lines []string, start, total int, src historySource, err error) {
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	name := s.overseerAgentName()
	if n := s.statedbN(name); n > 0 {
		// 1-based absolute indexes. Treat API end as exclusive upper bound
		// in the same space as total (= N): end=N yields idxs 1..N-1? No —
		// clients pass end=total expecting the last page. Map 0-based
		// exclusive end onto 1-based events the way chatlog does:
		// window [end-limit, end) in 0-based line space becomes
		// Range(end-limit+1, end+1) when total lines == N and idxs are 1..N.
		total = n
		if end <= 0 {
			end = total
		}
		if end > total {
			// Empty page, still report total — same as ReadRange past EOF.
			return nil, end - limit, total, historySourceStateDB, nil
		}
		lo0 := end - limit
		if lo0 < 0 {
			lo0 = 0
		}
		lo := lo0 + 1
		hi := end + 1
		evs := s.statedbRange(name, lo, hi)
		out := make([]string, 0, len(evs))
		for _, ev := range evs {
			body := strings.TrimSpace(string(ev.Body))
			if body == "" {
				continue
			}
			if echoedOwnerTurnLine(body) {
				continue
			}
			// Coalesce is a no-op on statedb bodies (already folded on
			// write); stamp a timestamp from the row when the body lacks one
			// so clients can sort in time order.
			out = append(out, historyLineWithTS(body, ev.TS))
		}
		return out, lo0, total, historySourceStateDB, nil
	}

	// Archive fallback: pre-statedb state dirs, or a store that has not
	// been imported yet. Deliberate — JSONL is not the live turn store
	// (🎯T613 / 🎯T548.2).
	s.mu.RLock()
	clog := s.chatLog
	s.mu.RUnlock()
	if clog == nil {
		return nil, 0, 0, historySourceChatlog, fmt.Errorf("no history")
	}
	raw, total, err := clog.ReadRange(end-limit, end)
	if err != nil {
		return nil, 0, 0, historySourceChatlog, err
	}
	display := chatlog.CoalesceStreamLines(raw)
	out := make([]string, 0, len(display))
	for _, ln := range display {
		if echoedOwnerTurnLine(ln) {
			continue
		}
		out = append(out, ln)
	}
	return out, end - len(raw), total, historySourceChatlog, nil
}

// historyLineWithTS ensures a frame carries a timestamp so a hard reload
// can paint in time order (🎯T613 acceptance). statedb rows always have
// ts; older body shapes sometimes omit it.
func historyLineWithTS(body, ts string) string {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return body
	}
	var m map[string]any
	if json.Unmarshal([]byte(body), &m) != nil {
		return body
	}
	if _, ok := m["timestamp"]; ok {
		return body
	}
	if _, ok := m["ts"]; ok {
		return body
	}
	m["timestamp"] = ts
	b, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return string(b)
}

// overseerReplayTail streams the most recent sealed turns for /ws/chat
// connect hydrate. Prefer statedb; JSONL is archive-only fallback.
// Returns (start, total, source, err) where start is the first index of
// the replayed window (0-based for archive, 1-based-minus-one for
// statedb so history_meta.older keeps meaning "lines before the tail").
func (s *Server) overseerReplayTail(maxTurns int, write func(line string) error) (start, total int, src historySource, err error) {
	if maxTurns <= 0 {
		maxTurns = historyReplayTurns
	}
	name := s.overseerAgentName()
	if n := s.statedbN(name); n > 0 {
		lo := s.statedbTailStartForPaint(name, maxTurns)
		if lo < 1 {
			lo = 1
		}
		evs := s.statedbRange(name, lo, n+1)
		for _, ev := range evs {
			body := strings.TrimSpace(string(ev.Body))
			if body == "" {
				continue
			}
			if echoedOwnerTurnLine(body) {
				continue
			}
			line := historyLineWithTS(body, ev.TS)
			if err := write(line); err != nil {
				return lo - 1, n, historySourceStateDB, err
			}
		}
		return lo - 1, n, historySourceStateDB, nil
	}

	// Archive fallback (🎯T613): chatlog JSONL when statedb has no rows.
	s.mu.RLock()
	clog := s.chatLog
	s.mu.RUnlock()
	if clog == nil {
		return 0, 0, historySourceChatlog, nil
	}
	start, total, err = clog.ReplayTailSealed(maxTurns, func(line string) error {
		if echoedOwnerTurnLine(line) {
			return nil
		}
		return write(line)
	})
	return start, total, historySourceChatlog, err
}

// overseerRecap builds a short text recap of recent turns for rewind
// rotate+recap. Prefer statedb so the surviving context is the live
// conversation, not the frozen JSONL archive (🎯T613).
func (s *Server) overseerRecap(maxTurns, maxBytes int) string {
	if maxTurns <= 0 {
		maxTurns = 30
	}
	if maxBytes <= 0 {
		maxBytes = 6 << 10
	}
	name := s.overseerAgentName()
	if n := s.statedbN(name); n > 0 {
		lo := s.statedbTailStart(name, maxTurns)
		if lo < 1 {
			lo = 1
		}
		evs := s.statedbRange(name, lo, n+1)
		return recapFromEvents(evs, maxTurns, maxBytes)
	}
	// Archive fallback (🎯T613): pre-statedb JSONL only.
	s.mu.RLock()
	clog := s.chatLog
	s.mu.RUnlock()
	if clog == nil {
		return ""
	}
	return clog.Recap(maxTurns, maxBytes)
}

func recapFromEvents(evs []muxwin.Event, maxTurns, maxBytes int) string {
	type turn struct {
		role string
		text string
	}
	var turns []turn
	for _, ev := range evs {
		switch ev.Type {
		case "user":
			text := statedb.AssistantProse("user", string(ev.Body))
			if text == "" {
				text = wireContentText(json.RawMessage(extractMessageContent(ev.Body)))
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			if len(turns) == 0 || turns[len(turns)-1].role != "user" || turns[len(turns)-1].text != text {
				turns = append(turns, turn{"user", text})
			}
		case "assistant":
			text := statedb.AssistantProse(ev.Type, string(ev.Body))
			if text == "" {
				continue
			}
			if len(turns) > 0 && turns[len(turns)-1].role == "assistant" {
				turns[len(turns)-1].text += text
			} else {
				turns = append(turns, turn{"assistant", text})
			}
		}
	}
	if len(turns) > maxTurns {
		turns = turns[len(turns)-maxTurns:]
	}
	var b strings.Builder
	for _, t := range turns {
		fmt.Fprintf(&b, "%s: %s\n", t.role, t.text)
	}
	out := b.String()
	if len(out) > maxBytes {
		out = out[len(out)-maxBytes:]
	}
	return out
}

func extractMessageContent(body json.RawMessage) json.RawMessage {
	var d struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(body, &d) != nil {
		return nil
	}
	if len(d.Message.Content) > 0 {
		return d.Message.Content
	}
	return d.Content
}

// logHistorySource records which store served a history read so a
// regression that falls back to the archive while statedb has rows is
// visible in the decision log.
func logHistorySource(op string, src historySource, total, n int) {
	if src == historySourceChatlog {
		slog.Info("history read from chatlog archive",
			"op", op, "total", total, "lines", n,
			"note", "statedb empty — JSONL is import-once archive (🎯T613)")
		return
	}
	slog.Debug("history read from statedb", "op", op, "total", total, "lines", n)
}
