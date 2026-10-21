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
		name, port, pid, listener, conf string
		handled                         bool
	}{
		{"owned", "13705", "42", "42", "", true},
		{"foreign", "13705", "42", "43", "", false},
		{"isolate", "15333", "42", "42", "", false},
		{"stopped", "13705", "0", "42", "", false},
		// 🎯T1086: a named private supervisord may serve a scratch port.
		{"private-supervisord", "15333", "42", "42", "/scratch/supervisord.conf", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := filepath.Join(t.TempDir(), "calls")
			// sctl, the policy read and the post-exit relaunch check are
			// stubbed: this pins the routing decision. The relaunch itself
			// runs against a real throwaway supervisord in
			// cmd/jevons-watchdog (TestT1086…).
			harness := `set -e
DAILY_PORT=13705
STOP_WAIT_SEC=1
SUPERVISOR_PROGRAM=jevonsd
log() { :; }
die() { echo "$*" >&2; exit 1; }
kill() { return 1; }
list_listen_pids() { echo "$LISTENER"; }
supervisorctl() {
 if [ "$1" = pid ]; then echo "$MANAGED_PID"; else printf '%s\n' "$*" >>"$CALLS"; fi
}
sctl() { supervisorctl "$@"; }
supervisor_policy() { printf 'autorestart=false autostart=false file=/x.ini'; }
ensure_supervised_relaunch() { printf 'relaunch %s\n' "$1" >>"$CALLS"; }
` + function + "\nif upgrade_with_supervisor; then echo handled; else echo fallback; fi\n"
			cmd := exec.Command("bash", "-c", harness)
			cmd.Env = append(os.Environ(), "PORT="+tc.port, "MANAGED_PID="+tc.pid, "LISTENER="+tc.listener, "CALLS="+calls, "SUPERVISOR_CONF="+tc.conf)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("upgrade: %v\n%s", err, out)
			}
			body, readErr := os.ReadFile(calls)
			if tc.handled {
				if string(out) != "handled\n" || readErr != nil || string(body) != "signal HUP jevonsd\nrelaunch 42\n" {
					t.Fatalf("owned upgrade not routed through HUP: %s %s %v", out, body, readErr)
				}
			} else if string(out) != "fallback\n" || !os.IsNotExist(readErr) {
				t.Fatalf("foreign/isolate service touched: %s %s %v", out, body, readErr)
			}
		})
	}
}
