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
	script := readRepo(t, "scripts/restart-jevonsd.sh")
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
	script := readRepo(t, "scripts/restart-jevonsd.sh")
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

// extractUpgradeWithSupervisor returns the production upgrade_with_supervisor
// function body from scripts/restart-jevonsd.sh for hermetic bash harnesses.
func extractUpgradeWithSupervisor(t *testing.T) string {
	t.Helper()
	script := readRepo(t, "scripts/restart-jevonsd.sh")
	start := strings.Index(script, "upgrade_with_supervisor() {")
	if start < 0 {
		t.Fatal("missing production supervisor upgrade function")
	}
	end := strings.Index(script[start:], "\n}\n")
	if end < 0 {
		t.Fatal("unterminated production function")
	}
	return script[start : start+end+3]
}

// 🎯T770: a supervised upgrade that outlives the grace window must not
// die with "supervised daemon did not finish upgrade exit". Match the
// non-supervisor path: force-kill, then succeed so supervisord can
// relaunch. The 2026-09-21 specimen logged that ERROR on every bounce
// while supervisord restarted ~30s later.
func TestSupervisedUpgradeForceKillsWedgedDaemon(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Fatal(err)
	}
	function := extractUpgradeWithSupervisor(t)
	calls := filepath.Join(t.TempDir(), "calls")
	// ALIVE stays set through the grace window so kill -0 keeps succeeding;
	// kill -9 clears it. That is the wedged-upgrade shape that used to die.
	harness := `set -e
DAILY_PORT=13705
STOP_WAIT_SEC=1
PORT=13705
ALIVE=1
log() { printf '%s\n' "$*" >>"$CALLS"; }
die() { echo "$*" >&2; exit 1; }
list_listen_pids() { echo 42; }
kill() {
  case "$1" in
    -0)
      if [ "$ALIVE" = 1 ]; then return 0; fi
      return 1
      ;;
    -9)
      printf 'kill -9 %s\n' "$2" >>"$CALLS"
      ALIVE=0
      return 0
      ;;
    *)
      printf 'kill %s\n' "$*" >>"$CALLS"
      return 0
      ;;
  esac
}
supervisorctl() {
  if [ "$1" = pid ]; then echo 42; else printf '%s\n' "$*" >>"$CALLS"; fi
}
` + function + "\nif upgrade_with_supervisor; then echo handled; else echo fallback; fi\n"
	cmd := exec.Command("bash", "-c", harness)
	cmd.Env = append(os.Environ(), "CALLS="+calls)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wedged upgrade must not die: %v\n%s", err, out)
	}
	if string(out) != "handled\n" {
		t.Fatalf("wedged upgrade should stay on the supervisor path, got %q", out)
	}
	body, readErr := os.ReadFile(calls)
	if readErr != nil {
		t.Fatal(readErr)
	}
	text := string(body)
	if !strings.Contains(text, "signal HUP jevonsd") {
		t.Fatalf("expected HUP before force-kill:\n%s", text)
	}
	if !strings.Contains(text, "kill -9 42") {
		t.Fatalf("expected SIGKILL after grace window:\n%s", text)
	}
	if !strings.Contains(text, "🎯T770") {
		t.Fatalf("expected T770 force-kill log:\n%s", text)
	}
	if strings.Contains(text, "did not finish upgrade exit") {
		t.Fatalf("old die message must not appear:\n%s", text)
	}
}

// 🎯T770 negative: a pid that survives SIGKILL is a real failure.
func TestSupervisedUpgradeDiesWhenForceKillFails(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Fatal(err)
	}
	function := extractUpgradeWithSupervisor(t)
	harness := `set -e
DAILY_PORT=13705
STOP_WAIT_SEC=1
PORT=13705
log() { :; }
die() { echo "$*" >&2; exit 1; }
list_listen_pids() { echo 42; }
# kill -0 always succeeds: the process is immortal even after -9.
kill() { return 0; }
supervisorctl() {
  if [ "$1" = pid ]; then echo 42; else :; fi
}
` + function + "\nupgrade_with_supervisor\n"
	cmd := exec.Command("bash", "-c", harness)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("immortal pid must die, got output:\n%s", out)
	}
	if !strings.Contains(string(out), "still alive after SIGKILL") {
		t.Fatalf("expected post-SIGKILL failure, got:\n%s", out)
	}
	if strings.Contains(string(out), "did not finish upgrade exit") {
		t.Fatalf("old grace-window die must not be the failure mode:\n%s", out)
	}
}
