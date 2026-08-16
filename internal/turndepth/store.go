// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turndepth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store holds each session's current turn depth on disk.
//
// One file per session, because N concurrent workers share one clone and one
// home directory: a single shared counter would attribute one agent's depth
// to another, which is the shared-mutable-state defect 🎯T376 exists about.
type Store struct {
	Root string
}

// StoreDirEnv overrides the counter directory (tests, isolates).
const StoreDirEnv = "JEVONS_TURNDEPTH_DIR"

// TurnTTL bounds how long a turn record is believed.
//
// The hook learns about turn ends from the harness telling it so. If that
// signal is ever missed — an event this hook is not matched on, a session
// resumed out of band — the record would otherwise persist forever and every
// later turn would meet the ceiling on its first call. A record older than
// this is treated as a turn that ended unobserved, which is the direction
// that fails toward silence rather than toward nagging.
const TurnTTL = time.Hour

// Turn is one session's live turn as the hook has counted it.
type Turn struct {
	Calls     int       `json:"calls"`
	Requested bool      `json:"requested"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DefaultStoreRoot is ~/.jevons/turndepth unless StoreDirEnv is set.
func DefaultStoreRoot() string {
	if dir := os.Getenv(StoreDirEnv); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "jevons-turndepth")
	}
	return filepath.Join(home, ".jevons", "turndepth")
}

// Load reads the session's turn. A session with no record, or one whose
// record has aged past TurnTTL, yields a fresh zero turn — absence and
// staleness are both "this is a new turn", not errors.
//
// A record that exists and cannot be parsed IS an error: silently treating a
// torn file as a fresh turn would disable the ceiling for that session for
// as long as the file stayed torn, and say nothing.
func (s *Store) Load(session string, now time.Time) (Turn, error) {
	b, err := os.ReadFile(s.path(session))
	if errors.Is(err, os.ErrNotExist) {
		return Turn{}, nil
	}
	if err != nil {
		return Turn{}, err
	}
	var t Turn
	if err := json.Unmarshal(b, &t); err != nil {
		return Turn{}, err
	}
	if !t.UpdatedAt.IsZero() && now.Sub(t.UpdatedAt) > TurnTTL {
		return Turn{}, nil
	}
	return t, nil
}

// Save writes the session's turn, stamped with now.
func (s *Store) Save(session string, t Turn, now time.Time) error {
	t.UpdatedAt = now
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	path := s.path(session)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeAtomic(path, b)
}

// Reset ends the session's turn. Removing the file rather than zeroing it
// keeps the directory bounded by live sessions instead of by every session
// that ever ran.
func (s *Store) Reset(session string) error {
	err := os.Remove(s.path(session))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Prune drops records untouched for longer than maxAge.
func (s *Store) Prune(maxAge time.Duration, now time.Time) error {
	entries, err := os.ReadDir(s.Root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(s.Root, e.Name())
		info, err := os.Stat(p)
		if err != nil || now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		if err := os.Remove(p); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) path(session string) string {
	return filepath.Join(s.Root, sessionKey(session)+".json")
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".turndepth-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// sessionKey makes a session id safe as a filename without collapsing two
// distinct ids onto one file for any id the harness actually issues (uuids).
func sessionKey(session string) string {
	if session == "" {
		return "unknown-session"
	}
	var b strings.Builder
	for _, r := range session {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
