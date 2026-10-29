// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"github.com/marcelocantos/claudia"
)

// LedgerProtectedPathGlob is the sidecar protected-path policy for the intent
// ledger (jevons 🎯T546, claudia 🎯T209).
const LedgerProtectedPathGlob = "**/bullseye.yaml"

// SidecarLedgerProtectedPaths is passed to every OMP sidecar seat at launch.
func SidecarLedgerProtectedPaths() []claudia.ProtectedPathRule {
	return []claudia.ProtectedPathRule{{
		Pattern: LedgerProtectedPathGlob,
		Reason: "refusing Write to bullseye.yaml — agents must not edit bullseye.yaml. " +
			"File, status, achieve, and query go through jevons_target_file or bullseye MCP/CLI " +
			"(`bullseye commit` / `bullseye query`). 🎯T546",
	}}
}
