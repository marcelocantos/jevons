// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package idea

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	// FileName is the durable ledger under state_dir.
	FileName = "ideas.json"
	// MaxKeep caps retained records (oldest drop on append). Thin residual.
	MaxKeep = 500
)

// Store is a durable listable idea ledger (atomic write-and-rename).
type Store struct {
	path string
	mu   sync.Mutex
}

type storeFile struct {
	Version int      `json:"version"`
	Ideas   []Record `json:"ideas"`
}

// Open loads (or creates) the idea ledger at stateDir/ideas.json.
func Open(stateDir string) (*Store, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, fmt.Errorf("idea store: stateDir required")
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("idea store: mkdir: %w", err)
	}
	return &Store{path: filepath.Join(stateDir, FileName)}, nil
}

// OpenPath opens a ledger at an explicit path (tests).
func OpenPath(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("idea store: path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("idea store: mkdir: %w", err)
	}
	return &Store{path: path}, nil
}

// Path returns the ledger file path.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Capture validates args, appends a record, and returns it.
func (s *Store) Capture(args CaptureArgs) (Record, error) {
	if s == nil {
		return Record{}, fmt.Errorf("idea store: nil")
	}
	rec, err := NewRecord(args)
	if err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ideas, err := s.load()
	if err != nil {
		return Record{}, err
	}
	// Dedupe by id (tests may re-mint).
	for i, existing := range ideas {
		if existing.ID == rec.ID {
			ideas[i] = rec
			if err := s.save(ideas); err != nil {
				return Record{}, err
			}
			return rec, nil
		}
	}
	ideas = append(ideas, rec)
	if len(ideas) > MaxKeep {
		// Keep newest by CreatedUnix.
		sort.Slice(ideas, func(i, j int) bool {
			return ideas[i].CreatedUnix < ideas[j].CreatedUnix
		})
		ideas = ideas[len(ideas)-MaxKeep:]
	}
	if err := s.save(ideas); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// List returns all ideas, newest first. Optional disposition filter (empty = all).
func (s *Store) List(disposition string) ([]Record, error) {
	if s == nil {
		return nil, fmt.Errorf("idea store: nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ideas, err := s.load()
	if err != nil {
		return nil, err
	}
	var want Disposition
	if filter := strings.TrimSpace(disposition); filter != "" {
		want = NormalizeDisposition(filter)
	}
	var out []Record
	for _, r := range ideas {
		if want != "" && r.Disposition != want {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedUnix == out[j].CreatedUnix {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedUnix > out[j].CreatedUnix
	})
	return out, nil
}

// Get returns one idea by id.
func (s *Store) Get(id string) (Record, error) {
	if s == nil {
		return Record{}, fmt.Errorf("idea store: nil")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Record{}, fmt.Errorf("idea store: id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ideas, err := s.load()
	if err != nil {
		return Record{}, err
	}
	for _, r := range ideas {
		if r.ID == id {
			return r, nil
		}
	}
	return Record{}, fmt.Errorf("idea store: not found: %s", id)
}

// Triage updates disposition for an existing idea.
func (s *Store) Triage(args TriageArgs) (Record, error) {
	if s == nil {
		return Record{}, fmt.Errorf("idea store: nil")
	}
	id := strings.TrimSpace(args.ID)
	if id == "" {
		return Record{}, fmt.Errorf("idea store: id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ideas, err := s.load()
	if err != nil {
		return Record{}, err
	}
	for i, r := range ideas {
		if r.ID != id {
			continue
		}
		updated, err := ApplyTriage(r, args)
		if err != nil {
			return Record{}, err
		}
		ideas[i] = updated
		if err := s.save(ideas); err != nil {
			return Record{}, err
		}
		return updated, nil
	}
	return Record{}, fmt.Errorf("idea store: not found: %s", id)
}

func (s *Store) load() ([]Record, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("idea store: read %s: %w", s.path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var sf storeFile
	if err := json.Unmarshal(data, &sf); err != nil {
		// Hard error on malformed state — never silent reset (AGENTS.md).
		return nil, fmt.Errorf("idea store: parse %s: %w", s.path, err)
	}
	if sf.Ideas == nil {
		return nil, nil
	}
	return sf.Ideas, nil
}

func (s *Store) save(ideas []Record) error {
	sf := storeFile{Version: 1, Ideas: ideas}
	if sf.Ideas == nil {
		sf.Ideas = []Record{}
	}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("idea store: marshal: %w", err)
	}
	data = append(data, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("idea store: write tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("idea store: rename: %w", err)
	}
	return nil
}
