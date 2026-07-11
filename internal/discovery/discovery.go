// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package discovery scans ~/.grok/sessions for Grok Build session
// directories and reports which sessions are active.
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

// SessionInfo holds metadata for one Grok Build session.
type SessionInfo struct {
	UUID       string    // session id (directory name under the cwd bucket)
	ProjectDir string    // URL-encoded cwd bucket under sessions/
	WorkDir    string    // decoded working directory
	GitBranch  string    // unused for Grok; kept for API stability
	ModTime    time.Time // last mtime of chat_history.jsonl (or session dir)
	Size       int64     // size of chat_history.jsonl when present
	Active     bool      // true if listed in active_sessions.json with live pid
}

type cacheEntry struct {
	info    SessionInfo
	fetched time.Time
}

// Scanner walks a Grok sessions tree.
//
// Layout (Grok Build):
//
//	~/.grok/sessions/<url-encoded-cwd>/<session-id>/chat_history.jsonl
//	~/.grok/active_sessions.json  [{session_id,pid,cwd,...}, ...]
type Scanner struct {
	// baseDir is ~/.grok/sessions
	baseDir string
	// activePath is ~/.grok/active_sessions.json (sibling of sessions/)
	activePath string
	cacheTTL   time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry

	activeMu      sync.Mutex
	activeCache   map[string]bool
	activeFetched time.Time
}

// NewScanner creates a Scanner. sessionsDir is typically ~/.grok/sessions.
// Active-session metadata is read from the parent dir's active_sessions.json.
func NewScanner(sessionsDir string) *Scanner {
	return &Scanner{
		baseDir:    sessionsDir,
		activePath: filepath.Join(filepath.Dir(sessionsDir), "active_sessions.json"),
		cacheTTL:   60 * time.Second,
		cache:      make(map[string]cacheEntry),
	}
}

// Scan returns sessions whose chat history (or session dir) was modified
// within maxAge. maxAge 0 means all sessions.
func (s *Scanner) Scan(maxAge time.Duration) ([]SessionInfo, error) {
	cutoff := time.Time{}
	if maxAge > 0 {
		cutoff = time.Now().Add(-maxAge)
	}

	cwdBuckets, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	active := s.activeUUIDs()
	var results []SessionInfo

	for _, bucket := range cwdBuckets {
		if !bucket.IsDir() {
			continue
		}
		// Skip non-bucket files (e.g. prompt_history.jsonl at sessions root of a cwd)
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
			info := SessionInfo{
				UUID:       sid,
				ProjectDir: bucket.Name(),
				WorkDir:    decodeCWDBucket(bucket.Name()),
				ModTime:    fi.ModTime(),
				Size:       fi.Size(),
				Active:     active[sid],
			}
			results = append(results, info)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].ModTime.After(results[j].ModTime)
	})
	return results, nil
}

// Get looks up a single session by id.
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

	cwdBuckets, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	active := s.activeUUIDs()
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
		info := SessionInfo{
			UUID:       sessionID,
			ProjectDir: bucket.Name(),
			WorkDir:    decodeCWDBucket(bucket.Name()),
			ModTime:    mod,
			Size:       size,
			Active:     active[sessionID],
		}
		s.mu.Lock()
		s.cache[sessionID] = cacheEntry{info: info, fetched: time.Now()}
		s.mu.Unlock()
		return &info, nil
	}
	return nil, nil
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
		// Require the process to still exist.
		if row.PID > 0 {
			if _, err := os.Stat(filepath.Join("/proc", itoa(row.PID))); err != nil {
				// macOS has no /proc — fall back to kill(0).
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
	// Grok encodes cwd with percent-encoding of path separators (%2F).
	if u, err := url.PathUnescape(encoded); err == nil && u != "" {
		return u
	}
	return encoded
}

// IsSessionID reports whether s looks like a Grok/ULID-style session id
// (8-4-4-4-12 hex with dashes — same shape as UUID).
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

// SessionPath returns the on-disk directory for a session under baseDir,
// or "" if not found.
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

// ChatHistoryPath returns chat_history.jsonl for the session, or "".
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
