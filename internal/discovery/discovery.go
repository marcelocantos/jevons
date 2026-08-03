// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package discovery scans provider session stores for fleet inspect/status
// transcript paths (🎯T213).
//
// Layouts:
//
//	Grok Build:  ~/.grok/sessions/<url-encoded-cwd>/<session-id>/chat_history.jsonl
//	Claude Code: ~/.claude/projects/<escaped-cwd>/<session-id>.jsonl
//
// Roots.GrokSessions and Roots.ClaudeProjects are independently optional;
// empty roots simply yield no sessions from that store.
package discovery

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Roots names on-disk session trees for multi-provider discovery (🎯T213).
// Zero values disable that store. Prefer this over hard-wiring ~/.grok only.
type Roots struct {
	// GrokSessions is typically ~/.grok/sessions.
	GrokSessions string
	// ClaudeProjects is typically ~/.claude/projects (not ~/.claude).
	ClaudeProjects string
}

// SessionInfo holds metadata for one discovered session (any provider).
type SessionInfo struct {
	UUID       string    // session id
	ProjectDir string    // cwd/project bucket name under the store root
	WorkDir    string    // decoded working directory when recoverable (Grok); else ""
	GitBranch  string    // unused; kept for API stability
	ModTime    time.Time // last mtime of the transcript file (or session dir)
	Size       int64     // transcript size when known
	Active     bool      // true if listed in Grok active_sessions.json with live pid
	// Provider is "grok", "claude", or "" when unknown.
	Provider string
}

type cacheEntry struct {
	info    SessionInfo
	fetched time.Time
}

// Scanner walks configured session roots (Grok and/or Claude).
//
// Grok active-session metadata is read from ~/.grok/active_sessions.json
// (sibling of sessions/) when GrokSessions is set.
type Scanner struct {
	baseDir        string // ~/.grok/sessions (may be empty)
	claudeProjects string // ~/.claude/projects (may be empty)
	activePath     string // ~/.grok/active_sessions.json
	cacheTTL       time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry

	activeMu      sync.Mutex
	activeCache   map[string]bool
	activeFetched time.Time
}

// NewScanner creates a Grok-only Scanner. Prefer NewScannerRoots for Claude.
func NewScanner(sessionsDir string) *Scanner {
	return NewScannerRoots(Roots{GrokSessions: sessionsDir})
}

// NewScannerRoots creates a Scanner over multi-provider roots (🎯T213).
func NewScannerRoots(r Roots) *Scanner {
	activePath := ""
	if r.GrokSessions != "" {
		activePath = filepath.Join(filepath.Dir(r.GrokSessions), "active_sessions.json")
	}
	return &Scanner{
		baseDir:        r.GrokSessions,
		claudeProjects: r.ClaudeProjects,
		activePath:     activePath,
		cacheTTL:       60 * time.Second,
		cache:          make(map[string]cacheEntry),
	}
}

// Scan returns sessions modified within maxAge (0 = all). Merges roots (🎯T213).
func (s *Scanner) Scan(maxAge time.Duration) ([]SessionInfo, error) {
	cutoff := time.Time{}
	if maxAge > 0 {
		cutoff = time.Now().Add(-maxAge)
	}
	active := s.activeUUIDs()
	var results []SessionInfo
	if s.baseDir != "" {
		grok, err := s.scanGrok(cutoff, active)
		if err != nil {
			return nil, err
		}
		results = append(results, grok...)
	}
	if s.claudeProjects != "" {
		claude, err := s.scanClaude(cutoff, active)
		if err != nil {
			return nil, err
		}
		results = append(results, claude...)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].ModTime.After(results[j].ModTime)
	})
	return results, nil
}

func (s *Scanner) scanGrok(cutoff time.Time, active map[string]bool) ([]SessionInfo, error) {
	cwdBuckets, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var results []SessionInfo
	for _, bucket := range cwdBuckets {
		if !bucket.IsDir() {
			continue
		}
		bucketPath := filepath.Join(s.baseDir, bucket.Name())
		entries, err := os.ReadDir(bucketPath)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sid := e.Name()
			if !IsSessionID(sid) {
				continue
			}
			sessPath := filepath.Join(bucketPath, sid)
			hist := filepath.Join(sessPath, "chat_history.jsonl")
			fi, err := os.Stat(hist)
			if err != nil {
				fi, err = os.Stat(sessPath)
				if err != nil {
					continue
				}
			}
			if !cutoff.IsZero() && fi.ModTime().Before(cutoff) {
				continue
			}
			results = append(results, SessionInfo{
				UUID:       sid,
				ProjectDir: bucket.Name(),
				WorkDir:    decodeCWDBucket(bucket.Name()),
				ModTime:    fi.ModTime(),
				Size:       fi.Size(),
				Active:     active[sid],
				Provider:   "grok",
			})
		}
	}
	return results, nil
}

