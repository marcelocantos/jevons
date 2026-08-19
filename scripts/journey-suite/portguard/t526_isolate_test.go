// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package portguard

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestT526ErrIfPortHeldAbortsBeforeMint replays the 2026-08-19 collision:
// a foreign listener already answers on the isolate port. ErrIfPortHeld must
// refuse before any MCP mint — never adopt the foreign daemon (🎯T526).
func TestT526ErrIfPortHeldAbortsBeforeMint(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	err = ErrIfPortHeld(port)
	if err == nil {
		t.Fatal("ErrIfPortHeld must refuse when a foreign process holds the port")
	}
	msg := err.Error()
	if !strings.Contains(msg, "already in use") || !strings.Contains(msg, "foreign") {
		t.Fatalf("expected collision refuse, got: %v", err)
	}

	// Acceptance (3): a forced collision must not leave a J20 omit row in
	// agents.json — the suite aborts before mint, so a fixture registry stays clean.
	stateDir := t.TempDir()
	agentsPath := filepath.Join(stateDir, "agents.json")
	if err := os.WriteFile(agentsPath, []byte("[]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ErrIfPortHeld(port) == nil {
		t.Fatal("collision still present")
	}
	raw, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "jv-t39015-omit") {
		t.Fatalf("J20 omit fixture must not appear after collision abort: %s", raw)
	}
}

func TestT526ErrIfForeignListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	foreign, err := ListenPID(port)
	if err != nil || foreign <= 0 {
		t.Fatalf("ListenPID: %v pid=%d", err, foreign)
	}
	if err := ErrIfForeignListener(port, foreign+1); err == nil {
		t.Fatal("must flag foreign listener")
	}
	if err := ErrIfForeignListener(port, foreign); err != nil {
		t.Fatalf("same pid must be ok: %v", err)
	}
	if err := ErrIfPortHeld(0); err != nil {
		t.Fatalf("port 0: %v", err)
	}
}
