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

// CoalesceStreamLines merges consecutive assistant stream deltas into one
// sealed assistant frame per turn (🎯T142). Non-assistant lines pass through
// unchanged. The durable log is never rewritten — this is a replay view.
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
	flush := func(stop string) {
		if acc.Len() == 0 && stop == "" {
			return
		}
		text := acc.String()
		acc.Reset()
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
		if err := json.Unmarshal([]byte(ln), &head); err != nil || head.Type != "assistant" {
			// Non-assistant: seal any open assistant stream first.
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
		text := assistantTextFromContent(nil)
		if head.Message != nil {
			text = assistantTextFromContent(head.Message.Content)
		}
		if text != "" {
			if acc.Len() > 0 {
				// 🎯T147-style fence edge: insert blank line before ``` if needed.
				prev := acc.String()
				if !strings.HasSuffix(prev, "\n") && strings.HasPrefix(text, "```") {
					acc.WriteString("\n\n")
				}
			}
			acc.WriteString(text)
			if head.Timestamp != "" {
				accTS = head.Timestamp
			}
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

func assistantTextFromContent(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, raw := range c {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := m["type"].(string); t != "text" && t != "" {
				// allow missing type if text present
				if _, has := m["text"]; !has {
					continue
				}
			}
			if s, ok := m["text"].(string); ok && s != "" {
				b.WriteString(s)
			}
		}
		return b.String()
	default:
		return ""
	}
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
