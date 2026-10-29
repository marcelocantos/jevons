// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package landhold

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const StoreFileName = "land-hold.json"

type record struct {
	Hold   Hold      `json:"hold"`
	Actor  string    `json:"actor,omitempty"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at,omitempty"`
}

type storeFile struct {
	Fleet  *record           `json:"fleet,omitempty"`
	Agents map[string]record `json:"agents,omitempty"`
}

// Store is the durable fleet-wide and per-seat land-hold ledger.
type Store struct {
	path string
	mu   sync.Mutex
	data storeFile
}

// Open loads or creates <stateDir>/fleet/land-hold.json.
func Open(stateDir string) (*Store, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, fmt.Errorf("landhold: StateDir required")
	}
	dir := filepath.Join(stateDir, "fleet")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		path: filepath.Join(dir, StoreFileName),
		data: storeFile{Agents: map[string]record{}},
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &s.data); err != nil {
			return nil, fmt.Errorf("landhold store %s: %w", s.path, err)
		}
	}
	if s.data.Agents == nil {
		s.data.Agents = map[string]record{}
	}
	return s, nil
}

// Path is the persistence file (empty when s is nil).
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (h Hold) union(o Hold) Hold {
	return Hold{NoMerge: h.NoMerge || o.NoMerge, NoActivation: h.NoActivation || o.NoActivation}
}

// EffectiveHold returns the union of fleet-wide and per-seat holds.
func (s *Store) EffectiveHold(seat string) Hold {
	if s == nil {
		return Hold{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Hold
	if s.data.Fleet != nil {
		out = out.union(s.data.Fleet.Hold)
	}
	if rec, ok := s.data.Agents[strings.TrimSpace(seat)]; ok {
		out = out.union(rec.Hold)
	}
	return out
}

// SetFleet records or clears a fleet-wide hold.
func (s *Store) SetFleet(h Hold, actor, reason string, now time.Time) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !h.Active() {
		s.data.Fleet = nil
	} else {
		s.data.Fleet = &record{Hold: h, Actor: actor, Reason: reason, At: now.UTC()}
	}
	return s.persistLocked()
}

// SetAgent records or clears a per-seat hold.
func (s *Store) SetAgent(seat string, h Hold, actor, reason string, now time.Time) error {
	seat = strings.TrimSpace(seat)
	if s == nil || seat == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !h.Active() {
		delete(s.data.Agents, seat)
	} else {
		s.data.Agents[seat] = record{Hold: h, Actor: actor, Reason: reason, At: now.UTC()}
	}
	return s.persistLocked()
}

// ClearAgent drops a seat row.
func (s *Store) ClearAgent(seat string) error {
	return s.SetAgent(seat, Hold{}, "", "", time.Now())
}

func (s *Store) persistLocked() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Snapshot is a copy safe to read without the lock.
func (s *Store) Snapshot() storeFile {
	if s == nil {
		return storeFile{Agents: map[string]record{}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	agents := make(map[string]record, len(s.data.Agents))
	maps.Copy(agents, s.data.Agents)
	out := storeFile{Agents: agents}
	if s.data.Fleet != nil {
		cp := *s.data.Fleet
		out.Fleet = &cp
	}
	return out
}
