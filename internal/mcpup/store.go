// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package mcpup is jevonsd's host side of Claudia's HTTP MCP proxy:
// durable OAuth token storage and boot wiring so Session backends reach
// owner-map HTTP MCP only through loopback (🎯T520).
package mcpup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/marcelocantos/claudia"
)

// Store persists MCP OAuth tokens under state_dir. Claudia keeps tokens
// in memory only; this file is the host's durable copy for refresh
// across daemon restarts (🎯T520).
type Store struct {
	path string

	mu   sync.Mutex
	byName map[string]claudia.MCPToken
}

// OpenStore loads tokens from path (missing file → empty store).
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, byName: map[string]claudia.MCPToken{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("mcpup store: read: %w", err)
	}
	if len(b) == 0 {
		return s, nil
	}
	var doc map[string]claudia.MCPToken
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("mcpup store: parse %s: %w", path, err)
	}
	for k, v := range doc {
		s.byName[k] = v
	}
	return s, nil
}

// Get returns a copy of the token for name, or nil.
func (s *Store) Get(name string) *claudia.MCPToken {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, ok := s.byName[name]
	if !ok {
		return nil
	}
	cp := tok
	return &cp
}

// Put writes tok for name and fsyncs the store file.
func (s *Store) Put(name string, tok *claudia.MCPToken) error {
	if name == "" || tok == nil {
		return fmt.Errorf("mcpup store: name and token required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byName[name] = *tok
	return s.flushLocked()
}

func (s *Store) flushLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("mcpup store: mkdir: %w", err)
	}
	b, err := json.MarshalIndent(s.byName, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("mcpup store: write: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("mcpup store: rename: %w", err)
	}
	return nil
}

// DefaultPath is state_dir/mcp_oauth_tokens.json.
func DefaultPath(stateDir string) string {
	return filepath.Join(stateDir, "mcp_oauth_tokens.json")
}
