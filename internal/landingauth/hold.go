// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package landingauth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Snapshot is a durable policy version for an exact directive family. A grant
// records Epoch at mint time; redemption must compare it at the action boundary.
// The family is target-scoped, NOT recipient-scoped: a hold addressed to one
// seat invalidates grants already delivered to any seat for that target.
type Snapshot struct {
	Held   bool      `json:"held"`
	Epoch  uint64    `json:"epoch"`
	HoldID string    `json:"hold_id,omitempty"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at,omitempty"`
}

// Store is one authority for target safety holds and grant redemption.
// Grant spenders must use this same Store; do not keep an independent epoch
// snapshot. A durable Store uses an advisory OS lock across all instances.
type Store struct {
	mu  sync.Mutex
	dir string
}

func NewStore(dir string) *Store { return &Store{dir: dir} }
func (s *Store) Durable() bool   { return s != nil && s.dir != "" }
func key(repo, target string) (string, error) {
	if repo == "" || !filepath.IsAbs(repo) || filepath.Clean(repo) != repo || target == "" || target != strings.TrimSpace(target) || strings.ContainsAny(target, "\r\n") {
		return "", fmt.Errorf("landingauth: canonical absolute repo and target required")
	}
	b, _ := json.Marshal([]string{repo, target})
	return string(b), nil
}

// withEpochLock serializes hold acceptance with grant redemption, including
// separate Store instances in the same process and separate daemon processes.
// A redemption callback MUST complete the protected action before returning;
// it must not call back into this Store. For integration, the callback also
// has already acquired the shared-ref landing lock, spends the grant durably
// before mutation, and keeps both locks until the final ref update completes.
// Lock order: landing lock -> Store.mu -> flock; hold/release take only the
// latter two. If spend wins first, a later hold cannot undo that merge. If
// hold wins first, spend fails on Held/epoch mismatch. The global authority
// lock can stall OTHER target grants during a slow Git action, but never holds
// sendq.mu. Keep integration bounded and move long-running precomputation
// outside the callback; do not release this lock between spend and ref update.
func (s *Store) withEpochLock(fn func(map[string]Snapshot) error) error {
	if s == nil {
		return fmt.Errorf("landingauth: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withEpochLockHeld(fn)
}

func (s *Store) withEpochLockHeld(fn func(map[string]Snapshot) error) error {
	if !s.Durable() {
		return fmt.Errorf("landingauth: durable authority store required")
	}
	dir := s.dir
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	path := filepath.Join(dir, "epochs.json")
	epochs := map[string]Snapshot{}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(b, &epochs); err != nil {
			return fmt.Errorf("landingauth: corrupt hold epochs: %w", err)
		}
		if epochs == nil {
			return fmt.Errorf("landingauth: null hold epochs")
		}
	}
	// The callback writes through persistEpochs while this lock is held.
	return fn(epochs)
}

func (s *Store) persistEpochs(epochs map[string]Snapshot) error {
	if !s.Durable() {
		return fmt.Errorf("landingauth: durable authority store required")
	}
	path := filepath.Join(s.dir, "epochs.json")
	b, err := json.MarshalIndent(epochs, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".epochs-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := tmp.Chmod(0600); err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	parent, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

// Snapshot reads the current target policy version for grant minting.
// Held remains true until an explicitly verified PO release event.
func (s *Store) Snapshot(repo, target string) (Snapshot, error) {
	family, err := key(repo, target)
	if err != nil {
		return Snapshot{}, err
	}
	var result Snapshot
	err = s.withEpochLock(func(epochs map[string]Snapshot) error { result = epochs[family]; return nil })
	return result, err
}

// WithCurrent excludes holds while the caller validates and spends a grant
// durably, then performs its protected landing. Acquire the landing lock first;
// the callback MUST NOT call other Store methods or release the landing lock.
func (s *Store) WithCurrent(repo, target string, expected uint64, action func() error) error {
	family, err := key(repo, target)
	if err != nil {
		return err
	}
	if action == nil {
		return fmt.Errorf("landingauth: action required")
	}
	return s.withEpochLock(func(epochs map[string]Snapshot) error {
		current := epochs[family]
		if current.Epoch != expected || current.Held {
			return fmt.Errorf("landingauth: stale authorization for %q: grant epoch %d, current epoch %d, held=%t (hold %s: %s)", family, expected, current.Epoch, current.Held, current.HoldID, current.Reason)
		}
		return action()
	})
}

// AcceptHold advances policy independent of whether a notification reaches its
// recipient. Only explicitly scoped, trusted hold events should call this; a
// message in a transcript is not a safety hold. Holds are never auto-released.
func (s *Store) AcceptHold(repo, target, holdID, reason string, at time.Time) (Snapshot, error) {
	family, err := key(repo, target)
	if err != nil {
		return Snapshot{}, err
	}
	if holdID == "" {
		return Snapshot{}, fmt.Errorf("landingauth: typed hold id required")
	}
	var result Snapshot
	err = s.withEpochLock(func(epochs map[string]Snapshot) error {
		old := epochs[family]
		if old.Epoch == ^uint64(0) {
			return fmt.Errorf("landingauth: hold epoch overflow")
		}
		result = Snapshot{Held: true, Epoch: old.Epoch + 1, HoldID: holdID, Reason: reason, At: at.UTC()}
		epochs[family] = result
		if err := s.persistEpochs(epochs); err != nil {
			epochs[family] = old
			return err
		}
		return nil
	})
	return result, err
}

// ReleaseHold is authorized ONLY after the caller verifies a completed event
// emitted by the current registered PO process. A self-declared MCP actor or
// text message cannot supply that provenance. The PO event ID is audit data.
func (s *Store) ReleaseHold(repo, target, poEventID string, at time.Time) (Snapshot, error) {
	family, err := key(repo, target)
	if err != nil {
		return Snapshot{}, err
	}
	if poEventID == "" {
		return Snapshot{}, fmt.Errorf("landingauth: verified PO event ID required")
	}
	var result Snapshot
	err = s.withEpochLock(func(epochs map[string]Snapshot) error {
		old := epochs[family]
		if !old.Held {
			return fmt.Errorf("landingauth: target is not held")
		}
		if old.Epoch == ^uint64(0) {
			return fmt.Errorf("landingauth: hold epoch overflow")
		}
		result = Snapshot{Epoch: old.Epoch + 1, Held: false, HoldID: poEventID, Reason: "verified PO release", At: at.UTC()}
		epochs[family] = result
		if err := s.persistEpochs(epochs); err != nil {
			epochs[family] = old
			return err
		}
		return nil
	})
	return result, err
}
