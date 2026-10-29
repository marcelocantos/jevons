// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package supervisormem is durable memory for the unattended supervisor tick
// (🎯T704): notes under <state_dir>/supervisor/notes.md, read and appended
// through the daemon so a repo-scoped session never needs ~/.jevons on PATH.
package supervisormem

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// MaxRollingLines is the brief's trim budget for the ROLLING section.
const MaxRollingLines = 150

const (
	headingRolling  = "## ROLLING"
	headingStanding = "## STANDING"
)

// NotesPath is ~/.jevons/supervisor/notes.md for the default state dir.
func NotesPath(stateDir string) string {
	return filepath.Join(stateDir, "supervisor", "notes.md")
}

// Store reads and appends the supervisor notes file for one state directory.
type Store struct {
	Path string
}

// Open returns a store for stateDir's supervisor notes.
func Open(stateDir string) (*Store, error) {
	dir := strings.TrimSpace(stateDir)
	if dir == "" {
		return nil, fmt.Errorf("supervisormem: state dir required")
	}
	return &Store{Path: NotesPath(dir)}, nil
}

// Read returns the full notes file. A missing file is an empty string, not an error.
func (s *Store) Read() (string, error) {
	if s == nil || s.Path == "" {
		return "", fmt.Errorf("supervisormem: store path required")
	}
	b, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(b), nil
}

// AppendRollingBlock appends one dated ROLLING block (including its ### header)
// and trims whole blocks from the top of ROLLING when it grows past
// MaxRollingLines.
func (s *Store) AppendRollingBlock(block string) error {
	if s == nil || s.Path == "" {
		return fmt.Errorf("supervisormem: store path required")
	}
	block = strings.TrimSpace(block)
	if block == "" {
		return fmt.Errorf("supervisormem: empty rolling block")
	}
	if !strings.HasPrefix(block, "### ") {
		return fmt.Errorf("supervisormem: rolling block must start with ### ")
	}
	return s.locked(func(body string) error {
		if body == "" {
			body = defaultSkeleton()
		}
		rolling, err := extractRolling(body)
		if err != nil {
			return err
		}
		rolling = strings.TrimRight(rolling, "\n")
		if rolling != "" {
			rolling += "\n\n"
		}
		rolling += block + "\n"
		rolling = trimRolling(rolling, MaxRollingLines)
		next, err := replaceRolling(body, rolling)
		if err != nil {
			return err
		}
		return writeAtomic(s.Path, next)
	})
}

func (s *Store) locked(fn func(string) error) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	lockPath := s.Path + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	body, err := s.Read()
	if err != nil {
		return err
	}
	return fn(body)
}

func defaultSkeleton() string {
	return `# Supervision notes

Two sections. STANDING holds facts that stay true until something changes
them, and is never trimmed. ROLLING holds what recent passes did, newest
last, and is trimmed from the top when it grows past ~150 lines.

` + headingStanding + `

` + headingRolling + `

`
}

func extractRolling(body string) (string, error) {
	i := strings.Index(body, headingRolling)
	if i < 0 {
		return "", fmt.Errorf("supervisormem: %s section missing", headingRolling)
	}
	rest := body[i+len(headingRolling):]
	rest = strings.TrimLeft(rest, " \t\r\n")
	if j := strings.Index(rest, "\n## "); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimRight(rest, "\n"), nil
}

func replaceRolling(body, rolling string) (string, error) {
	i := strings.Index(body, headingRolling)
	if i < 0 {
		return "", fmt.Errorf("supervisormem: %s section missing", headingRolling)
	}
	prefix := body[:i+len(headingRolling)]
	if !strings.HasSuffix(prefix, "\n") {
		prefix += "\n"
	}
	if rolling == "" {
		return prefix + "\n", nil
	}
	return prefix + "\n" + rolling + "\n", nil
}

func trimRolling(rolling string, maxLines int) string {
	if maxLines <= 0 || rolling == "" {
		return rolling
	}
	for lineCount(rolling) > maxLines {
		trimmed, ok := dropFirstRollingBlock(rolling)
		if !ok {
			break
		}
		rolling = trimmed
	}
	return rolling
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

func dropFirstRollingBlock(rolling string) (string, bool) {
	rolling = strings.TrimLeft(rolling, "\n")
	if !strings.HasPrefix(rolling, "### ") {
		return rolling, false
	}
	lines := strings.Split(rolling, "\n")
	end := len(lines)
	for i := 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "### ") {
			end = i
			break
		}
	}
	if end >= len(lines) {
		return "", true
	}
	rest := strings.Join(lines[end:], "\n")
	return strings.TrimLeft(rest, "\n"), true
}

func writeAtomic(path, body string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".notes-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

// RollingLineCount reports how many lines sit in the ROLLING section.
func RollingLineCount(body string) (int, error) {
	rolling, err := extractRolling(body)
	if err != nil {
		return 0, err
	}
	return lineCount(rolling), nil
}
