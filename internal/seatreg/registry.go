// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package seatreg is the jevons-owned agent registry (🎯T866.7).
// The file is still ~/.jevons/agents.json. Launch still calls through
// claudia.Agent so the sidecar stays the seat implementation. This
// package is the construction and path jevonsd uses.
package seatreg

import (
	"path/filepath"

	"github.com/marcelocantos/claudia"
)

// FileName is the registry basename under the jevons state directory.
const FileName = "agents.json"

// Path is the registry file under stateDir (usually ~/.jevons).
func Path(stateDir string) string {
	return filepath.Join(stateDir, FileName)
}

// New loads or creates the registry at path.
func New(path string) (*claudia.Registry, error) {
	return claudia.NewRegistry(path)
}
