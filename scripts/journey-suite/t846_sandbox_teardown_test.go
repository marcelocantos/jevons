// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// 🎯T846: a finished journey isolate must leave no private claudia-anchor
// tmux server and no jevons-journey-* temp tree behind. The leak was that
// killPrivateClaudiaTmux + RemoveAll lived only in defers, which exitNow
// (fatal, signal, outage) skips. These oracles pin the exitNow path.

const t846ChildEnv = "JOURNEY_T846_CHILD"

func init() {
	// TestMain in cleanup_test.go owns the child dispatch switch. We
	// piggy-back by handling our modes there... except TestMain is already
	// defined. Instead, re-exec modes live in that TestMain's switch via
	// the shared childEnv. We use a distinct env so the existing switch
	// falls through — so we handle T846 children here via TestT846Helper.
}

// TestT846Helper is the subprocess fixture: arm sandbox cleanup the same
// way main does, start a real claudia-anchor tmux on the private socket,
// then exit through exitNow the way fatal/signal/happy do.
func TestT846Helper(t *testing.T) {
	mode := os.Getenv(t846ChildEnv)
	if mode == "" {
		t.Skip("subprocess fixture for 🎯T846")
	}
	root := os.Getenv("JOURNEY_T846_ROOT")
	if root == "" {
		t.Fatal("JOURNEY_T846_ROOT unset")
	}
	// Reset the package-level cleanups so prior tests cannot bleed in.
	cleanups = exitCleanup{}
	cleanups.Add(func() {
		killPrivateClaudiaTmux(root)
		_ = os.RemoveAll(root)
	})
	if err := startAnchorOn(root); err != nil {
		fmt.Fprintln(os.Stderr, "start anchor:", err)
		os.Exit(97)
	}
	// Prove the anchor is live before we exit — otherwise a green cleanup
	// proves nothing.
	if !anchorAlive(root) {
		fmt.Fprintln(os.Stderr, "anchor never started")
		os.Exit(97)
	}
	switch mode {
	case "fatal":
		fatal(errors.New("t846 simulated failure"))
	case "happy":
		exitNow(0)
	case "signal":
		catchSignals()
		fmt.Println("ready")
		time.Sleep(30 * time.Second)
		os.Exit(99)
	default:
		os.Exit(97)
	}
}

func TestT846ExitNowReclaimsSandboxAndAnchor(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	for _, tc := range []struct {
		mode     string
		signalIt bool
		wantCode int
	}{
		{mode: "happy", wantCode: 0},
		{mode: "fatal", wantCode: 1},
		{mode: "signal", signalIt: true, wantCode: 130},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "jevons-journey-t846")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			// Seed a file so RemoveAll has something to erase.
			if err := os.WriteFile(filepath.Join(root, "marker"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command(os.Args[0], "-test.run=^TestT846Helper$", "-test.count=1")
			cmd.Env = append(os.Environ(),
				t846ChildEnv+"="+tc.mode,
				"JOURNEY_T846_ROOT="+root,
			)

			var code int
			if tc.signalIt {
				stdout, err := cmd.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				buf := make([]byte, len("ready\n"))
				if _, err := stdout.Read(buf); err != nil {
					t.Fatalf("child never ready: %v", err)
				}
				if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
					t.Fatal(err)
				}
				err = cmd.Wait()
				code = exitCode(err)
			} else {
				out, err := cmd.CombinedOutput()
				code = exitCode(err)
				if code == 97 {
					t.Fatalf("fixture failed to stage: %s", out)
				}
			}

			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", code, tc.wantCode)
			}

			deadline := time.Now().Add(5 * time.Second)
			for anchorAlive(root) {
				if time.Now().After(deadline) {
					t.Fatalf("%s: claudia-anchor still alive under %s after exitNow", tc.mode, root)
				}
				time.Sleep(50 * time.Millisecond)
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatalf("%s: sandbox %s still on disk after exitNow (err=%v)", tc.mode, root, err)
			}
		})
	}
}

// TestT846KillPrivateIsIdempotentOnMissingSocket is the no-server common case.
func TestT846KillPrivateIsIdempotentOnMissingSocket(t *testing.T) {
	killPrivateClaudiaTmux(t.TempDir()) // must not panic or error
}

func startAnchorOn(root string) error {
	sock := filepath.Join(root, "claudia", "tmux.sock")
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		return err
	}
	// Same shape claudia's EnsureServer uses.
	cmd := exec.Command("tmux", "-S", sock, "new-session", "-d", "-s", "claudia-anchor")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func anchorAlive(root string) bool {
	sock := filepath.Join(root, "claudia", "tmux.sock")
	if _, err := os.Stat(sock); err != nil {
		// tmux may leave the sock briefly; fall through to list-sessions.
	}
	out, err := exec.Command("tmux", "-S", sock, "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "claudia-anchor" {
			return true
		}
	}
	return false
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
