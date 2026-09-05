// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReactUpgradeReloadsObsoleteLoadedArguments(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Fatal(err)
	}
	script := readRepo(t, "scripts/restart-daily-jevonsd.sh")
	start := strings.Index(script, "start_or_adopt_daemon() {")
	if start < 0 {
		t.Fatal("missing production adoption function")
	}
	end := strings.Index(script[start:], "\n}\n")
	if end < 0 {
		t.Fatal("unterminated adoption function")
	}
	function := script[start : start+end+3]
	for _, legacy := range []string{"false", "true"} {
		t.Run(legacy, func(t *testing.T) {
			dir := t.TempDir()
			plistDir := filepath.Join(dir, "Library", "LaunchAgents")
			if err := os.MkdirAll(plistDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(plistDir, "com.marcelocantos.jevonsd.plist"), []byte(dir+"/bin/jevonsd"), 0o644); err != nil {
				t.Fatal(err)
			}
			fake := "#!/bin/sh\nprintf 'install %s\\n' \"$*\" >>\"$CALLS\"\n"
			if err := os.WriteFile(filepath.Join(dir, "bin", "jevonsd"), []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
			calls := filepath.Join(dir, "calls")
			harness := `set -e
ROOT="$HOME"
BIN="$ROOT/bin/jevonsd"
log() { :; }
die() { echo "$*" >&2; exit 1; }
start_daemon_detached() { echo unexpected-detached >>"$CALLS"; exit 1; }
launchctl() {
  printf 'launchctl %s\n' "$*" >>"$CALLS"
  if [ "$1" = print ]; then
    if [ "$LEGACY" = true ]; then echo 'arguments = -port 13705 -vanilla-port 0'; else echo 'arguments = -port 13705'; fi
  fi
}
` + function + "\nstart_or_adopt_daemon\n"
			cmd := exec.Command("bash", "-c", harness)
			cmd.Env = append(os.Environ(), "HOME="+dir, "CALLS="+calls, "LEGACY="+legacy)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("adopt: %v\n%s", err, out)
			}
			out, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			text := string(out)
			if legacy == "true" {
				install, unload, load := strings.Index(text, "install -install-daemon-agent"), strings.Index(text, "launchctl bootout"), strings.Index(text, "launchctl bootstrap")
				if install < 0 || unload <= install || load <= unload || strings.Contains(text, "kickstart") {
					t.Fatalf("obsolete arguments were not replaced in order:\n%s", text)
				}
			} else if !strings.Contains(text, "launchctl kickstart") || strings.Contains(text, "install ") || strings.Contains(text, "bootout") {
				t.Fatalf("current job unnecessarily rewritten:\n%s", text)
			}
		})
	}
}

func TestReactUpgradeUsesOnlyTheSupervisorOwningTheListener(t *testing.T) {
	script := readRepo(t, "scripts/restart-daily-jevonsd.sh")
	start := strings.Index(script, "upgrade_with_supervisor() {")
	if start < 0 {
		t.Fatal("missing production supervisor upgrade function")
	}
	end := strings.Index(script[start:], "\n}\n")
	if end < 0 {
		t.Fatal("unterminated production function")
	}
	function := script[start : start+end+3]
	for _, tc := range []struct {
		name, port, pid, listener string
		handled                   bool
	}{
		{"owned", "13705", "42", "42", true},
		{"foreign", "13705", "42", "43", false},
		{"isolate", "15333", "42", "42", false},
		{"stopped", "13705", "0", "42", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := filepath.Join(t.TempDir(), "calls")
			harness := `set -e
DAILY_PORT=13705
STOP_WAIT_SEC=1
log() { :; }
die() { echo "$*" >&2; exit 1; }
kill() { return 1; }
list_listen_pids() { echo "$LISTENER"; }
supervisorctl() {
 if [ "$1" = pid ]; then echo "$MANAGED_PID"; else printf '%s\n' "$*" >>"$CALLS"; fi
}
` + function + "\nif upgrade_with_supervisor; then echo handled; else echo fallback; fi\n"
			cmd := exec.Command("bash", "-c", harness)
			cmd.Env = append(os.Environ(), "PORT="+tc.port, "MANAGED_PID="+tc.pid, "LISTENER="+tc.listener, "CALLS="+calls)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("upgrade: %v\n%s", err, out)
			}
			body, readErr := os.ReadFile(calls)
			if tc.handled {
				if string(out) != "handled\n" || readErr != nil || string(body) != "signal HUP jevonsd\n" {
					t.Fatalf("owned upgrade not routed through HUP: %s %s %v", out, body, readErr)
				}
			} else if string(out) != "fallback\n" || !os.IsNotExist(readErr) {
				t.Fatalf("foreign/isolate service touched: %s %s %v", out, body, readErr)
			}
		})
	}
}
