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

// DefaultForPurpose maps a declared purpose onto a role when spawn omits
// role=. The agent name does not decide.
func DefaultForPurpose(purpose, agentName string) string {
	_ = agentName
	switch Normalize(purpose) {
	case Overseer:
		return Overseer
	case Aside:
		return Aside
	case ProductOwner:
		return ProductOwner
	case Boss:
		return Boss
	case Auditor:
		return Auditor
	default:
		return Worker
	}
}

// OfSeat is a seat's declared role: the explicit role when one is recorded,
// else the role its declared purpose implies (🎯T766.4). Claudia's purpose
// is the work|aside|overseer enum, so a product owner or boss is only ever
// named by its role. The seat name never decides.
func OfSeat(role, purpose string) string {
	if r := Normalize(role); r != "" {
		return r
	}
	return DefaultForPurpose(purpose, "")
}

// DoctrineMarker opens the role-doctrine section Assemble injects between
// the universal brief and the mission. StripDoctrine is its inverse.
const DoctrineMarker = "[Jevons role doctrine]"

// Assemble builds spawn instructions: universal brief + role body + mission.
// Role body is wrapped with a marker so callers can detect role injection.
func Assemble(universal, roleBody, mission string) string {
	var parts []string
	if u := strings.TrimSpace(universal); u != "" {
		parts = append(parts, u)
	}
	if b := strings.TrimSpace(roleBody); b != "" {
		parts = append(parts, DoctrineMarker+"\n\n"+b)
	}
	if m := strings.TrimSpace(mission); m != "" {
		parts = append(parts, m)
	}
	return strings.Join(parts, "\n\n")
}

// StripDoctrine removes the role-doctrine section Assemble injected ahead of
// a mission, given the same role body, and returns the mission that follows
// it. Text that does not open with the marker is returned unchanged with
// ok=true. Text that opens with the marker but whose doctrine cannot be
// bounded — the body is unknown or differs from what was injected — returns
// ok=false: the caller cannot tell where the doctrine ends and the sender's
// own words begin (🎯T658).
func StripDoctrine(text, roleBody string) (mission string, ok bool) {
	rest := strings.TrimLeft(text, " \t\r\n")
	if !strings.HasPrefix(rest, DoctrineMarker) {
		return text, true
	}
	rest = strings.TrimLeft(rest[len(DoctrineMarker):], " \t\r\n")
	body := strings.TrimSpace(roleBody)
	if body == "" || !strings.HasPrefix(rest, body) {
		return "", false
	}
	return strings.TrimLeft(rest[len(body):], " \t\r\n"), true
}

// RepoRelPath is the repository-relative path shown in the T425 identity
// header for a resolved role definition.
func RepoRelPath(d Def) string {
	if p := strings.TrimSpace(d.Path); p != "" {
		if strings.HasPrefix(p, "builtin/") {
			return "internal/roles/" + p
		}
		return p
	}
	if d.Name != "" {
		return "internal/roles/builtin/" + d.Name + ".md"
	}
	return ""
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
