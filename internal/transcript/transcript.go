// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package transcript provides read, truncate, and fork operations on
// Grok Build session chat_history.jsonl files.
package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/marcelocantos/jevons/internal/discovery"
)

// Turn represents a single user→assistant exchange extracted from a transcript.
type Turn struct {
	Number int    `json:"turn_number"`
	Role   string `json:"role"` // "user" or "assistant"
	Text   string `json:"text"`
}

// Entry is a lightweight, chronological view of one transcript line,
// carrying the fields needed to derive a thread's live status. Unlike
// Turn it is not grouped into exchanges and it preserves the
// timestamp and assistant stop_reason.
type Entry struct {
	Type       string    // "user", "assistant", "system", …
	Role       string    // message.role when present
	Text       string    // extracted text (user string or assistant text blocks)
	StopReason string    // assistant message.stop_reason when present
	HasToolUse bool      // an assistant content block of type "tool_use"
	IsUserTurn bool      // a user message with extractable prompt text (turn boundary)
	Timestamp  time.Time // line timestamp when present
}

// Reader provides transcript operations backed by the Grok sessions tree.
type Reader struct {
	sessionsDir string // ~/.grok/sessions
}

// NewReader creates a Reader rooted at the given Grok sessions directory.
func NewReader(sessionsDir string) *Reader {
	return &Reader{sessionsDir: sessionsDir}
}

// Read parses a session transcript and returns turns grouped by user message boundaries.
// A turn boundary is a JSONL line with type "user" whose content yields non-empty
// prompt text (Grok top-level content string/text-blocks, or Claude nested message).
func (r *Reader) Read(sessionID string) ([]map[string]any, error) {
	path, err := r.findJSONL(sessionID)
	if err != nil {
		return nil, err
	}

	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}

	turns := extractTurns(lines)
	if len(turns) == 0 && hasTranscriptPayload(lines) {
		return nil, fmt.Errorf(
			"transcript for session %q has %d lines but no user turns (unrecognized format?)",
			sessionID, len(lines),
		)
	}

	result := make([]map[string]any, len(turns))
	for i, t := range turns {
		result[i] = map[string]any{
			"turn_number": t.Number,
			"role":        t.Role,
			"text":        t.Text,
		}
	}
	return result, nil
}

// Tail returns the last n transcript entries in chronological order,
// parsed richly enough for status derivation. If n <= 0 all entries are
// returned. Blank and unparseable lines are skipped.
func (r *Reader) Tail(sessionID string, n int) ([]Entry, error) {
	path, err := r.findJSONL(sessionID)
	if err != nil {
		return nil, err
	}
	entries, err := parseEntries(path)
	if err != nil {
		return nil, err
	}
	if n > 0 && len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	return entries, nil
}

// parseEntries reads a transcript file into chronological Entry values.
// Supports Grok top-level content and Claude nested message envelopes.
func parseEntries(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()

	var entries []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1MB line buffer

	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}

		var env lineEnvelope
		if err := json.Unmarshal([]byte(raw), &env); err != nil {
			continue // skip lines we cannot parse
		}

		e := Entry{Type: env.Type}
		if env.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339, env.Timestamp); err == nil {
				e.Timestamp = ts
			}
		}
		if env.Message != nil {
			e.Role = env.Message.Role
			e.StopReason = env.Message.StopReason
			e.fillFromContent(env.Type, env.Message.Content)
		} else if len(env.Content) > 0 {
			// Grok Build: content lives at the top level.
			e.fillFromContent(env.Type, env.Content)
			if e.Role == "" {
				switch env.Type {
				case "user":
					e.Role = "user"
				case "assistant":
					e.Role = "assistant"
				}
			}
		}
		// tool_result / reasoning / system lines carry Type only (v1).
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}

// fillFromContent sets Text/HasToolUse/IsUserTurn from a content payload
// (JSON string or array of typed blocks).
func (e *Entry) fillFromContent(typ string, content json.RawMessage) {
	text, hasToolUse, isUser := parseContentPayload(typ, content)
	e.Text = text
	if hasToolUse {
		e.HasToolUse = true
	}
	if isUser {
		e.IsUserTurn = true
	}
}

