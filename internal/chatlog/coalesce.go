// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package chatlog

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/marcelocantos/jevons/internal/silentresponse"
)

// terminalStops match chat_events.js TERMINAL_STOPS — seal a turn.
var terminalStops = map[string]bool{
	"end_turn":      true,
	"stop_sequence": true,
	"max_tokens":    true,
}

// joinAssistantSegments inserts a blank line between two known segments when
// neither already has a line break at the boundary (🎯T161). Structural only —
// no markdown/content sniffing (no ``` / capital / list special cases).
func joinAssistantSegments(prev, next string) string {
	if prev == "" {
		return next
	}
	if next == "" {
		return prev
	}
	if strings.HasSuffix(prev, "\n") || strings.HasSuffix(prev, "\r") ||
		strings.HasPrefix(next, "\n") || strings.HasPrefix(next, "\r") {
		return prev + next
	}
	return prev + "\n\n" + next
}

// appendSegmentText appends text to acc: structural segment join when edge
// is set, otherwise bare concat (intra-segment tokens).
func appendSegmentText(acc *strings.Builder, text string, edge *bool) {
	if text == "" {
		return
	}
	if acc.Len() == 0 {
		acc.WriteString(text)
		*edge = false
		return
	}
	if *edge {
		joined := joinAssistantSegments(acc.String(), text)
		acc.Reset()
		acc.WriteString(joined)
		*edge = false
		return
	}
	acc.WriteString(text)
}

// applyAssistantContent walks message content in order (🎯T161):
// non-text blocks mark a segment edge for the following text; multiple text
// blocks in one message are segment edges between them; continuous single
// text deltas bare-concat.
func applyAssistantContent(acc *strings.Builder, content any, edge *bool) (wrote bool) {
	switch c := content.(type) {
	case string:
		if c == "" {
			return false
		}
		appendSegmentText(acc, c, edge)
		return true
	case []any:
		textParts := 0
		for _, raw := range c {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			if t != "" && t != "text" {
				if acc.Len() > 0 {
					*edge = true
				}
				continue
			}
			s, ok := m["text"].(string)
			if !ok || s == "" {
				continue
			}
			if textParts > 0 {
				*edge = true
			}
			appendSegmentText(acc, s, edge)
			textParts++
			wrote = true
		}
		return wrote
	default:
		return false
	}
}

// streamAcc is one logical assistant response during CoalesceStreamLines.
// 🎯T223: identity is stream_id when present; fragments join by id even if
// a user (or other) line is interleaved in the journal.
type streamAcc struct {
	key      string
	acc      strings.Builder
	edge     bool
	ts       string
	stop     string
	firstIdx int
	streamID string // wire stream_id when known (empty for legacy)
}

