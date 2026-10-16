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

// TestT974WatchAgentLoopIsGatedOnDevelopmentDaemon is a source ratchet:
// the mutual-supervision go-routine must only start for the development
// daemon. An isolate that always called WatchAgentLoop raised
// supervisor-gap ~90–120s after boot (🎯T974).
func TestT974WatchAgentLoopIsGatedOnDevelopmentDaemon(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		// Tests may run with cwd = package dir or module root.
		src, err = os.ReadFile(filepath.Join("cmd", "jevonsd", "main.go"))
	}
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "config.IsDevelopmentDaemon(cfg.Port, cfg.StateDir)") {
		t.Fatal("main.go must gate WatchAgentLoop on config.IsDevelopmentDaemon (🎯T974)")
	}
	// The unconditional form must not reappear beside the gate.
	if strings.Contains(body, "go supervise.WatchAgentLoop(ctx") &&
		!strings.Contains(body, "if config.IsDevelopmentDaemon") {
		t.Fatal("WatchAgentLoop must not be started unconditionally")
	}
}

// TestT974IsolateIsNotDevelopmentDaemon pins the journey-suite shape:
// throwaway state_dir + non-13705 port is never the development daemon.
func TestT974IsolateIsNotDevelopmentDaemon(t *testing.T) {
	isolate := t.TempDir()
	for _, port := range []int{0, config.JourneyPort, 34567} {
		if config.IsDevelopmentDaemon(port, isolate) {
			t.Fatalf("port %d + isolate state_dir must not be development", port)
		}
	}
	if !config.IsDevelopmentDaemon(config.DevelopmentPort, config.Default().StateDir) {
		t.Fatal("development port + daily state_dir must be development")
	}
}
