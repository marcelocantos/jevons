// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package statedb

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AssistantProse extracts painted assistant text from a stored event body.
// Same rule as ui displayRows (🎯T569): a content block with a text field
// counts as prose unless it is tool_use / tool_result — daily statedb rows
// often omit `"type":"text"`.
func AssistantProse(typ, body string) string {
	if typ != "assistant" {
		return ""
	}
	var raw struct {
		Text    string          `json:"text"`
		Content json.RawMessage `json:"content"`
		Message *struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return strings.TrimSpace(body)
	}
	content := raw.Content
	if raw.Message != nil && len(raw.Message.Content) > 0 {
		content = raw.Message.Content
	}
	if text := contentProse(content); text != "" {
		return text
	}
	return strings.TrimSpace(raw.Text)
}

func contentProse(content json.RawMessage) string {
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	if trimmed[0] == '"' {
		var s string
		if json.Unmarshal(content, &s) != nil {
			return ""
		}
		return strings.TrimSpace(s)
	}
	if trimmed[0] != '[' {
		return ""
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "tool_use" || b.Type == "tool_result" {
			continue
		}
		if t := strings.TrimSpace(b.Text); t != "" {
			if b.Type == "" || b.Type == "text" || b.Type == "output_text" {
				parts = append(parts, t)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, ""))
}

// PruneDuplicateAssistantProse drops later assistant rows whose extracted
// prose exactly matches an earlier one. User / tool / other rows stay.
// Indexes are not rewritten — holes are allowed (Count < N).
func (s *Store) PruneDuplicateAssistantProse(agent string) (int, error) {
	if s == nil || strings.TrimSpace(agent) == "" {
		return 0, nil
	}
	rows, err := s.Range(agent, 1, 1<<30)
	if err != nil {
		return 0, err
	}
	seen := map[string]int{}
	var drop []int
	for _, ev := range rows {
		if ev.Type != "assistant" {
			continue
		}
		text := AssistantProse(ev.Type, ev.Body)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			drop = append(drop, ev.Index)
			continue
		}
		seen[text] = ev.Index
	}
	if len(drop) == 0 {
		return 0, nil
	}
	if err := s.deleteIndexes(agent, drop); err != nil {
		return 0, err
	}
	return len(drop), nil
}

func (s *Store) deleteIndexes(agent string, idxs []int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`DELETE FROM transcript_events WHERE agent = ? AND idx = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, idx := range idxs {
		if _, err := stmt.Exec(agent, idx); err != nil {
			return fmt.Errorf("statedb: delete idx=%d: %w", idx, err)
		}
	}
	return tx.Commit()
}