// CoalesceStreamLines merges assistant stream deltas into one sealed
// assistant frame per logical response (🎯T142 / 🎯T223).
//
// Join key is stream_id when present on the wire. Missing stream_id uses a
// single open legacy stream until a terminal stop — user / agent_note lines
// do NOT seal that stream (the T223 journal split: user mid-assistant was
// wrongly treating interleave as a new turn boundary).
//
// Sealed shape (stream_id preserved when known):
//
//	{"type":"assistant","stream_id":"…","message":{"role":"assistant","content":[{"type":"text","text":"..."}],"stop_reason":"end_turn"}}
//
// The durable log is never rewritten — this is a replay view only.
func CoalesceStreamLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}

	type head struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		StreamID  string `json:"stream_id"`
		Message   *struct {
			Role       string `json:"role"`
			StopReason string `json:"stop_reason"`
			Content    any   `json:"content"`
		} `json:"message"`
	}

	streams := make(map[string]*streamAcc)
	// legacyOpen is the key of the unlabeled open stream, or "".
	legacyOpen := ""
	legacyN := 0
	// lineIsAssistantFragment[i] = stream key if line i is absorbed into a stream.
	lineIsAssistantFragment := make([]string, len(lines))

	ensureStream := func(key, wireID string, idx int) *streamAcc {
		st, ok := streams[key]
		if ok {
			return st
		}
		st = &streamAcc{key: key, firstIdx: idx, streamID: wireID}
		streams[key] = st
		return st
	}

	markEdgeOpen := func() {
		for _, st := range streams {
			if st.acc.Len() > 0 && st.stop == "" {
				st.edge = true
			}
		}
	}

	for i, ln := range lines {
		var h head
		if err := json.Unmarshal([]byte(ln), &h); err != nil {
			// Broken JSON: seal any open streams at this boundary so later
			// lines do not silently glue across garbage.
			for _, st := range streams {
				if st.stop == "" {
					st.stop = "end_turn"
				}
			}
			legacyOpen = ""
			continue
		}

		if h.Type == "tool_result" || h.Type == "result" {
			// Protocol segment edge; keep streams open (one sealed bubble).
			markEdgeOpen()
			continue
		}

		if h.Type == "system" {
			// Full settle — seal every open stream (matches client shouldClearWorking).
			for _, st := range streams {
				if st.stop == "" {
					st.stop = "end_turn"
				}
			}
			legacyOpen = ""
			continue
		}

		if h.Type != "assistant" {
			// 🎯T223: user / agent_note / status / etc. do NOT seal open
			// assistant streams. Interleave is preserved; join is by id.
			continue
		}

		stop := ""
		if h.Message != nil {
			stop = h.Message.StopReason
		}
		wireID := strings.TrimSpace(h.StreamID)

		var key string
		if wireID != "" {
			key = "id:" + wireID
			ensureStream(key, wireID, i)
		} else {
			if legacyOpen == "" {
				legacyOpen = fmt.Sprintf("legacy-%d", legacyN)
				legacyN++
				ensureStream(legacyOpen, "", i)
			}
			key = legacyOpen
		}
		st := streams[key]
		lineIsAssistantFragment[i] = key

		if h.Message != nil {
			if applyAssistantContent(&st.acc, h.Message.Content, &st.edge) && h.Timestamp != "" {
				st.ts = h.Timestamp
			}
		}
		if terminalStops[stop] {
			st.stop = stop
			if key == legacyOpen {
				legacyOpen = ""
			}
		}
	}

	// EOF: seal any still-open streams.
	for _, st := range streams {
		if st.stop == "" {
			st.stop = "end_turn"
		}
	}

	// Pass 2: emit sealed body at each stream's first fragment index;
	// pass through non-absorbed lines; skip subsequent assistant fragments.
	out := make([]string, 0, len(lines)/8+8)
	emitted := make(map[string]bool, len(streams))

	flushStream := func(st *streamAcc) {
		if st == nil || emitted[st.key] {
			return
		}
		emitted[st.key] = true
		if st.acc.Len() == 0 && st.stop == "" {
			return
		}
		stop := st.stop
		if stop == "" {
			stop = "end_turn"
		}
		// 🎯T240: sealed turn whose concatenated text is silent — emit
		// empty body only (history/replay display path). Durable journal
		// is not rewritten; this is a view filter.
		body := st.acc.String()
		var content []any
		if silentresponse.Is(body) {
			content = []any{}
		} else if body != "" {
			content = []any{
				map[string]any{"type": "text", "text": body},
			}
		} else {
			content = []any{}
		}
		msg := map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"role":        "assistant",
				"content":     content,
				"stop_reason": stop,
			},
		}
		if st.ts != "" {
			msg["timestamp"] = st.ts
		}
		if st.streamID != "" {
			msg["stream_id"] = st.streamID
		}
		b, err := json.Marshal(msg)
		if err != nil {
			return
		}
		out = append(out, string(b))
	}

	// Index streams by firstIdx for O(1) emit.
	byFirst := make(map[int][]*streamAcc)
	for _, st := range streams {
		byFirst[st.firstIdx] = append(byFirst[st.firstIdx], st)
	}

	for i, ln := range lines {
		if sts := byFirst[i]; len(sts) > 0 {
			for _, st := range sts {
				flushStream(st)
			}
		}
		if key := lineIsAssistantFragment[i]; key != "" {
			// Absorbed into a sealed stream frame.
			continue
		}
		// Non-assistant (or unparseable) — pass through as-is.
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(ln), &probe); err != nil {
			out = append(out, ln)
			continue
		}
		out = append(out, ln)
	}
	// Any stream whose firstIdx was never visited (shouldn't happen).
	for _, st := range streams {
		flushStream(st)
	}
	return out
}

// ReplayTailSealed is ReplayTail with 🎯T142 sealed-turn coalescing on the
// window before streaming to fn. start/total still describe the raw journal
// (for /api/history paging); the number of fn invocations is the sealed count.
func (l *Log) ReplayTailSealed(maxTurns int, fn func(line string) error) (start, total int, err error) {
	lines, starts, err := l.snapshot()
	if err != nil {
		return 0, 0, err
	}
	total = len(lines)
	cut := 0
	if maxTurns > 0 && len(starts) > maxTurns {
		cut = starts[len(starts)-maxTurns]
	}
	sealed := CoalesceStreamLines(lines[cut:])
	for _, ln := range sealed {
		if err := fn(ln); err != nil {
			return cut, total, err
		}
	}
	return cut, total, nil
}
