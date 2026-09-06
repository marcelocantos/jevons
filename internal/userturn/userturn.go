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
	raw := strings.TrimSpace(text)
	if raw == "" {
		return false
	}
	display := raw
	// Unwrap a single outer <user_query>…</user_query> for inject checks.
	if strings.HasPrefix(display, "<user_query") {
		if i := strings.Index(display, ">"); i >= 0 {
			inner := display[i+1:]
			if j := strings.LastIndex(inner, "</user_query>"); j >= 0 {
				display = strings.TrimSpace(inner[:j])
			}
		}
	}
	low := strings.ToLower(display)
	if strings.Contains(low, "<system-reminder") || strings.Contains(low, "system-reminder") {
		return true
	}
	if strings.HasPrefix(display, "[Jevons fleet standing brief") ||
		strings.Contains(display, "Jevons fleet standing brief") {
		return true
	}
	if strings.HasPrefix(display, "[event:") || strings.HasPrefix(strings.ToLower(display), "[event:") {
		return true
	}
	if strings.HasPrefix(display, "[Daemon restart") {
		return true
	}
	if strings.HasPrefix(strings.ToLower(display), "background task") {
		return true
	}
	if strings.Contains(low, "background task") && strings.Contains(low, "completed") {
		return true
	}
	return false
}
