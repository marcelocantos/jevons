// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package uidaemon installs the development React surface probe.
package uidaemon

import (
	"path/filepath"
)

// ReactLabel is the launchd job that supervises the React daily surface.
// It must not KeepAlive jevonsd — that job is com.marcelocantos.jevonsd
// (🎯T553.3). This label is a StartInterval document probe only.
const ReactLabel = "com.marcelocantos.jevons-ui"

// ReactProbeInterval is how often launchd runs the React document probe.
const ReactProbeInterval = 60

// Spec describes the React probe installation.
type Spec struct {
	Binary   string
	Home     string
	StateDir string
	PathEnv  string
}

func reactPlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", ReactLabel+".plist")
}
