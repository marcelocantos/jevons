// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CoachCursor tracks drip-feed progress so cycles do not re-ingest whole
// histories (🎯T243 continuous read). state_dir/rsi/coach_cursor.json.
type CoachCursor struct {
	// ChatLogByte is the next byte offset to read in the owner chatlog.
	ChatLogByte int64 `json:"chat_log_byte"`
	// EventLogByte is the next byte offset in the event journal.
	EventLogByte int64 `json:"event_log_byte"`
	// SessionOffsets maps session chat_history path → next byte offset.
	SessionOffsets map[string]int64 `json:"session_offsets,omitempty"`
	// UpdatedAt last drip advance (RFC3339).
	UpdatedAt string `json:"updated_at,omitempty"`
}

// CoachCursorPath is stateDir/rsi/coach_cursor.json.
func CoachCursorPath(stateDir string) string {
	return filepath.Join(stateDir, "rsi", "coach_cursor.json")
}

// LoadCoachCursor loads cursor; missing → zero (first drip reads from start,
// but callers typically seed to EOF on first boot to avoid flooding).
func LoadCoachCursor(stateDir string) (CoachCursor, error) {
	path := CoachCursorPath(stateDir)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return CoachCursor{SessionOffsets: map[string]int64{}}, nil
	}
	if err != nil {
		return CoachCursor{}, fmt.Errorf("rsi coach cursor: read: %w", err)
	}
	if len(bytesTrimSpace(data)) == 0 {
		return CoachCursor{SessionOffsets: map[string]int64{}}, nil
	}
	var c CoachCursor
	if err := json.Unmarshal(data, &c); err != nil {
		return CoachCursor{}, fmt.Errorf("rsi coach cursor: parse: %w", err)
	}
	if c.SessionOffsets == nil {
		c.SessionOffsets = map[string]int64{}
	}
	return c, nil
}

// SaveCoachCursor atomically persists cursor.
func SaveCoachCursor(stateDir string, c CoachCursor) error {
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("rsi coach cursor: stateDir required")
	}
	dir := filepath.Join(stateDir, "rsi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("rsi coach cursor: mkdir: %w", err)
	}
	if c.SessionOffsets == nil {
		c.SessionOffsets = map[string]int64{}
	}
	path := CoachCursorPath(stateDir)
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// SeedCursorAtEOF sets chat/event offsets to current file sizes so the first
// live cycle only sees new appends (no historical flood). Sessions: seed each
// known path to its size when maxSessions > 0.
func SeedCursorAtEOF(stateDir, chatLogPath, eventLogPath, sessionsDir string, maxSessions int) (CoachCursor, error) {
	c, err := LoadCoachCursor(stateDir)
	if err != nil {
		return c, err
	}
	// Only seed when still zero (first run).
	if c.ChatLogByte == 0 && c.EventLogByte == 0 && len(c.SessionOffsets) == 0 {
		c.ChatLogByte = fileSize(chatLogPath)
		c.EventLogByte = fileSize(eventLogPath)
		if sessionsDir != "" {
			for _, p := range listSessionHistories(sessionsDir, maxSessions) {
				c.SessionOffsets[p] = fileSize(p)
			}
		}
		c.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := SaveCoachCursor(stateDir, c); err != nil {
			return c, err
		}
	}
	return c, nil
}

// DripChatLogTurns reads new chatlog lines since cursor.ChatLogByte.
// Advances cursor on success. Returns turns + updated cursor.
func DripChatLogTurns(path string, cur CoachCursor, maxTurns int) ([]ChatTurn, CoachCursor, error) {
	if strings.TrimSpace(path) == "" {
		return nil, cur, nil
	}
	if maxTurns <= 0 {
		maxTurns = DefaultChatLookback
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, cur, nil
		}
		return nil, cur, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, cur, err
	}
	size := st.Size()
	// Truncation / rewrite: reset offset.
	if cur.ChatLogByte > size {
		cur.ChatLogByte = 0
	}
	if _, err := f.Seek(cur.ChatLogByte, io.SeekStart); err != nil {
		return nil, cur, err
	}

	var turns []ChatTurn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var read int64
	for sc.Scan() {
		line := sc.Text()
		// +1 for newline (scanner strips it; file used \n).
		read += int64(len(line)) + 1
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		turn, ok := parseChatlogLine(line, path)
		if !ok {
			continue
		}
		turns = append(turns, turn)
	}
	if err := sc.Err(); err != nil {
		return nil, cur, err
	}
	if len(turns) > maxTurns {
		turns = turns[len(turns)-maxTurns:]
	}
	// Prefer actual file position after scan.
	if pos, err := f.Seek(0, io.SeekCurrent); err == nil {
		cur.ChatLogByte = pos
	} else {
		cur.ChatLogByte += read
		if cur.ChatLogByte > size {
			cur.ChatLogByte = size
		}
	}
	return turns, cur, nil
}

