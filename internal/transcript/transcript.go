// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package transcript provides read, truncate, and fork operations on
// provider session transcripts (Grok chat_history.jsonl and Claude
// session JSONL under ~/.claude/projects — 🎯T213).
//
// 🎯T422 — IT DOES NOT DECODE THOSE FILES ITSELF. Every line goes through
// turnev.Decode, the one decoder, because this package and the send
// confirmation were reading the same sessions and disagreeing about them: the
// confirmation reported a payload absent that the receiver was already working
// from, and this package reported "105 lines but no user turns (unrecognized
// format?)" for a live 286KB conversation. One cause under both — a message
// accepted behind a live turn is replayed as a queued_command ATTACHMENT and
// never becomes a user message, so a reader that knows only about user
// messages is looking in the one place the payload structurally cannot be.
//
// What this package still owns is RENDERING: grouping decoded records into
// user→assistant turns, the 🎯T329 inject rule, truncate and fork. What it no
// longer owns is what a line means.
package transcript

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/turnev"
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

// Reader provides transcript operations over multi-provider session roots.
type Reader struct {
	roots discovery.Roots
}

// NewReader creates a Grok-only Reader. sessionsDir is typically
// ~/.grok/sessions. Prefer NewReaderRoots when Claude discovery is needed.
func NewReader(sessionsDir string) *Reader {
	return NewReaderRoots(discovery.Roots{GrokSessions: sessionsDir})
}

