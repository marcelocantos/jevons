// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package handover

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Rotation is the durable last-rotation record (🎯T392.1.1). The governor
// hold key is this, not an in-memory map: a SIGHUP that forgets process
// state must still hold after a just-completed migrate or compact.
type Rotation struct {
	Agent string `json:"agent"`
	Kind  string `json:"kind"` // migrate | compact | upgrade
	At    string `json:"at"`   // RFC3339
}

// Time is when the rotation happened, and whether that is knowable.
func (r Rotation) Time() (time.Time, bool) {
	stamp := strings.TrimSpace(r.At)
	if stamp == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// RotationStore persists last-rotation rows under dir.
type RotationStore struct {
	dir string
}

// NewRotationStore roots a store at dir (conventionally <state_dir>/rotations).
func NewRotationStore(dir string) *RotationStore { return &RotationStore{dir: dir} }

func (s *RotationStore) path(agent string) (string, error) {
	name := strings.TrimSpace(agent)
	if name == "" {
		return "", fmt.Errorf("rotation: agent name is required")
	}
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", fmt.Errorf("rotation: unusable agent name %q", agent)
	}
	return filepath.Join(s.dir, name+".json"), nil
}

// Put writes a last-rotation record. At is stamped when the caller left it blank.
func (s *RotationStore) Put(r Rotation) error {
	if s == nil {
		return fmt.Errorf("rotation: no store")
	}
	path, err := s.path(r.Agent)
	if err != nil {
		return err
	}
	if strings.TrimSpace(r.At) == "" {
		r.At = time.Now().Format(time.RFC3339)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("rotation: marshal: %w", err)
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("rotation: create store dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("rotation: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rotation: commit: %w", err)
	}
	return nil
}

// Get returns the last rotation for an agent. ok=false means none.
func (s *RotationStore) Get(agent string) (Rotation, bool, error) {
	if s == nil {
		return Rotation{}, false, nil
	}
	path, err := s.path(agent)
	if err != nil {
		return Rotation{}, false, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Rotation{}, false, nil
	}
	if err != nil {
		return Rotation{}, false, fmt.Errorf("rotation: read %q: %w", agent, err)
	}
	var r Rotation
	if err := json.Unmarshal(data, &r); err != nil {
		return Rotation{}, false, fmt.Errorf("rotation: parse %q: %w", agent, err)
	}
	if strings.TrimSpace(r.Agent) == "" {
		r.Agent = agent
	}
	return r, true, nil
}

// Observe fills SinceLastRotation / HasLastRotation from disk at now.
func (s *RotationStore) Observe(agent string, now time.Time) (since time.Duration, ok bool) {
	r, found, err := s.Get(agent)
	if err != nil || !found {
		return 0, false
	}
	at, tok := r.Time()
	if !tok {
		return 0, false
	}
	if now.IsZero() {
		now = time.Now()
	}
	return now.Sub(at), true
}
