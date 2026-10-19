// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package userturn identifies owner boundaries in canonical conversation events.
package userturn

import (
	"encoding/json"
	"strings"
)

// IsOwnerFrame gives explicit provenance precedence over legacy text markers.
// An owner can quote a fleet report; an agent's ordinary prose is still an
// injection. Text inference applies only to unmarked historical records.
// The caller must establish the user event type (including the separate type
// column of older SQLite rows whose JSON payload has no type or role).
func IsOwnerFrame(body []byte) bool {
	var frame struct {
		Type    string          `json:"type"`
		Origin  string          `json:"turn_origin"`
		Text    string          `json:"text"`
		Content json.RawMessage `json:"content"`
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(body, &frame) != nil {
		return false
	}
	if frame.Origin != "" {
		return frame.Origin == "owner"
	}
	content := frame.Message.Content
	if len(content) == 0 {
		content = frame.Content
	}
	text := frame.Text
	if len(content) > 0 {
		if json.Unmarshal(content, &text) != nil {
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(content, &blocks) != nil {
				return false
			}
			text = ""
			for _, block := range blocks {
				if block.Type == "text" {
					text += block.Text
				}
			}
		}
	}
	// T362: unmarked protocol-control records are not owner requests. Keep
	// this additional mux rule out of the older transcript reader's classifier.
	var control struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(text), &control) == nil && strings.TrimSpace(control.Type) != "" {
		return false
	}
	return strings.TrimSpace(text) != "" && !LegacyInjectionText(text)
}

// LegacyInjectionText retains the historical transcript reader's T329 rule.
// Canonical events must use IsOwnerFrame so explicit origin is not inferred.
func LegacyInjectionText(text string) bool {
	display := unwrapUserQuery(strings.TrimSpace(text))
	if display == "" {
		return false
	}
	// A declared harness fact is not an owner boundary (🎯T329, 🎯T766.4).
	// The line is "open-intent harness <kind>". A rendered banner is owner text.
	fields := strings.Fields(display)
	if len(fields) != 3 || fields[0] != "open-intent" || fields[1] != "harness" {
		return false
	}
	switch fields[2] {
	case "daemon-restart", "standing-brief", "event", "system-reminder", "background-task":
		return true
	default:
		return false
	}
}

// unwrapUserQuery drops one outer <user_query>…</user_query> wrapper.
// The comparison is the tag, not a sentence.
func unwrapUserQuery(display string) string {
	const open = "<user_query"
	const closeTag = "</user_query>"
	if len(display) < len(open) || display[:len(open)] != open {
		return display
	}
	i := strings.Index(display, ">")
	if i < 0 {
		return display
	}
	inner := display[i+1:]
	j := strings.LastIndex(inner, closeTag)
	if j < 0 {
		return display
	}
	return strings.TrimSpace(inner[:j])
}