// parseContent is kept as a thin alias for older call sites / clarity in docs.
func (e *Entry) parseContent(typ string, content json.RawMessage) {
	e.fillFromContent(typ, content)
}

// Truncate rewrites a session transcript to keep only the first keepTurns
// user→assistant exchanges. Lines before the first user turn (metadata,
// snapshots) are always preserved.
func (r *Reader) Truncate(sessionID string, keepTurns int) error {
	path, err := r.findJSONL(sessionID)
	if err != nil {
		return err
	}

	lines, err := readLines(path)
	if err != nil {
		return err
	}

	kept := truncateLines(lines, keepTurns)
	return writeLines(path, kept)
}

// Fork creates a copy of a session transcript truncated to keepTurns,
// returning the new session UUID. The original transcript is untouched.
func (r *Reader) Fork(sessionID string, keepTurns int) (string, error) {
	path, err := r.findJSONL(sessionID)
	if err != nil {
		return "", err
	}

	lines, err := readLines(path)
	if err != nil {
		return "", err
	}

	kept := truncateLines(lines, keepTurns)

	newUUID := uuid.New().String()
	dir := filepath.Dir(path)
	newPath := filepath.Join(dir, newUUID+".jsonl")

	if err := writeLines(newPath, kept); err != nil {
		return "", err
	}

	slog.Info("forked transcript", "from", sessionID, "to", newUUID, "turns", keepTurns)
	return newUUID, nil
}

// findJSONL locates chat_history.jsonl for a Grok session id.
func (r *Reader) findJSONL(sessionID string) (string, error) {
	if !discovery.IsSessionID(sessionID) {
		return "", fmt.Errorf("invalid session ID: %q", sessionID)
	}
	path := discovery.ChatHistoryPath(r.sessionsDir, sessionID)
	if path == "" {
		return "", fmt.Errorf("transcript not found for session %q", sessionID)
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("transcript not found for session %q: %w", sessionID, err)
	}
	return path, nil
}

// lineEnvelope covers both Grok (top-level content) and Claude (nested message).
type lineEnvelope struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	// Content is Grok Build top-level payload (string or text blocks).
	Content json.RawMessage `json:"content"`
	Message *struct {
		Role       string          `json:"role"`
		StopReason string          `json:"stop_reason"`
		Content    json.RawMessage `json:"content"`
	} `json:"message"`
}

// jsonlLine holds the raw bytes and parsed type/role for a single JSONL line.
type jsonlLine struct {
	raw  string
	typ  string // "user", "assistant", "progress", "file-history-snapshot", etc.
	role string // from message.role if present
	// isUserTurn is true for type="user" lines with extractable prompt text
	// (Grok top-level content or Claude nested string content).
	isUserTurn bool
}

// readLines reads and parses all JSONL lines from a transcript file.
func readLines(path string) ([]jsonlLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()

	var lines []jsonlLine
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1MB line buffer

	for scanner.Scan() {
		raw := scanner.Text()
		if strings.TrimSpace(raw) == "" {
			continue
		}

		var envelope lineEnvelope
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			// Keep unparseable lines as-is.
			lines = append(lines, jsonlLine{raw: raw})
			continue
		}

		line := jsonlLine{
			raw: raw,
			typ: envelope.Type,
		}

		// Prefer Claude nested message when present; else Grok top-level content.
		var content json.RawMessage
		if envelope.Message != nil {
			line.role = envelope.Message.Role
			content = envelope.Message.Content
		} else {
			content = envelope.Content
		}

		if envelope.Type == "user" {
			if _, _, isUser := parseContentPayload("user", content); isUser {
				line.isUserTurn = true
			}
		}

		lines = append(lines, line)
	}
	return lines, scanner.Err()
}

// hasTranscriptPayload reports whether the file looks like a real chat
// (not just empty/metadata), used to distinguish calm-empty from parser miss.
func hasTranscriptPayload(lines []jsonlLine) bool {
	for _, l := range lines {
		switch l.typ {
		case "user", "assistant", "tool_result", "reasoning", "tool_use":
			return true
		}
	}
	return false
}