func (s *Scanner) scanClaude(cutoff time.Time, active map[string]bool) ([]SessionInfo, error) {
	projBuckets, err := os.ReadDir(s.claudeProjects)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var results []SessionInfo
	for _, bucket := range projBuckets {
		if !bucket.IsDir() {
			continue
		}
		bucketPath := filepath.Join(s.claudeProjects, bucket.Name())
		entries, err := os.ReadDir(bucketPath)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			sid, ok := claudeSessionIDFromBase(e.Name())
			if !ok {
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			if !cutoff.IsZero() && fi.ModTime().Before(cutoff) {
				continue
			}
			results = append(results, SessionInfo{
				UUID:       sid,
				ProjectDir: bucket.Name(),
				ModTime:    fi.ModTime(),
				Size:       fi.Size(),
				Active:     active[sid],
				Provider:   "claude",
			})
		}
	}
	return results, nil
}

// Get looks up a session by id across configured roots (Grok then Claude).
func (s *Scanner) Get(sessionID string) (*SessionInfo, error) {
	s.mu.Lock()
	if ce, ok := s.cache[sessionID]; ok && time.Since(ce.fetched) < s.cacheTTL {
		s.mu.Unlock()
		info := ce.info
		return &info, nil
	}
	s.mu.Unlock()

	if !IsSessionID(sessionID) {
		return nil, nil
	}
	active := s.activeUUIDs()
	if s.baseDir != "" {
		if info := s.getGrok(sessionID, active); info != nil {
			s.mu.Lock()
			s.cache[sessionID] = cacheEntry{info: *info, fetched: time.Now()}
			s.mu.Unlock()
			return info, nil
		}
	}
	if s.claudeProjects != "" {
		if info := s.getClaude(sessionID, active); info != nil {
			s.mu.Lock()
			s.cache[sessionID] = cacheEntry{info: *info, fetched: time.Now()}
			s.mu.Unlock()
			return info, nil
		}
	}
	return nil, nil
}

func (s *Scanner) getGrok(sessionID string, active map[string]bool) *SessionInfo {
	cwdBuckets, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil
	}
	for _, bucket := range cwdBuckets {
		if !bucket.IsDir() {
			continue
		}
		sessPath := filepath.Join(s.baseDir, bucket.Name(), sessionID)
		fi, err := os.Stat(sessPath)
		if err != nil {
			continue
		}
		hist := filepath.Join(sessPath, "chat_history.jsonl")
		size := fi.Size()
		mod := fi.ModTime()
		if hfi, err := os.Stat(hist); err == nil {
			size = hfi.Size()
			mod = hfi.ModTime()
		}
		return &SessionInfo{
			UUID:       sessionID,
			ProjectDir: bucket.Name(),
			WorkDir:    decodeCWDBucket(bucket.Name()),
			ModTime:    mod,
			Size:       size,
			Active:     active[sessionID],
			Provider:   "grok",
		}
	}
	return nil
}

func (s *Scanner) getClaude(sessionID string, active map[string]bool) *SessionInfo {
	path := ClaudeJSONLPath(s.claudeProjects, sessionID)
	if path == "" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	return &SessionInfo{
		UUID:       sessionID,
		ProjectDir: filepath.Base(filepath.Dir(path)),
		ModTime:    fi.ModTime(),
		Size:       fi.Size(),
		Active:     active[sessionID],
		Provider:   "claude",
	}
}

// IsActive reports whether sessionID is listed as an active Grok session.
func (s *Scanner) IsActive(sessionID string) bool {
	return s.activeUUIDs()[sessionID]
}

