// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package uidaemon is the 🎯T540.4 daily UI LaunchAgent pair:
// React on :13705 and vanilla on :13706. Vite :5173 is not a standing job.
package uidaemon

import (
	"path/filepath"
	"strconv"

	"github.com/marcelocantos/jevons/internal/config"
)

// ReactLabel is the launchd job that supervises the React daily surface.
// It must not KeepAlive jevonsd — that is the brew reclaim hazard 🎯T405
// already refused. The job is a StartInterval probe.
const ReactLabel = "com.marcelocantos.jevons-ui"

// VanillaLabel is the KeepAlive UI-only vanilla reference on DailyVanillaPort.
const VanillaLabel = "com.marcelocantos.jevons-ui-vanilla"

// ReactProbeInterval is how often launchd runs the React document probe.
const ReactProbeInterval = 60

// Spec is everything both plists need at install time.
type Spec struct {
	Binary   string
	Home     string
	StateDir string
	PathEnv  string
	Upstream string
}

func reactPlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", ReactLabel+".plist")
}

func vanillaPlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", VanillaLabel+".plist")
}

func defaultUpstream() string {
	return "127.0.0.1:" + strconv.Itoa(config.DailyPort)
}
