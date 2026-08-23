// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package roles

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Assignments persists agent-name → role for the fleet registry (🎯T536.2).
// Stored beside agents.json so instance→role stays inspectable without a
// claudia AgentDef.Role field on the published pin.
type Assignments struct {
	mu   sync.Mutex
	path string
	m    map[string]string // agent name → role
}

// OpenAssignments loads (or creates empty) assignments at path.
func OpenAssignments(path string) (*Assignments, error) {
	a := &Assignments{path: path, m: map[string]string{}}
	if path == "" {
		return a, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return a, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return a, nil
	}
	if err := json.Unmarshal(b, &a.m); err != nil {
		return nil, fmt.Errorf("agent roles %s: malformed (hard error, not reset): %w", path, err)
	}
	if a.m == nil {
		a.m = map[string]string{}
	}
	return a, nil
}

// Get returns the recorded role for an agent, or "".
func (a *Assignments) Get(agent string) string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return Normalize(a.m[strings.TrimSpace(agent)])
}

// Set records role for agent and persists. Empty role clears.
func (a *Assignments) Set(agent, role string) error {
	if a == nil {
		return fmt.Errorf("assignments store is nil")
	}
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return fmt.Errorf("agent name is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.m == nil {
		a.m = map[string]string{}
	}
	r := Normalize(role)
	if r == "" {
		delete(a.m, agent)
	} else {
		a.m[agent] = r
	}
	return a.saveLocked()
}

// Remove drops an agent (on kill / reap).
func (a *Assignments) Remove(agent string) error {
	return a.Set(agent, "")
}

// CountRole returns how many live assignments use role.
func (a *Assignments) CountRole(role string) int {
	if a == nil {
		return 0
	}
	r := Normalize(role)
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, v := range a.m {
		if Normalize(v) == r {
			n++
		}
	}
	return n
}

func (a *Assignments) saveLocked() error {
	if a.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(a.m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := a.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, a.path)
}

// DefaultAssignmentsPath is state_dir/agent_roles.json.
func DefaultAssignmentsPath(stateDir string) string {
	if strings.TrimSpace(stateDir) == "" {
		return ""
	}
	return filepath.Join(stateDir, "agent_roles.json")
}