func (s *Scanner) activeUUIDs() map[string]bool {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if s.activeCache != nil && time.Since(s.activeFetched) < 5*time.Second {
		return s.activeCache
	}
	result := make(map[string]bool)
	defer func() {
		s.activeCache = result
		s.activeFetched = time.Now()
	}()
	if s.activePath == "" {
		return result
	}
	data, err := os.ReadFile(s.activePath)
	if err != nil {
		return result
	}
	var rows []struct {
		SessionID string `json:"session_id"`
		PID       int    `json:"pid"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return result
	}
	for _, row := range rows {
		if row.SessionID == "" {
			continue
		}
		if row.PID > 0 {
			if _, err := os.Stat(filepath.Join("/proc", itoa(row.PID))); err != nil {
				if !pidAlive(row.PID) {
					continue
				}
			}
		}
		result[row.SessionID] = true
	}
	return result
}

func pidAlive(pid int) bool {
	return processExists(pid)
}

func decodeCWDBucket(encoded string) string {
	if u, err := url.PathUnescape(encoded); err == nil && u != "" {
		return u
	}
	return encoded
}

// IsSessionID reports whether s looks like a UUID/ULID-with-dashes session id.
func IsSessionID(s string) bool {
	return IsUUID(s)
}

// IsUUID reports whether s is a 36-char UUID/ULID-with-dashes string.
func IsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// SessionPath returns the Grok session directory under baseDir, or "".
func SessionPath(sessionsDir, sessionID string) string {
	if !IsSessionID(sessionID) {
		return ""
	}
	cwdBuckets, err := os.ReadDir(sessionsDir)
	if err != nil {
		return ""
	}
	for _, bucket := range cwdBuckets {
		if !bucket.IsDir() {
			continue
		}
		p := filepath.Join(sessionsDir, bucket.Name(), sessionID)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
	}
	return ""
}

// ChatHistoryPath returns Grok chat_history.jsonl for the session, or "".
func ChatHistoryPath(sessionsDir, sessionID string) string {
	dir := SessionPath(sessionsDir, sessionID)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "chat_history.jsonl")
}

// EncodeCWDBucket encodes a workdir the way Grok names session buckets.
func EncodeCWDBucket(workdir string) string {
	return strings.ReplaceAll(url.PathEscape(workdir), "+", "%20")
}

// EncodeClaudeProject encodes a workdir the way Claude Code names project
// buckets under ~/.claude/projects (non-alphanumeric/dash → '-').
func EncodeClaudeProject(workDir string) string {
	var b strings.Builder
	for _, r := range workDir {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func claudeSessionIDFromBase(base string) (sid string, ok bool) {
	if !strings.HasSuffix(base, ".jsonl") {
		return "", false
	}
	name := strings.TrimSuffix(base, ".jsonl")
	switch name {
	case "", "updates", "chat_history", "events", "rewind_points",
		"prompt_history", "hunk_records", "btw_history":
		return "", false
	}
	if !IsSessionID(name) {
		return "", false
	}
	return name, true
}

// ClaudeJSONLPath locates projects/<bucket>/<sessionID>.jsonl by walking
// project buckets. projectsDir is typically ~/.claude/projects (🎯T213).
func ClaudeJSONLPath(projectsDir, sessionID string) string {
	if projectsDir == "" || !IsSessionID(sessionID) {
		return ""
	}
	buckets, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}
	want := sessionID + ".jsonl"
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		p := filepath.Join(projectsDir, bucket.Name(), want)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// ClaudeJSONLPathForWorkDir returns the deterministic Claude JSONL path for a
// known workdir under an overrideable projects root. "" when absent.
func ClaudeJSONLPathForWorkDir(projectsDir, workDir, sessionID string) string {
	if projectsDir == "" || sessionID == "" {
		return ""
	}
	p := filepath.Join(projectsDir, EncodeClaudeProject(workDir), sessionID+".jsonl")
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		return p
	}
	return ""
}

// TranscriptPath returns the JSONL transcript for sessionID across roots.
// Prefer Grok chat_history when present; else Claude session JSONL (🎯T213).
func TranscriptPath(r Roots, sessionID string) string {
	if !IsSessionID(sessionID) {
		return ""
	}
	if r.GrokSessions != "" {
		if p := ChatHistoryPath(r.GrokSessions, sessionID); p != "" {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	if r.ClaudeProjects != "" {
		if p := ClaudeJSONLPath(r.ClaudeProjects, sessionID); p != "" {
			return p
		}
	}
	return ""
}
