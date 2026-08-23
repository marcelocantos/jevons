// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/roles"
)

// OpenRoleAssignments loads state_dir/agent_roles.json (🎯T536.2).
func (s *Server) OpenRoleAssignments(stateDir string) error {
	if s == nil {
		return fmt.Errorf("server is nil")
	}
	path := roles.DefaultAssignmentsPath(stateDir)
	a, err := roles.OpenAssignments(path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.roleAssignments = a
	s.roleCat = defaultRolesCatalog()
	s.mu.Unlock()
	return nil
}

// SetRoleAssignments installs a test/hermetic assignments store.
func (s *Server) SetRoleAssignments(a *roles.Assignments) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.roleAssignments = a
	if s.roleCat.OwnerDir == "" && s.roleCat.OverrideDir == "" && s.roleCat.RepoDir == "" {
		s.roleCat = defaultRolesCatalog()
	}
	s.mu.Unlock()
}

func defaultRolesCatalog() roles.Catalog {
	override := strings.TrimSpace(os.Getenv("JEVONS_ROLES_DIR"))
	if override == "" {
		if home, err := os.UserHomeDir(); err == nil {
			override = filepath.Join(home, ".jevons", "roles")
		}
	}
	return roles.Catalog{OwnerDir: override}
}

// rolesCatalog returns the resolver, optionally with a repo-local overlay.
func (s *Server) rolesCatalog(workdir string) roles.Catalog {
	c := defaultRolesCatalog()
	if s != nil {
		s.mu.Lock()
		if s.roleCat.OwnerDir != "" || s.roleCat.OverrideDir != "" || s.roleCat.RepoDir != "" {
			c = s.roleCat
		}
		s.mu.Unlock()
	}
	if wd := strings.TrimSpace(workdir); wd != "" && c.RepoDir == "" {
		candidate := filepath.Join(wd, "internal", "config", "roles")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			c.RepoDir = candidate
		}
	}
	return c
}

// resolveRoleDef looks up a role definition (override > builtin).
func (s *Server) resolveRoleDef(role string) (roles.Def, error) {
	return s.rolesCatalog("").Resolve(role)
}

// roleDisplay is the agent_list role= column (🎯T536.2).
func (s *Server) roleDisplay(d claudia.AgentDef) string {
	if r := strings.TrimSpace(d.Role); r != "" {
		return roles.Normalize(r)
	}
	return s.agentRole(d.Name, d.Purpose)
}

// agentRole returns the recorded role for name, or a purpose/name default.
func (s *Server) agentRole(name, purpose string) string {
	if s != nil {
		s.mu.Lock()
		a := s.roleAssignments
		s.mu.Unlock()
		if a != nil {
			if r := a.Get(name); r != "" {
				return r
			}
		}
	}
	return roles.DefaultForPurpose(purpose, name)
}

// recordAgentRole persists spawn role= alongside AgentDef.Role.
func (s *Server) recordAgentRole(name, role string) error {
	if s == nil || strings.TrimSpace(name) == "" {
		return nil
	}
	s.mu.Lock()
	a := s.roleAssignments
	s.mu.Unlock()
	if a == nil {
		a, _ = roles.OpenAssignments("")
		s.mu.Lock()
		if s.roleAssignments == nil {
			s.roleAssignments = a
		} else {
			a = s.roleAssignments
		}
		s.mu.Unlock()
	}
	return a.Set(name, role)
}

// clearAgentRole drops the assignment when an agent is killed/reaped.
func (s *Server) clearAgentRole(name string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	a := s.roleAssignments
	s.mu.Unlock()
	if a == nil {
		return
	}
	_ = a.Remove(name)
}
