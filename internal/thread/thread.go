// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package thread is the durable spine of the butler: a thread is a
// semantic unit of work (a Claude Code conversation plus its metadata
// and derived status), NOT tied to a live process. The process is a
// disposable cache — spun up to interact, stopped when idle, rehydrated
// on demand via --resume. The Store persists thread records so the full
// set survives a daemon restart ("never lose a thread").
package thread

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Kind distinguishes how the butler relates to a thread's process.
type Kind string

const (
	// KindAdopted is an existing session registered observe-only: the
	// butler tails its transcript for status but does not own or drive
	// the process. Directing it requires an explicit take-over.
	KindAdopted Kind = "adopted"
	// KindSpawned is a session the butler created and owns end-to-end.
	KindSpawned Kind = "spawned"
)

// Thread is the persisted record. It is deliberately small: durable
// identity plus the handle needed to rehydrate (SessionID → --resume).
// Live status is derived on demand from the transcript, never stored.
type Thread struct {
	ID          string    `json:"id"`              // stable butler-level handle
	Kind        Kind      `json:"kind"`            // adopted | spawned
	WorkDir     string    `json:"workdir"`         // the session's working directory
	SessionID   string    `json:"session_id"`      // Claude Code session UUID (for --resume)
	Description string    `json:"description"`     // owner's work-language label
	Model       string    `json:"model,omitempty"` // model override for the process, if any
	CreatedAt   time.Time `json:"created_at"`
}

// Store is a durable, concurrency-safe registry of threads backed by a
// single JSON file. Every mutation is flushed with an atomic
// write-and-rename so a crash mid-write cannot corrupt the set.
type Store struct {
	path string

	mu      sync.Mutex
	threads map[string]*Thread // keyed by Thread.ID
}

// NewStore loads the registry from path, creating an empty one if the
// file does not yet exist. A malformed file is a hard error rather than
// a silent reset — losing the thread set is the failure this package
// exists to prevent.
func NewStore(path string) (*Store, error) {
	s := &Store{path: path, threads: make(map[string]*Thread)}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("read thread store %q: %w", path, err)
	}

	var records []*Thread
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parse thread store %q: %w", path, err)
	}
	for _, t := range records {
		s.threads[t.ID] = t
	}
	return s, nil
}

// AdoptArgs parameterises Store.Adopt. WorkDir and Description are the
// only optional fields; the caller resolves WorkDir from the session's
// transcript before calling if the owner did not supply one.
type AdoptArgs struct {
	ID          string // defaults to SessionID when empty
	WorkDir     string
	SessionID   string
	Description string
	Now         time.Time // injected for deterministic tests; defaults to time.Now
}

// Adopt registers an existing session as an observe-only thread and
// persists it. It never touches the session's process — adoption is
// non-invasive by construction.
func (s *Store) Adopt(args AdoptArgs) (*Thread, error) {
	if args.SessionID == "" {
		return nil, fmt.Errorf("adopt: session id is required")
	}
	id := args.ID
	if id == "" {
		id = args.SessionID
	}
	now := args.Now
	if now.IsZero() {
		now = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.threads[id]; ok {
		if existing.SessionID == args.SessionID {
			return existing, nil // idempotent re-adopt
		}
		return nil, fmt.Errorf("adopt: thread %q already exists for a different session", id)
	}

	t := &Thread{
		ID:          id,
		Kind:        KindAdopted,
		WorkDir:     args.WorkDir,
		SessionID:   args.SessionID,
		Description: args.Description,
		CreatedAt:   now,
	}
	s.threads[id] = t
	if err := s.save(); err != nil {
		delete(s.threads, id)
		return nil, err
	}
	return t, nil
}

// Put inserts or replaces a thread record and persists it. Used by the
// spawn path (KindSpawned) and by any lifecycle transition that mutates
// a record.
func (s *Store) Put(t *Thread) error {
	if t.ID == "" {
		return fmt.Errorf("put: thread id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.threads[t.ID]
	s.threads[t.ID] = t
	if err := s.save(); err != nil {
		if prev != nil {
			s.threads[t.ID] = prev
		} else {
			delete(s.threads, t.ID)
		}
		return err
	}
	return nil
}

// FindBySession returns the thread that already tracks sessionID, if any.
// It is what makes adoption idempotent — re-adopting a session returns the
// existing thread instead of creating a duplicate.
func (s *Store) FindBySession(sessionID string) (*Thread, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.threads {
		if t.SessionID == sessionID {
			c := *t
			return &c, true
		}
	}
	return nil, false
}

// Get returns a copy of the thread with the given ID.
func (s *Store) Get(id string) (*Thread, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.threads[id]
	if !ok {
		return nil, false
	}
	c := *t
	return &c, true
}

// List returns all threads sorted by creation time (newest first).
func (s *Store) List() []*Thread {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Thread, 0, len(s.threads))
	for _, t := range s.threads {
		c := *t
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// Remove deletes a thread record and persists the change.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.threads[id]
	if !ok {
		return nil
	}
	delete(s.threads, id)
	if err := s.save(); err != nil {
		s.threads[id] = prev
		return err
	}
	return nil
}

// save serialises the registry with an atomic write-and-rename. The
// caller must hold s.mu.
func (s *Store) save() error {
	records := make([]*Thread, 0, len(s.threads))
	for _, t := range s.threads {
		records = append(records, t)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal thread store: %w", err)
	}

	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create thread store dir: %w", err)
		}
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write thread store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("commit thread store: %w", err)
	}
	return nil
}
