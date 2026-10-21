// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/turnev"
)

// DefaultChatLookback is how many recent owner-chat user turns to sample.
const DefaultChatLookback = 80

// DefaultSessionLookback is how many recent session transcript files to scan.
const DefaultSessionLookback = 12

// DefaultSessionTurnCap is max user-ish turns loaded per session file.
const DefaultSessionTurnCap = 40

// ChatTurn is one owner-chat or session-transcript turn (pure input for deeper extract).
// Source labels:
//   - owner_chat: jevons chatlog JSONL (durable owner conversation)
//   - session:    provider/session chat_history.jsonl (mnemo-indexed surface)
type ChatTurn struct {
	// Role is user, assistant, error, or other.
	Role string
	// Text is plain-text content (tool blocks stripped to text when present).
	Text string
	// Source is owner_chat or session.
	Source string
	// SourceID is chatlog basename, session id, or path fragment.
	SourceID string
	// TS is observation time when known.
	TS time.Time
}

// ChatTurnsToEvidence maps owner-chat / session turns into actionable Evidence.
// Only user (and explicit error) turns with friction signals become evidence;
// assistants alone never mint (avoids model-chatter flooding).
//
// Kinds:
//   - chat_gap: owner_chat friction (product pain in owner messages)
//   - transcript_friction: session transcript friction (agent-run / mnemo surface)
func ChatTurnsToEvidence(turns []ChatTurn) []Evidence {
	out := make([]Evidence, 0, len(turns)/4)
	for _, t := range turns {
		role := strings.ToLower(strings.TrimSpace(t.Role))
		text := strings.TrimSpace(t.Text)
		if text == "" {
			continue
		}
		// Error-role lines always count; user turns only with friction.
		if role != "error" && role != "user" {
			continue
		}
		sig, phrase := frictionSignal(text)
		if !sig && role != "error" {
			continue
		}
		kind := "chat_gap"
		comp := "owner_chat"
		dec := "friction"
		if strings.ToLower(t.Source) == "session" {
			kind = "transcript_friction"
			comp = "session"
			dec = "transcript"
		}
		if role == "error" {
			if kind == "chat_gap" {
				dec = "chat_error"
			} else {
				dec = "session_error"
			}
			if !sig {
				phrase = "error"
			}
		}
		msg := "owner friction: " + phrase
		if kind == "transcript_friction" {
			msg = "session friction: " + phrase
		}
		// Stable cluster message: phrase only (not full free-text).
		out = append(out, Evidence{
			Kind:      kind,
			Component: comp,
			Decision:  dec,
			Outcome:   "error",
			Message:   msg,
			SourceID:  t.SourceID,
			TS:        t.TS,
			Fields: map[string]string{
				"phrase": phrase,
				"source": firstNonEmpty(t.Source, "owner_chat"),
			},
		})
	}
	return out
}

// LoadChatLogTurns reads a jevons chatlog JSONL and returns recent user/error turns.
// maxTurns ≤0 uses DefaultChatLookback. Missing file → empty, nil error.
func LoadChatLogTurns(path string, maxTurns int) ([]ChatTurn, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	if maxTurns <= 0 {
		maxTurns = DefaultChatLookback
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var all []ChatTurn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		turn, ok := parseChatlogLine(line, path)
		if !ok {
			continue
		}
		all = append(all, turn)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(all) > maxTurns {
		all = all[len(all)-maxTurns:]
	}
	return all, nil
}

// LoadSessionTranscriptTurns reads one session chat_history.jsonl (Grok/Claude shapes).
// maxTurns ≤0 uses DefaultSessionTurnCap. Missing file → empty, nil error.
func LoadSessionTranscriptTurns(path, sessionID string, maxTurns int) ([]ChatTurn, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	if maxTurns <= 0 {
		maxTurns = DefaultSessionTurnCap
	}
	if sessionID == "" {
		sessionID = filepath.Base(filepath.Dir(path))
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var all []ChatTurn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		turn, ok := parseSessionLine(line, sessionID)
		if !ok {
			continue
		}
		all = append(all, turn)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(all) > maxTurns {
		all = all[len(all)-maxTurns:]
	}
	return all, nil
}

// LoadRecentSessionTurns walks sessionsDir for chat_history.jsonl files (newest first)
// and returns user/error turns. maxSessions ≤0 uses DefaultSessionLookback.
// Best-effort: unreadable trees yield empty + nil (ambient must not fail the cycle).
func LoadRecentSessionTurns(sessionsDir string, maxSessions, maxTurnsPerSession int) ([]ChatTurn, error) {
	sessionsDir = strings.TrimSpace(sessionsDir)
	if sessionsDir == "" {
		return nil, nil
	}
	if maxSessions <= 0 {
		maxSessions = DefaultSessionLookback
	}
	if maxTurnsPerSession <= 0 {
		maxTurnsPerSession = DefaultSessionTurnCap
	}
	st, err := os.Stat(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !st.IsDir() {
		return nil, nil
	}

	type fileHit struct {
		path string
		mod  time.Time
		sid  string
	}
	var hits []fileHit
	_ = filepath.WalkDir(sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if d.Name() != "chat_history.jsonl" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		sid := filepath.Base(filepath.Dir(path))
		hits = append(hits, fileHit{path: path, mod: info.ModTime(), sid: sid})
		return nil
	})
	// Newest first.
	for i := 0; i < len(hits); i++ {
		for j := i + 1; j < len(hits); j++ {
			if hits[j].mod.After(hits[i].mod) {
				hits[i], hits[j] = hits[j], hits[i]
			}
		}
	}
	if len(hits) > maxSessions {
		hits = hits[:maxSessions]
	}
	var out []ChatTurn
	for _, h := range hits {
		turns, err := LoadSessionTranscriptTurns(h.path, h.sid, maxTurnsPerSession)
		if err != nil {
			continue
		}
		out = append(out, turns...)
	}
	return out, nil
}

func parseChatlogLine(line, path string) (ChatTurn, bool) {
	var d struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		Message   struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(line), &d) != nil {
		return ChatTurn{}, false
	}
	typ := strings.ToLower(strings.TrimSpace(d.Type))
	if typ != "user" && typ != "error" {
		return ChatTurn{}, false
	}
	text := extractContentText(d.Message.Content)
	if text == "" {
		return ChatTurn{}, false
	}
	// Skip daemon/system injects that look like owner text but are harness noise.
	if isHarnessInject(text) {
		return ChatTurn{}, false
	}
	role := typ
	if d.Message.Role != "" {
		role = d.Message.Role
	}
	var ts time.Time
	if d.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339Nano, d.Timestamp); err == nil {
			ts = t
		} else if t, err := time.Parse(time.RFC3339, d.Timestamp); err == nil {
			ts = t
		}
	}
	return ChatTurn{
		Role:     role,
		Text:     text,
		Source:   "owner_chat",
		SourceID: filepath.Base(path),
		TS:       ts,
	}, true
}