// NewReaderRoots creates a Reader over multi-provider roots (🎯T213).
func NewReaderRoots(r discovery.Roots) *Reader {
	return &Reader{roots: r}
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

	// 🎯T422 clause 7: a session with content is never reported empty. The
	// strict pass is the 🎯T329 owner-chat shape; when it finds nothing the
	// injects are rendered, and when there are no prompts to render at all the
	// replies are. Only a file whose lines this decoder genuinely cannot
	// interpret produces an error, and that error names the shapes it saw.
	turns := extractTurns(lines, false)
	if len(turns) == 0 {
		turns = extractTurns(lines, true)
	}
	if len(turns) == 0 {
		turns = assistantOnlyTurns(lines)
	}
	if len(turns) == 0 && hasTranscriptPayload(lines) {
		return nil, fmt.Errorf(
			"transcript for session %q has %d lines and no readable turns — %s",
			sessionID, len(lines), describeLines(lines),
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
		rec, ok := turnev.Decode([]byte(raw))
		if !ok {
			continue // skip lines we cannot parse
		}
		entries = append(entries, entryFrom(rec))
	}
	return entries, scanner.Err()
}

// entryFrom projects a decoded record onto the status view. The projection is
// all this function does — nothing here re-reads the JSON, which is the point
// of 🎯T422: the phase/idle derivation and the send confirmation now disagree
// about a session only if turnev.Decode disagrees with itself.
func entryFrom(rec turnev.Record) Entry {
	return Entry{
		Type:       rec.Type,
		Role:       rec.Role,
		Text:       rec.Text,
		StopReason: rec.StopReason,
		HasToolUse: rec.HasToolUse,
		IsUserTurn: isTurnBoundary(rec),
		Timestamp:  rec.Timestamp,
	}
}

// isTurnBoundary reports whether a record is a prompt the agent received.
//
// A queued_command attachment counts, and that is the 🎯T422 repair: it is what
// a message accepted behind a live turn BECOMES when the queue drains it into
// that turn. Treating only user messages as prompts is what let a busy agent
// read as having received nothing — of 33 queued payloads measured on
// 2026-08-10, 32 arrived this way and none as a user message.
//
// A tool_result never counts however exactly its bytes match the payload: an
// agent capturing its own pane quotes an unsubmitted message into its own
// transcript (clause 3). turnev.Decode has already made that distinction; this
// function only reads it.
func isTurnBoundary(rec turnev.Record) bool {
	if rec.IsSidechain {
		return false
	}
	switch rec.Kind {
	case turnev.KindUserMessage, turnev.KindQueuedCommand:
		return strings.TrimSpace(rec.Text) != ""
	}
	return false
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

// findJSONL locates the transcript JSONL for a session id across Grok and
// Claude roots (🎯T213). Preference: Grok chat_history, else Claude session file.
func (r *Reader) findJSONL(sessionID string) (string, error) {
	if !discovery.IsSessionID(sessionID) {
		return "", fmt.Errorf("invalid session ID: %q", sessionID)
	}
	path := discovery.TranscriptPath(r.roots, sessionID)
	if path == "" {
		return "", fmt.Errorf("transcript not found for session %q", sessionID)
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("transcript not found for session %q: %w", sessionID, err)
	}
	return path, nil
}

// jsonlLine holds the raw bytes and the decoded record for a single JSONL
// line. The record comes from turnev.Decode and nothing here re-reads the
// JSON (🎯T422).
type jsonlLine struct {
	raw string
	rec turnev.Record
	// decoded is false for a line that is not JSON at all. Such lines are
	// preserved verbatim through truncate/fork and COUNTED by Read, which
	// names them rather than quietly shrinking the conversation.
	decoded bool
	// isUserTurn is true for a prompt the agent received — an authored user
	// message, or a queued_command attachment the queue drained into a turn.
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

		rec, ok := turnev.Decode([]byte(raw))
		if !ok {
			// Keep unparseable lines as-is: truncate/fork must not drop them,
			// and Read counts them so an unreadable session says so.
			lines = append(lines, jsonlLine{raw: raw})
			continue
		}
		lines = append(lines, jsonlLine{
			raw:        raw,
			rec:        rec,
			decoded:    true,
			isUserTurn: isTurnBoundary(rec),
		})
	}
	return lines, scanner.Err()
}

// hasTranscriptPayload reports whether the file looks like a real chat
// (not just empty/metadata), used to distinguish calm-empty from parser miss.
func hasTranscriptPayload(lines []jsonlLine) bool {
	for _, l := range lines {
		if !l.decoded {
			// A line that is not JSON at all is still content, and a reader
			// that skips it silently reports an empty conversation for a file
			// full of text it declined to read (🎯T422 clause 7).
			return true
		}
		if l.rec.Kind != turnev.KindOther {
			return true
		}
		switch l.rec.Type {
		case "user", "assistant", "tool_result", "reasoning", "tool_use":
			return true
		}
	}
	return false
}

// describeLines names what a file held, for the one error this reader is still
// allowed to return. "Unrecognized format?" was a guess offered to an operator
// with nothing to act on; a census of the record kinds and the count of lines
// that were not JSON at all is the difference between a defect report and a
// shrug (🎯T422 clause 7).
func describeLines(lines []jsonlLine) string {
	counts := map[string]int{}
	unparseable := 0
	for _, l := range lines {
		if !l.decoded {
			unparseable++
			continue
		}
		key := l.rec.Kind.String()
		if l.rec.Kind == turnev.KindOther && l.rec.Type != "" {
			key = "other:" + l.rec.Type
		}
		counts[key]++
	}
	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	parts := make([]string, 0, len(kinds)+1)
	for _, k := range kinds {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	if unparseable > 0 {
		parts = append(parts, fmt.Sprintf("not-json=%d", unparseable))
	}
	if len(parts) == 0 {
		return "the file held no records"
	}
	return "records seen: " + strings.Join(parts, " ")
}

// isNonBoundaryUserText reports harness/fleet inject user bodies that must
// not open a new owner turn (🎯T329). Mirrors ChatEvents.isNonBoundaryUserText
// and AgentTranscript.classifyInspectUserLine inject kinds so sealed RHS
// history matches live coalesce (one assistant bubble per real owner turn).
func isNonBoundaryUserText(text string) bool {
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

// extractTurns groups JSONL lines into user→assistant turns.
// tool_result / reasoning lines are ignored for turn text (v1).
// 🎯T329: system-reminder / standing-brief / background-task user lines are
// non-boundaries — they do not flush the open turn or mint a new assistant row.
//
// keepInject disables that rule. It is the second pass of 🎯T422 clause 7 and
// never the first: for the owner's own chat the T329 rule is what keeps one
// assistant bubble per real owner turn, but for a fleet worker EVERY prompt is
// brief-prefixed, so applying it to such a session filters the conversation
// down to nothing and the reader then reports the file unreadable. Rendering
// the injects is worse than the T329 shape and far better than rendering
// nothing; which one a session gets is decided by whether the strict pass
// found anything, not by guessing whose session it is.
func extractTurns(lines []jsonlLine, keepInject bool) []Turn {
	var turns []Turn
	turnNum := 0
	var userText, assistantText string
	inTurn := false

	for _, l := range lines {
		if !l.decoded {
			continue
		}
		if l.isUserTurn {
			text := l.rec.Text
			// 🎯T329: harness inject is not an owner turn boundary.
			if !keepInject && isNonBoundaryUserText(text) {
				continue
			}
			// Flush previous turn.
			if inTurn {
				turns = append(turns, Turn{Number: turnNum, Role: "user", Text: userText})
				if assistantText != "" {
					turns = append(turns, Turn{Number: turnNum, Role: "assistant", Text: assistantText})
				}
			}
			turnNum++
			inTurn = true
			userText = text
			assistantText = ""
		} else if inTurn && l.rec.Kind == turnev.KindAssistant {
			if text := l.rec.Text; text != "" {
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

// assistantOnlyTurns renders a session that has replies but no prompt this
// reader can see — a resumed seat whose opening turns predate the file, or a
// shape this decoder does not yet know. It is the last step before giving up,
// and it exists because "I could not find the prompts" and "there is nothing
// here" are different answers and only one of them is true (🎯T422 clause 7).
func assistantOnlyTurns(lines []jsonlLine) []Turn {
	var turns []Turn
	for _, l := range lines {
		if !l.decoded || l.rec.Kind != turnev.KindAssistant {
			continue
		}
		if text := strings.TrimSpace(l.rec.Text); text != "" {
			turns = append(turns, Turn{Number: len(turns) + 1, Role: "assistant", Text: l.rec.Text})
		}
	}
	return turns
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
