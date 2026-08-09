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

// Store persists pending handovers on disk (🎯T285).
//
// The ordering it exists to protect: resolve the predecessor's transcript
// path, PERSIST it, then rotate the agent onto a new session. Rotation
// overwrites the registry's session id, so after it the pointer exists
// nowhere else; a crash between rotation and a successful launch would
// otherwise strand the successor with no way back to its own history.
type Store struct {
	dir string
}

// NewStore roots a store at dir (conventionally <state_dir>/handover).
func NewStore(dir string) *Store { return &Store{dir: dir} }

// path is the per-agent record file. Agent names come from the registry
// (and are used as filenames elsewhere), but a name carrying a separator
// would escape the directory, so refuse rather than sanitise: a silently
// renamed brief is a brief nobody finds.
func (s *Store) path(agent string) (string, error) {
	name := strings.TrimSpace(agent)
	if name == "" {
		return "", fmt.Errorf("handover: agent name is required")
	}
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", fmt.Errorf("handover: unusable agent name %q", agent)
	}
	return filepath.Join(s.dir, name+".json"), nil
}

// Put writes a pending handover with atomic write-and-rename, stamping
// CreatedAt when the caller left it blank.
func (s *Store) Put(p Pending) error {
	path, err := s.path(p.Agent)
	if err != nil {
		return err
	}
	if p.CreatedAt == "" {
		p.CreatedAt = time.Now().Format(time.RFC3339)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("handover: marshal record: %w", err)
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("handover: create store dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("handover: write record: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("handover: commit record: %w", err)
	}
	return nil
}

// Get returns the pending handover for an agent. ok=false means none is
// waiting, which is the normal case for an agent that is not mid-switch.
// A record that cannot be read or parsed is an error, never a silent
// "none": losing a handover quietly is the failure this package guards.
func (s *Store) Get(agent string) (Pending, bool, error) {
	path, err := s.path(agent)
	if err != nil {
		return Pending{}, false, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Pending{}, false, nil
	}
	if err != nil {
		return Pending{}, false, fmt.Errorf("handover: read record for %q: %w", agent, err)
	}
	var p Pending
	if err := json.Unmarshal(data, &p); err != nil {
		return Pending{}, false, fmt.Errorf("handover: parse record for %q (%s): %w", agent, path, err)
	}
	return p, true, nil
}

// MarkDelivered records that the successor received its seed, so a
// migration resumed after a crash does not seed it a second time.
func (s *Store) MarkDelivered(agent string) error {
	p, ok, err := s.Get(agent)
	if err != nil || !ok {
		return err
	}
	p.Delivered = true
	return s.Put(p)
}

// Clear removes a record once it is no longer needed. A missing file is
// success: the post-condition is "no pending handover".
func (s *Store) Clear(agent string) error {
	path, err := s.path(agent)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("handover: clear record for %q: %w", agent, err)
	}
	return nil
}
