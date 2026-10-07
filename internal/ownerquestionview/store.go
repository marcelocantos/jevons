// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package ownerquestions keeps the cross-repository owner-decision index.
// The ledger's off-frontier status is not a question: intake is explicit and
// keyed by repository, target, question id and content version. The store is
// independent of a daemon process; readers reopen it after restart.
package ownerquestionview

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type State string

const (
	Open       State = "open"
	Answered   State = "answered"
	Superseded State = "superseded"
	Moot       State = "moot"
)

type Identity struct {
	Repo    string `json:"repo"`
	Target  string `json:"target"`
	ID      string `json:"id"`
	Version string `json:"version"`
}
type Question struct {
	Identity    Identity  `json:"identity"`
	Text        string    `json:"text"`
	Asker       string    `json:"asker"`
	AnswerRoute string    `json:"answer_route"`
	State       State     `json:"state"`
	Resolution  string    `json:"resolution,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Store uses a lock separate from the atomically replaced JSON file so
// independent daemon processes cannot lose each other's updates.
type Store struct{ dir string }

func New(dir string) *Store    { return &Store{dir: dir} }
func (s *Store) path() string  { return filepath.Join(s.dir, "owner-questions.json") }
func validState(st State) bool { return st == Open || st == Answered || st == Superseded || st == Moot }
func (id Identity) valid() bool {
	return filepath.IsAbs(id.Repo) && filepath.Clean(id.Repo) == id.Repo &&
		strings.TrimSpace(id.Target) != "" && strings.TrimSpace(id.ID) != "" && strings.TrimSpace(id.Version) != ""
}
func (id Identity) key() string {
	return id.Repo + "\x00" + id.Target + "\x00" + id.ID + "\x00" + id.Version
}
func (id Identity) family() string { return id.Repo + "\x00" + id.Target + "\x00" + id.ID }
func load(path string) ([]Question, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Question{}, nil
	}
	if err != nil {
		return nil, err
	}
	var rows []Question
	if err = json.Unmarshal(b, &rows); err != nil {
		return nil, fmt.Errorf("owner questions: corrupt store: %w", err)
	}
	for _, q := range rows {
		if !q.Identity.valid() || !validState(q.State) {
			return nil, fmt.Errorf("owner questions: corrupt record %q", q.Identity.ID)
		}
	}
	return rows, nil
}
func sorted(rows []Question) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].Identity.key() < rows[j].Identity.key() })
}

// List returns a snapshot, optionally filtering to currently-open decisions.
// Read errors are surfaced, never turned into an empty list.
func (s *Store) List(openOnly bool) ([]Question, error) {
	rows, err := load(s.path())
	if err != nil {
		return nil, err
	}
	out := make([]Question, 0, len(rows))
	for _, q := range rows {
		if !openOnly || q.State == Open {
			out = append(out, q)
		}
	}
	sorted(out)
	return out, nil
}
func (s *Store) mutate(fn func([]Question) ([]Question, error)) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(s.dir, "owner-questions.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	rows, err := load(s.path())
	if err != nil {
		return err
	}
	rows, err = fn(rows)
	if err != nil {
		return err
	}
	sorted(rows)
	b, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(s.dir, ".owner-questions-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err = tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), s.path()); err != nil {
		return err
	}
	if dir, err := os.Open(s.dir); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// Record is idempotent on the same identity/version. A genuinely revised
// question supersedes earlier open versions, retaining the audit history.
// It never reopens a resolved version on a replayed report.
func (s *Store) Record(q Question) error {
	if !q.Identity.valid() || strings.TrimSpace(q.Text) == "" || strings.TrimSpace(q.Asker) == "" || strings.TrimSpace(q.AnswerRoute) == "" {
		return fmt.Errorf("question requires absolute repo, target, id, version, text, asker and answer route")
	}
	q.State = Open
	q.UpdatedAt = time.Now().UTC()
	return s.mutate(func(rows []Question) ([]Question, error) {
		for _, old := range rows {
			if old.Identity.key() == q.Identity.key() {
				return rows, nil
			}
		}
		for i := range rows {
			if rows[i].Identity.family() == q.Identity.family() && rows[i].State == Open {
				rows[i].State = Superseded
				rows[i].Resolution = "revised question version"
				rows[i].UpdatedAt = q.UpdatedAt
			}
		}
		return append(rows, q), nil
	})
}

// Resolve requires the full identity (including version), a terminal state and
// an audit note. A late answer cannot close a newer version by target alone.
func (s *Store) Resolve(id Identity, state State, note string) error {
	if !id.valid() || state == Open || !validState(state) || strings.TrimSpace(note) == "" {
		return fmt.Errorf("resolution requires full identity, answered/superseded/moot state and note")
	}
	return s.mutate(func(rows []Question) ([]Question, error) {
		for i := range rows {
			if rows[i].Identity.key() == id.key() {
				if rows[i].State != Open {
					if rows[i].State == state && rows[i].Resolution == note {
						return rows, nil
					}
					return nil, fmt.Errorf("question already %s", rows[i].State)
				}
				rows[i].State = state
				rows[i].Resolution = note
				rows[i].UpdatedAt = time.Now().UTC()
				return rows, nil
			}
		}
		return nil, fmt.Errorf("unknown owner question %s/%s/%s@%s", id.Repo, id.Target, id.ID, id.Version)
	})
}
