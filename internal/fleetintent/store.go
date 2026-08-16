// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleetintent

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

// StoreFileName is the durable intent file under <stateDir>/fleet.
const StoreFileName = "intent.json"

// storeFile is the on-disk shape.
type storeFile struct {
	Fleet  Record            `json:"fleet"`
	Agents map[string]Record `json:"agents"`
}

// Store is the durable per-agent and fleet-wide intent ledger.
//
// Durability is the point, not an optimisation: the incident this package
// answers included a daemon restart resurrecting parked workers, so an
// intent that lives only in the daemon's memory would be erased by exactly
// the event it has to survive.
type Store struct {
	path string
	mu   sync.Mutex
	data storeFile
}

// Open loads or creates <stateDir>/fleet/intent.json. A malformed file is a
// hard error, never a silent reset — a store that quietly forgets a park is
// worse than one that refuses to start, because the forgetting is invisible
// until something gets resurrected.
func Open(stateDir string) (*Store, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, fmt.Errorf("fleetintent: StateDir required")
	}
	dir := filepath.Join(stateDir, "fleet")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		path: filepath.Join(dir, StoreFileName),
		data: storeFile{Agents: map[string]Record{}},
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
			return nil, fmt.Errorf("fleetintent store %s: %w", s.path, err)
		}
	}
	if s.data.Agents == nil {
		s.data.Agents = map[string]Record{}
	}
	return s, nil
}

// Path is the file this store persists to (empty for a nil store).
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Snapshot returns a copy safe to read without the lock. A nil store yields
// an all-working snapshot, so every control can call through unconditionally
// and a daemon started without a state dir behaves exactly as it did before
// this package existed.
func (s *Store) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{Agents: map[string]Record{}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	agents := make(map[string]Record, len(s.data.Agents))
	maps.Copy(agents, s.data.Agents)
	return Snapshot{Fleet: s.data.Fleet, Agents: agents}
}

// AgentState is the resolved intent for name.
func (s *Store) AgentState(name string) State { return s.Snapshot().AgentState(name) }

// FleetState is the resolved fleet-wide intent.
func (s *Store) FleetState() State { return s.Snapshot().FleetState() }

// Allow is the one call every control makes: may control c act on name?
func (s *Store) Allow(name string, c Control) Decision {
	return s.Snapshot().Allow(name, c)
}

// AllowSpawn is the fleet-only gate for starting a worker that does not
// exist yet.
func (s *Store) AllowSpawn() Decision { return s.Snapshot().AllowSpawn() }

// SetAgent records a deliberate intent for one agent. Setting Working is how
// an owner or overseer lifts a park; it keeps the provenance so the cockpit
// can say who lifted it.
func (s *Store) SetAgent(name string, st State, by, reason string, at time.Time) error {
	if s == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("fleetintent: agent name required")
	}
	if !Valid(st) {
		return fmt.Errorf("fleetintent: invalid state %q", st)
	}
	if at.IsZero() {
		at = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Agents == nil {
		s.data.Agents = map[string]Record{}
	}
	s.data.Agents[name] = Record{State: st, By: strings.TrimSpace(by), Reason: strings.TrimSpace(reason), At: at.UTC()}
	s.pruneReapedLocked(at)
	return s.persistLocked()
}

// SetFleet records a fleet-wide intent — the provider wall of 🎯T406, or an
// owner standing the whole fleet down.
func (s *Store) SetFleet(st State, by, reason string, at time.Time) error {
	if s == nil {
		return nil
	}
	if !Valid(st) {
		return fmt.Errorf("fleetintent: invalid state %q", st)
	}
	if at.IsZero() {
		at = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Fleet = Record{State: st, By: strings.TrimSpace(by), Reason: strings.TrimSpace(reason), At: at.UTC()}
	return s.persistLocked()
}

// Forget drops any recorded intent for name, returning it to Working by
// default. Used when an agent name is removed from the registry outright —
// an intent for a name nothing will ever look up again is only clutter.
func (s *Store) Forget(name string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data.Agents[name]; !ok {
		return nil
	}
	delete(s.data.Agents, name)
	return s.persistLocked()
}

// ReapedRetention is how long a [Reaped] stamp is kept.
//
// Reaped is the one state that accumulates without bound: every agent the
// fleet ever removes leaves one, and nothing ever lifts it, so without a
// bound the file and the per-tick snapshot copy grow with the lifetime of the
// daemon. Parked and the blocked states are never pruned — those are standing
// instructions, and forgetting one is the failure this package exists to
// prevent.
//
// A week is far longer than the stale-view window the stamp defends against:
// the fleet view of 🎯T413 outlived the registry by minutes, and any control
// that could still name a seven-day-dead agent is reading something older
// than the daemon that removed it.
const ReapedRetention = 7 * 24 * time.Hour

// pruneReapedLocked drops expired reap stamps. Caller holds mu.
func (s *Store) pruneReapedLocked(now time.Time) {
	for name, rec := range s.data.Agents {
		if rec.State == Reaped && !rec.At.IsZero() && now.Sub(rec.At) > ReapedRetention {
			delete(s.data.Agents, name)
		}
	}
}

// persistLocked writes atomically (write-and-rename). Caller holds mu.
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
