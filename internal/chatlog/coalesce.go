// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package chatlog

import (
	"encoding/json"
	"strings"
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

// CoalesceStreamLines merges consecutive assistant stream deltas into one
// sealed assistant frame per turn (🎯T142). Non-assistant lines pass through
// unchanged. The durable log is never rewritten — this is a replay view.
//
// 🎯T161: after non-text assistant content (tool_use) or tool_result, the next
// text joins with joinAssistantSegments. Intra-segment token deltas bare-concat.
// No content-shaped fence heuristics (T147 removed).
//
// Sealed shape:
//
//	{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"..."}],"stop_reason":"end_turn"}}
func CoalesceStreamLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, 0, len(lines)/8+8)
	var acc strings.Builder
	var accTS string
	segmentEdgePending := false
	flush := func(stop string) {
		if acc.Len() == 0 && stop == "" {
			return
		}
		text := acc.String()
		acc.Reset()
		segmentEdgePending = false
		if stop == "" {
			stop = "end_turn"
		}
		msg := map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "text", "text": text},
				},
				"stop_reason": stop,
			},
		}
		if accTS != "" {
			msg["timestamp"] = accTS
		}
		accTS = ""
		b, err := json.Marshal(msg)
		if err != nil {
			return
		}
		out = append(out, string(b))
	}

	for _, ln := range lines {
		var head struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Message   *struct {
				Role       string `json:"role"`
				StopReason string `json:"stop_reason"`
				Content    any   `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ln), &head); err != nil {
			if acc.Len() > 0 {
				flush("end_turn")
			}
			out = append(out, ln)
			continue
		}
		if head.Type == "tool_result" || head.Type == "result" {
			// Protocol segment edge; keep stream open for one sealed bubble.
			if acc.Len() > 0 {
				segmentEdgePending = true
			}
			out = append(out, ln)
			continue
		}
		if head.Type != "assistant" {
			// Other non-assistant: seal any open assistant stream first.
			if acc.Len() > 0 {
				flush("end_turn")
			}
			out = append(out, ln)
			continue
		}
		stop := ""
		if head.Message != nil {
			stop = head.Message.StopReason
		}
		wrote := false
		if head.Message != nil {
			wrote = applyAssistantContent(&acc, head.Message.Content, &segmentEdgePending)
		}
		if wrote && head.Timestamp != "" {
			accTS = head.Timestamp
		}
		if terminalStops[stop] {
			// Empty end_turn still seals (even if no text accumulated).
			flush(stop)
		}
	}
	if acc.Len() > 0 {
		flush("end_turn")
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
