// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package seatplan stores per-seat placement and migration fields that
// the published claudia AgentDef does not carry. Development builds
// against a sibling claudia checkout used to keep these on the registry
// row; the pinned module has no such fields, so they live beside
// agents.json and round-trip on their own.
package seatplan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/marcelocantos/claudia"
)

// State is one seat's host policy and in-flight Claudia migration.
type State struct {
	PreferProvider        claudia.Provider   `json:"prefer_provider,omitempty"`
	AllowedProviders      []claudia.Provider `json:"allowed_providers,omitempty"`
	AllowNone             bool               `json:"allow_none,omitempty"`
	ExcludeProviders      []claudia.Provider `json:"exclude_providers,omitempty"`
	HostMayInterrupt      bool               `json:"host_may_interrupt,omitempty"`
	HostNeverPark         bool               `json:"host_never_park,omitempty"`
	MigrationSeed         string             `json:"migration_seed,omitempty"`
	MigrationFrom         claudia.Provider   `json:"migration_from,omitempty"`
	MigrationFromSession  string             `json:"migration_from_session,omitempty"`
	MigrationPendingStart bool               `json:"migration_pending_start,omitempty"`
}

// Allowed reports the destination allow-list. restricted is false when
// every published dest is eligible. An explicit empty list (AllowNone)
// is restricted and allows nothing.
func (s State) Allowed() (providers []claudia.Provider, restricted bool) {
	if s.AllowNone {
		return []claudia.Provider{}, true
	}
	if s.AllowedProviders == nil {
		return nil, false
	}
	return append([]claudia.Provider(nil), s.AllowedProviders...), true
}

// Store is the seatplan file. A nil Store reads as empty state.
type Store struct {
	path string
	mu   sync.Mutex
	rows map[string]State
}

// Open loads path, creating it on the first write. An empty path is
// memory-only, for tests that do not reload.
func Open(path string) (*Store, error) {
	st := &Store{path: path, rows: map[string]State{}}
	if path == "" {
		return st, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	if len(body) == 0 {
		return st, nil
	}
	if err := json.Unmarshal(body, &st.rows); err != nil {
		return nil, fmt.Errorf("seat plan %s: %w", path, err)
	}
	if st.rows == nil {
		st.rows = map[string]State{}
	}
	return st, nil
}

// Get returns the seat's state. A missing seat is the zero state.
func (s *Store) Get(name string) State {
	if s == nil {
		return State{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rows[name]
}

// Put replaces the seat's state and saves.
func (s *Store) Put(name string, st State) error {
	if s == nil {
		return fmt.Errorf("seat plan store is not configured")
	}
	return s.Update(name, func(cur *State) { *cur = st })
}

// Update mutates the seat's state under the lock and saves.
func (s *Store) Update(name string, fn func(*State)) error {
	if s == nil {
		return fmt.Errorf("seat plan store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.rows[name]
	fn(&cur)
	if s.rows == nil {
		s.rows = map[string]State{}
	}
	s.rows[name] = cur
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	body, err := json.MarshalIndent(s.rows, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
