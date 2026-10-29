// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/roles"
)

// overseerRolesCatalog resolves the overseer role file for this daemon
// (owner override > builtin). 🎯T511 phase 5 / 🎯T44: role doctrine is not
// duplicated in persona.md; Persona appends it once at render time.
func (c Config) overseerRolesCatalog() roles.Catalog {
	override := strings.TrimSpace(os.Getenv("JEVONS_ROLES_DIR"))
	if override == "" && strings.TrimSpace(c.StateDir) != "" {
		override = filepath.Join(strings.TrimSpace(c.StateDir), "roles")
	}
	return roles.Catalog{OwnerDir: override}
}

func (c Config) overseerRoleBody() (string, error) {
	def, err := c.overseerRolesCatalog().Resolve(roles.Overseer)
	if err != nil {
		return "", fmt.Errorf("config: overseer role: %w", err)
	}
	return strings.TrimSpace(def.Body), nil
}

// appendOverseerRoleDoctrine adds the authoritative overseer role body after
// the templated persona shell (same marker as fleet spawn assembly).
func appendOverseerRoleDoctrine(shell, roleBody string) string {
	shell = strings.TrimRight(shell, " \t\n")
	body := strings.TrimSpace(roleBody)
	if body == "" {
		return shell + "\n"
	}
	return shell + "\n\n" + roles.DoctrineMarker + "\n\n" + body + "\n"
}