// extractTurns groups JSONL lines into user→assistant turns.
// tool_result / reasoning lines are ignored for turn text (v1).
func extractTurns(lines []jsonlLine) []Turn {
	var turns []Turn
	turnNum := 0
	var userText, assistantText string
	inTurn := false

	for _, l := range lines {
		if l.isUserTurn {
			// Flush previous turn.
			if inTurn {
				turns = append(turns, Turn{Number: turnNum, Role: "user", Text: userText})
				if assistantText != "" {
					turns = append(turns, Turn{Number: turnNum, Role: "assistant", Text: assistantText})
				}
			}
			turnNum++
			inTurn = true
			userText = extractText(l.raw)
			assistantText = ""
		} else if inTurn && l.typ == "assistant" {
			text := extractAssistantText(l.raw)
			if text != "" {
				if assistantText != "" {
					assistantText += "\n"
				}
				assistantText += text
			}
		}
	}

	// Flush last turn.
	if inTurn {
		turns = append(turns, Turn{Number: turnNum, Role: "user", Text: userText})
		if assistantText != "" {
			turns = append(turns, Turn{Number: turnNum, Role: "assistant", Text: assistantText})
		}
	}

	return turns
}

// parseContentPayload extracts display text from a content field that is either
// a JSON string or an array of typed blocks (text / tool_use / tool_result).
// isUserTurn is true only for type "user" with non-empty extractable prompt text
// (plain string, or text blocks — not pure tool_result arrays).
func parseContentPayload(typ string, content json.RawMessage) (text string, hasToolUse bool, isUserTurn bool) {
	if len(content) == 0 {
		return "", false, false
	}
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" || trimmed == "null" {
		return "", false, false
	}

	// Plain JSON string.
	if trimmed[0] == '"' {
		var s string
		if json.Unmarshal(content, &s) != nil {
			return "", false, false
		}
		s = strings.TrimSpace(s)
		if typ == "user" && s != "" {
			return s, false, true
		}
		return s, false, false
	}

	// Array of content blocks.
	if trimmed[0] != '[' {
		return "", false, false
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return "", false, false
	}
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case "tool_use":
			hasToolUse = true
		}
		// tool_result blocks intentionally ignored for turn text (v1).
	}
	text = strings.Join(parts, "\n")
	if typ == "user" && strings.TrimSpace(text) != "" {
		isUserTurn = true
	}
	return text, hasToolUse, isUserTurn
}

// extractText pulls the user's prompt text from a user-turn JSONL line
// (Grok top-level or Claude nested).
func extractText(raw string) string {
	var envelope lineEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return ""
	}
	content := envelope.Content
	if envelope.Message != nil && len(envelope.Message.Content) > 0 {
		content = envelope.Message.Content
	}
	text, _, _ := parseContentPayload("user", content)
	return text
}

// extractAssistantText pulls assistant display text from a JSONL line.
// Grok: top-level content string (or text blocks). Claude: message.content blocks.
// tool_calls alone with empty content yield "".
func extractAssistantText(raw string) string {
	var envelope lineEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return ""
	}
	content := envelope.Content
	if envelope.Message != nil && len(envelope.Message.Content) > 0 {
		content = envelope.Message.Content
	}
	text, _, _ := parseContentPayload("assistant", content)
	return text
}

// truncateLines keeps all lines up to and including the Nth user turn,
// plus all subsequent non-user-turn lines (assistant responses, tool results,
// progress) until the next user turn. Metadata lines before the first user
// turn are always preserved.
func truncateLines(lines []jsonlLine, keepTurns int) []string {
	var kept []string
	turnsSeen := 0

	for _, l := range lines {
		if l.isUserTurn {
			turnsSeen++
			if turnsSeen > keepTurns {
				break
			}
		}
		kept = append(kept, l.raw)
	}

	return kept
}

// writeLines writes raw JSONL lines to a file, one per line.
func writeLines(path string, lines []string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create transcript: %w", err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, line := range lines {
		if _, err := w.WriteString(line); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
	}
	return w.Flush()
}
