// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/config"
)

// 🎯T1090: the daemon supervises the owner's watchdog LaunchAgent only when
// it is the development daemon. 🎯T405's half of the pair must stay intact
// there; everywhere else the loop must not start.
func TestT1090OnlyTheDevelopmentDaemonSupervisesTheOwnerWatchdog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dev := config.Default().StateDir
	if dev != filepath.Join(home, ".jevons") {
		t.Fatalf("development state dir = %q, want under the scratch HOME", dev)
	}
	for _, tc := range []struct {
		name     string
		port     int
		stateDir string
		want     bool
	}{
		{"development", config.DevelopmentPort, dev, true},
		{"development-trailing-slash", config.DevelopmentPort, dev + "/", true},
		{"journey-isolate", config.JourneyPort, t.TempDir(), false},
		{"scratch-port-dev-state", 15333, dev, false},
		{"dev-port-throwaway-state", config.DevelopmentPort, t.TempDir(), false},
		{"empty-state", config.DevelopmentPort, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := supervisesOwnerWatchdog(tc.port, tc.stateDir); got != tc.want {
				t.Fatalf("supervisesOwnerWatchdog(%d, %q) = %v, want %v", tc.port, tc.stateDir, got, tc.want)
			}
		})
	}
}

// The predicate is only worth anything if main gates the loop on it. A
// source check, because booting a whole daemon to watch a goroutine not
// start would test far more than this.
func TestT1090MainGatesWatchAgentLoop(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	call := strings.Index(s, "supervise.WatchAgentLoop(")
	if call < 0 {
		t.Fatal("main.go no longer starts supervise.WatchAgentLoop; 🎯T405 needs the development daemon to supervise its watchdog")
	}
	if strings.Count(s, "supervise.WatchAgentLoop(") != 1 {
		t.Fatal("more than one supervise.WatchAgentLoop call in main.go; each must be gated by supervisesOwnerWatchdog")
	}
	guard := strings.LastIndex(s[:call], "if supervisesOwnerWatchdog(cfg.Port, cfg.StateDir) {")
	if guard < 0 || strings.Count(s[guard:call], "\n}") > 0 || strings.Count(s[guard:call], "\n\t}") > 0 {
		t.Fatal("supervise.WatchAgentLoop is not directly inside `if supervisesOwnerWatchdog(cfg.Port, cfg.StateDir) {`")
	}
}