// DripEventLogRows reads new eventlog JSONL lines since EventLogByte.
func DripEventLogRows(path string, cur CoachCursor, maxRows int) ([]EventRow, CoachCursor, error) {
	if strings.TrimSpace(path) == "" {
		return nil, cur, nil
	}
	if maxRows <= 0 {
		maxRows = DefaultLookback
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, cur, nil
		}
		return nil, cur, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, cur, err
	}
	size := st.Size()
	if cur.EventLogByte > size {
		cur.EventLogByte = 0
	}
	if _, err := f.Seek(cur.EventLogByte, io.SeekStart); err != nil {
		return nil, cur, err
	}

	var rows []EventRow
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var raw map[string]any
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		row := EventRow{
			TS:        asString(raw["ts"]),
			Level:     asString(raw["level"]),
			Msg:       asString(raw["msg"]),
			Component: asString(raw["component"]),
			Decision:  asString(raw["decision"]),
			Corr:      asString(raw["corr"]),
		}
		if outcome, ok := raw["outcome"].(string); ok {
			row.Outcome = outcome
		}
		if fields, ok := raw["fields"].(map[string]any); ok {
			row.Fields = map[string]string{}
			for k, v := range fields {
				row.Fields[k] = fmt.Sprint(v)
				if k == "outcome" && row.Outcome == "" {
					row.Outcome = fmt.Sprint(v)
				}
			}
		}
		// Some journals nest outcome only in fields.
		if row.Outcome == "" {
			if o, ok := raw["outcome"].(string); ok {
				row.Outcome = o
			}
		}
		rows = append(rows, row)
	}
	if err := sc.Err(); err != nil {
		return nil, cur, err
	}
	if len(rows) > maxRows {
		rows = rows[len(rows)-maxRows:]
	}
	if pos, err := f.Seek(0, io.SeekCurrent); err == nil {
		cur.EventLogByte = pos
	} else {
		cur.EventLogByte = size
	}
	return rows, cur, nil
}

// DripSessionTurns reads new lines from session chat_history files.
func DripSessionTurns(sessionsDir string, cur CoachCursor, maxSessions, maxTurnsPerSession int) ([]ChatTurn, CoachCursor, error) {
	if strings.TrimSpace(sessionsDir) == "" {
		return nil, cur, nil
	}
	if maxSessions <= 0 {
		maxSessions = DefaultSessionLookback
	}
	if maxTurnsPerSession <= 0 {
		maxTurnsPerSession = DefaultSessionTurnCap
	}
	if cur.SessionOffsets == nil {
		cur.SessionOffsets = map[string]int64{}
	}
	paths := listSessionHistories(sessionsDir, maxSessions)
	var out []ChatTurn
	for _, path := range paths {
		turns, next, err := dripOneSession(path, cur.SessionOffsets[path], maxTurnsPerSession)
		if err != nil {
			continue
		}
		cur.SessionOffsets[path] = next
		out = append(out, turns...)
	}
	return out, cur, nil
}

func dripOneSession(path string, offset int64, maxTurns int) ([]ChatTurn, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, offset, nil
		}
		return nil, offset, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	size := st.Size()
	if offset > size {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	sid := filepath.Base(filepath.Dir(path))
	var turns []ChatTurn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		turn, ok := parseSessionLine(line, sid)
		if !ok {
			continue
		}
		turns = append(turns, turn)
	}
	if err := sc.Err(); err != nil {
		return nil, offset, err
	}
	if len(turns) > maxTurns {
		turns = turns[len(turns)-maxTurns:]
	}
	pos, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		pos = size
	}
	return turns, pos, nil
}

func fileSize(path string) int64 {
	if strings.TrimSpace(path) == "" {
		return 0
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func listSessionHistories(sessionsDir string, maxSessions int) []string {
	type hit struct {
		path string
		mod  time.Time
	}
	var hits []hit
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
		hits = append(hits, hit{path: path, mod: info.ModTime()})
		return nil
	})
	for i := 0; i < len(hits); i++ {
		for j := i + 1; j < len(hits); j++ {
			if hits[j].mod.After(hits[i].mod) {
				hits[i], hits[j] = hits[j], hits[i]
			}
		}
	}
	if maxSessions > 0 && len(hits) > maxSessions {
		hits = hits[:maxSessions]
	}
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.path)
	}
	return out
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
