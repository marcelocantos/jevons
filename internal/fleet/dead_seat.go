// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"strings"

	"github.com/marcelocantos/claudia"
)

// DeadSeatRemovable reports whether a dead non-AutoStart seat of this
// purpose leaves the registry (🎯T544) rather than lingering as a "stopped"
// row in the fleet tree. Only work seats (purpose work or unset, which the
// fleet reads as work) are removed; asides, product owners and the overseer
// keep their rows, since those carry history the owner reopens. Shared by
// the MCP sweep (internal/mcpserver) and the HTTP feed's inline twin
// (internal/server) so the two policies cannot drift.
func DeadSeatRemovable(purpose string) bool {
	purpose = strings.TrimSpace(purpose)
	return purpose == "" || purpose == claudia.PurposeWork
}
