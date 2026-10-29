// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package proclist

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// workspaceWriteProfile is the seatbelt shape Codex workspace-write uses:
// default-allow, then deny all file writes except temp. /bin/ps is setuid
// and exec of a setuid binary is denied; in-process sysctl is not.
const workspaceWriteProfile = `(version 1)
(allow default)
(deny file-write*)
(allow file-write* (subpath "/private/tmp") (subpath "/tmp") (subpath "/private/var/folders"))
`

func TestT1232SandboxExecListsOwnPID(t *testing.T) {
	if os.Getenv("T1232_SANDBOXED") == "1" {
		procs, err := All()
		if err != nil {
			t.Fatalf("All: %v", err)
		}
		me := os.Getpid()
		for _, p := range procs {
			if p.PID == me {
				if len(procs) < 1 {
					t.Fatal("empty listing")
				}
				fmt.Printf("OK count=%d pid=%d ppid=%d\n", len(procs), p.PID, p.PPID)
				return
			}
		}
		t.Fatalf("missing self pid=%d count=%d", me, len(procs))
	}

	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not on PATH")
	}

	ps := exec.Command("sandbox-exec", "-p", workspaceWriteProfile, "/bin/ps", "-p", "1")
	if out, err := ps.CombinedOutput(); err == nil {
		t.Fatalf("control: sandboxed /bin/ps succeeded; the profile does not reproduce the T1232 deny: %s", out)
	} else if !deniedSetuid(string(out), err) {
		t.Fatalf("control: sandboxed /bin/ps failed for the wrong reason: err=%v out=%s", err, out)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sandbox-exec", "-p", workspaceWriteProfile, exe,
		"-test.run=^TestT1232SandboxExecListsOwnPID$", "-test.v", "-test.count=1")
	cmd.Env = append(os.Environ(), "T1232_SANDBOXED=1", "JEVONS_LIVE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sandboxed proclist: %v\n%s", err, out)
	}
	got := string(out)
	if !strings.Contains(got, "OK count=") {
		t.Fatalf("sandboxed probe output missing OK: %s", got)
	}
	if i := strings.Index(got, "OK count="); i >= 0 {
		n, _ := strconv.Atoi(strings.TrimRight(got[i+len("OK count="):], "0123456789"))
		_ = n
	}
	if !strings.Contains(got, "pid=") {
		t.Fatalf("sandboxed listing did not name a pid: %s", got)
	}
}

func deniedSetuid(out string, err error) bool {
	s := strings.ToLower(out + err.Error())
	return strings.Contains(s, "operation not permitted") || strings.Contains(s, "not permitted")
}
