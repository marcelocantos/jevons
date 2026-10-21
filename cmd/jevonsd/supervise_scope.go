// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import "github.com/marcelocantos/jevons/internal/config"

// supervisesOwnerWatchdog reports whether this daemon is the one 🎯T405
// makes responsible for the owner's watchdog LaunchAgent
// (com.marcelocantos.jevons-watchdog under $HOME/Library/LaunchAgents).
//
// That job probes the development port and restarts the development
// daemon, so only the development daemon — port 13705 and state_dir
// ~/.jevons together — may observe or reinstate it. Either one alone is an
// isolate: a journey on a scratch port, or a hand-run daemon on 13705
// with a throwaway state dir. Before 🎯T1090 every daemon ran the loop,
// and an isolate told the owner their real watchdog had a gap.
func supervisesOwnerWatchdog(port int, stateDir string) bool {
	return port == config.DevelopmentPort && config.IsDailyStateDir(stateDir)
}
