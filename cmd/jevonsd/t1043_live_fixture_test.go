// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// t1043Fixture is a preflight contract, NOT a running-daemon oracle. A full
// jevonsd boot starts an overseer and several ambient loops even when frontier
// consumption is disabled. Until all those effects and Claudia's broker/sidecar
// handshake can be confined, no opt-in flag may launch a test daemon.
type t1043Fixture struct {
	root, home, state, xdg, sessions, sock, config string
	port                                           int
}

func newT1043Fixture(t *testing.T) t1043Fixture {
	t.Helper()
	root := t.TempDir()
	sock, err := os.MkdirTemp("/tmp", "t1043-") // Unix socket path limit
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sock) })
	f := t1043Fixture{root: root, home: filepath.Join(root, "home"), state: filepath.Join(root, "state"), xdg: filepath.Join(root, "xdg"), sessions: filepath.Join(root, "sessions"), sock: sock}
	for _, path := range []string{f.home, f.state, f.xdg, f.sessions} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.port = ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	f.config = fmt.Sprintf("owner_name: Fixture\noverseer_name: fixture-overseer\nstate_dir: %q\nworkdir: %q\nfrontier_consume:\n  disabled: true\n", f.state, f.root)
	return f
}

func (f t1043Fixture) validate() error {
	if f.port == 13705 || f.port <= 0 || f.port > 65535 {
		return fmt.Errorf("unsafe listener port %d", f.port)
	}
	for _, dir := range []string{f.home, f.state, f.xdg, f.sessions, f.sock} {
		if !filepath.IsAbs(dir) || dir == os.Getenv("HOME") || strings.HasPrefix(dir, filepath.Join(os.Getenv("HOME"), ".jevons")) {
			return fmt.Errorf("unsafe fixture directory %q", dir)
		}
	}
	for _, path := range []string{filepath.Join(f.sock, "broker.sock"), filepath.Join(f.sock, "omp.sock"), filepath.Join(f.sock, "tmux.sock")} {
		if len(path) >= 100 {
			return fmt.Errorf("Unix socket path too long: %q", path)
		}
	}
	if !strings.Contains(f.config, "  disabled: true") || !strings.Contains(f.config, "state_dir: "+fmt.Sprintf("%q", f.state)) {
		return fmt.Errorf("fixture config does not isolate state and disable frontier")
	}
	return nil
}

// This is deliberately not a green LIVE claim. The preflight runs in normal
// hermetic suites; the opt-in remains blocked rather than falling back to a
// real provider or the owner's daemon when a fake handshake cannot be proven.
func TestT1043LiveFixturePreflight(t *testing.T) {
	f := newT1043Fixture(t)
	if err := f.validate(); err != nil {
		t.Fatal(err)
	}
	t.Run("reject development port", func(t *testing.T) {
		bad := f
		bad.port = 13705
		if bad.validate() == nil {
			t.Fatal("accepted development listener")
		}
	})
	t.Run("reject owner HOME", func(t *testing.T) {
		bad := f
		bad.home = os.Getenv("HOME")
		if bad.validate() == nil {
			t.Fatal("accepted owner HOME")
		}
	})
}

// Even with this opt-in, do not boot: a valid path/port preflight alone does
// not confine startup's global side effects or prove fake-provider compatibility.
// The subsequent implementation must replace this Skip with a process-group
// and detached-sidecar reaper, fake broker handshake, isolated HOME/XDG,
// disabled ambient loops and a same-daemon API admission+PID observation.
func TestT1043IsolatedLiveAdmission(t *testing.T) {
	if os.Getenv("JEVONS_T1043_ISOLATED_LIVE") != "1" {
		t.Skip("set JEVONS_T1043_ISOLATED_LIVE=1 only after isolation is proven")
	}
	f := newT1043Fixture(t)
	if err := f.validate(); err != nil {
		t.Fatal(err)
	}
	t.Skip("unsafe to boot: overseer/ambient side effects and fake Claudia provider handshake not yet isolated; no LIVE claim")
}