// parseSessionLine reads one provider session-JSONL line into a coach turn.
//
// 🎯T422 clause 1: the line's SHAPE is turnev's answer, not this file's. This
// was the coach's own copy of the session decoder — a top-level content for
// Grok, a nested message for Claude, type=error for a harness notice — and a
// second reader of a file is a second answer waiting to disagree with the
// first. What it got wrong is what every hand-rolled copy got wrong: a prompt
// accepted behind a live turn is replayed as a queued_command ATTACHMENT and
// never becomes a user message, so the coach reading a BUSY agent's session
// saw none of the prompts it was actually given. Of 33 queued payloads
// measured on 2026-08-10, 32 arrived that way and none as a user record.
//
// What stays here is the coach's own policy, which is not about the file:
// which roles are friction-bearing, the harness-inject filter, and the length
// cap that keeps a skill dump from being read as owner pain.
func parseSessionLine(line, sessionID string) (ChatTurn, bool) {
	rec, ok := turnev.Decode([]byte(line))
	if !ok {
		return ChatTurn{}, false
	}
	var role string
	switch rec.Kind {
	case turnev.KindUserMessage, turnev.KindQueuedCommand:
		// A queued command is a prompt the agent received; the CLI just wrote
		// it as an attachment rather than as a user record.
		role = "user"
	case turnev.KindError:
		role = "error"
	default:
		// A tool_result is not authored (clause 3), an assistant turn is not
		// friction, and queue bookkeeping is not a prompt.
		return ChatTurn{}, false
	}
	text := strings.TrimSpace(rec.Text)
	if text == "" {
		return ChatTurn{}, false
	}
	// Session system-reminder / huge skill dumps are not owner friction.
	if isHarnessInject(text) || len(text) > 4000 {
		return ChatTurn{}, false
	}
	return ChatTurn{
		Role:     role,
		Text:     text,
		Source:   "session",
		SourceID: sessionID,
		TS:       rec.Timestamp,
	}, true
}

func extractContentText(raw json.RawMessage) string {
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
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Text != "" {
				b.WriteString(p.Text)
			}
		}
		return strings.TrimSpace(b.String())
	}
	return ""
}

func isHarnessInject(text string) bool {
	// A declared harness fact is not owner friction (🎯T766.4).
	// The line is "open-intent harness <kind>". A rendered banner is owner text.
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 3 || fields[0] != "open-intent" || fields[1] != "harness" {
		return false
	}
	switch fields[2] {
	case "daemon-restart", "standing-brief", "system-reminder", "agent-responded", "subagent", "verifier":
		return true
	default:
		return false
	}
}

// frictionSignal reports a declared friction fact and the stable phrase
// that fact clusters under. The line is "open-intent friction <token>".
// Owner prose is not a signal (🎯T766.4).
func frictionSignal(text string) (bool, string) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 3 || fields[0] != "open-intent" || fields[1] != "friction" {
		return false, ""
	}
	switch fields[2] {
	case "still-not-working":
		return true, "still not working"
	case "still-not-fixed":
		return true, "still not fixed"
	case "still-broken":
		return true, "still broken"
	case "doesnt-work":
		return true, "doesnt work"
	case "does-not-work":
		return true, "does not work"
	case "not-working":
		return true, "not working"
	case "broken-again":
		return true, "broken again"
	case "keeps-failing":
		return true, "keeps failing"
	case "keeps-happening":
		return true, "keeps happening"
	case "same-bug":
		return true, "same bug"
	case "same-issue":
		return true, "same issue"
	case "how-many-times":
		return true, "how many times"
	case "this-is-broken":
		return true, "this is broken"
	case "this-broke":
		return true, "this broke"
	case "regression":
		return true, "regression"
	case "why-is-this-still":
		return true, "why is this still"
	case "why-does-this":
		return true, "why does this"
	case "shouldnt-have-to":
		return true, "shouldnt have to"
	case "should-not-have-to":
		return true, "should not have to"
	case "forgot-to-file":
		return true, "forgot to file"
	case "should-have-filed":
		return true, "should have filed"
	case "standing-rule":
		return true, "standing rule"
	case "going-forward":
		return true, "going forward"
	case "from-now-on":
		return true, "from now on"
	case "permission-denied":
		return true, "permission denied"
	case "timed-out":
		return true, "timed out"
	case "timeout":
		return true, "timeout"
	case "panic":
		return true, "panic:"
	case "failed-to":
		return true, "failed to"
	case "error":
		return true, "error:"
	default:
		return false, ""
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
