// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package roles implements declarative fleet agent roles (🎯T511 mechanism;
// 🎯T536.2 adds the built-in auditor). An agent is spawned AS a role; "agent"
// stays reserved for instances.
package roles

import (
	"fmt"
	"strings"
)

// Canonical built-in role names. Built-ins cannot be deleted — only overridden.
const (
	Auditor      = "auditor"
	Worker       = "worker"
	ProductOwner = "product-owner"
	Overseer     = "overseer"
	Aside        = "aside"
	Boss         = "boss"
)

// Source labels for List / Resolve.
const (
	SourceBuiltin = "builtin"
	SourceRepo    = "repo"
	SourceOwner   = "owner"
	// SourceOverride is an alias of SourceOwner (legacy wording).
	SourceOverride = SourceOwner
)

// Def is one role definition: frontmatter metadata + instruction body.
type Def struct {
	Name     string
	Purpose  string // default purpose when spawn omits purpose
	ReadOnly bool
	Summary  string
	Body     string
	Source   string // builtin | repo | owner
	Path     string // file path when loaded from disk; empty for embed
}

// BuiltinNames is the guarded set (🎯T511 / 🎯T536.2): cannot be deleted.
func BuiltinNames() []string {
	return []string{Overseer, ProductOwner, Boss, Worker, Aside, Auditor}
}

// IsBuiltin reports whether name is a protected built-in role.
func IsBuiltin(name string) bool {
	n := Normalize(name)
	for _, b := range BuiltinNames() {
		if n == b {
			return true
		}
	}
	return false
}

// Normalize canonicalizes a role name (trim, lower, underscore→hyphen).
func Normalize(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, "_", "-")
	n = strings.ReplaceAll(n, " ", "-")
	switch n {
	case "productowner", "po", "product-owner":
		return ProductOwner
	case "work", "worker", "implementer":
		return Worker
	case "audit", "auditor":
		return Auditor
	}
	return n
}

// DefaultForPurpose maps legacy purpose (🎯T114) onto a role when spawn
// omits role=.
func DefaultForPurpose(purpose, agentName string) string {
	switch strings.TrimSpace(strings.ToLower(purpose)) {
	case "overseer":
		return Overseer
	case "aside":
		return Aside
	}
	n := strings.ToLower(strings.TrimSpace(agentName))
	if strings.HasSuffix(n, "-po") || strings.HasSuffix(n, "_po") || n == "po" {
		return ProductOwner
	}
	return Worker
}

// Assemble builds spawn instructions: universal brief + role body + mission.
// Role body is wrapped with a marker so callers can detect role injection.
func Assemble(universal, roleBody, mission string) string {
	var parts []string
	if u := strings.TrimSpace(universal); u != "" {
		parts = append(parts, u)
	}
	if b := strings.TrimSpace(roleBody); b != "" {
		parts = append(parts, "[Jevons role doctrine]\n\n"+b)
	}
	if m := strings.TrimSpace(mission); m != "" {
		parts = append(parts, m)
	}
	return strings.Join(parts, "\n\n")
}

// DeleteRefused explains why a role cannot be removed.
func DeleteRefused(name string, liveInstances int, force bool) error {
	n := Normalize(name)
	if n == "" {
		return fmt.Errorf("role name is required")
	}
	if IsBuiltin(n) {
		return fmt.Errorf("built-in role %q cannot be deleted (override only)", n)
	}
	if liveInstances > 0 && !force {
		return fmt.Errorf("role %q has %d live instance(s); pass force to remove", n, liveInstances)
	}
	return nil
}
