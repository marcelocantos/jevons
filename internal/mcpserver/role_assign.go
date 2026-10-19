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
// Role is read from agent_roles.json, the record jevons keeps alongside
// AgentDef.Role (recordAgentRole), with the purpose/name derivation as
// the fallback.
func (s *Server) roleDisplay(d claudia.AgentDef) string {
	return s.agentRole(d.Name, d.Purpose)
}

// agentRole returns name's declared fleet role (🎯T766.4): the role recorded
// at spawn (agent_roles.json, 🎯T511 / 🎯T536.2), else the registry row's
// AgentDef.Role, else the role its declared purpose implies. Claudia's
// purpose is the work|aside|overseer enum, so every live product owner
// carries purpose=work and the recorded role is the fact that says product
// owner. The seat name never decides.
func (s *Server) agentRole(name, purpose string) string {
	if r := s.recordedRole(name); r != "" {
		return r
	}
	defRole := ""
	if s != nil && s.registry != nil {
		if d := s.registry.Def(name); d != nil {
			defRole = d.Role
		}
	}
	return roles.OfSeat(defRole, purpose)
}

// seatRoleOf is agentRole for a registry row. Pure classifiers read
// roles.OfSeat(d.Role, d.Purpose); withSeatRole fills d.Role so they see
// the recorded role too.
func (s *Server) seatRoleOf(d claudia.AgentDef) string {
	if r := s.recordedRole(d.Name); r != "" {
		return r
	}
	return roles.OfSeat(d.Role, d.Purpose)
}

// recordedRole is the role recorded at spawn for name, or "".
func (s *Server) recordedRole(name string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	a := s.roleAssignments
	s.mu.Unlock()
	return a.Get(name)
}

// withSeatRole returns a copy of d whose Role is its declared role.
func (s *Server) withSeatRole(d claudia.AgentDef) claudia.AgentDef {
	d.Role = s.seatRoleOf(d)
	return d
}

// withSeatRoles is withSeatRole over a registry listing.
func (s *Server) withSeatRoles(defs []claudia.AgentDef) []claudia.AgentDef {
	out := make([]claudia.AgentDef, len(defs))
	for i, d := range defs {
		out[i] = s.withSeatRole(d)
	}
	return out
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
