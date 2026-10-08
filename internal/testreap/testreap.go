// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package testreap kills the processes a test leaves behind, even when the
// test binary dies without running its cleanups (🎯T932).
//
// t.Cleanup is not enough on its own. When -timeout fires, the testing
// package panics out of a timer goroutine and exits without running a single
// cleanup; a gate that SIGKILLs the binary runs nothing at all. Children the
// test started — and anything they detached into a session of their own —
// are reparented to launchd and live for the machine's uptime. 25 fake
// sidecars and 4 throwaway daemons were found that way on 2026-09-29.
//
// Arm starts a watcher outside the test's process group that holds the read
// end of a pipe whose write end only the test binary has. The kernel closes
// that end however the binary exits — return, panic, timeout, SIGKILL — and
// the watcher then kills every process whose argv names the test's temp
// root. Matching on argv rather than on a process group is deliberate: the
// fixtures under test detach their daemons (cmd/detach, the claudia broker's
// seats) precisely so that they escape the group of whoever started them.
package testreap

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// cleanupWait bounds how long a passing test waits for the watcher's sweep.
const cleanupWait = 10 * time.Second

// watcherScript blocks until stdin reaches EOF — the test binary is gone, or
// its cleanup closed the pipe — then kills everything matching $1. `exec`
// matters: pkill never signals itself, but it would signal a shell whose own
// argv carries the pattern before it finished the sweep.
// The pattern begins with an absolute temp path, never a dash. Do not use
// GNU's `--` terminator: macOS pkill rejects it, silently disabling the
// emergency sweep on this machine.
const watcherScript = `read -r _ ; exec "$1" -KILL -f "$2"`

// Arm arranges that every process whose argv names the test's temp root is
// killed when the test ends, pass, fail, or the binary dying under it. It
// returns that root (with a trailing separator): the parent of every
// t.TempDir() this test hands out.
//
// Call it before starting any child. Fixtures should still stop their own
// children in t.Cleanup; Arm is the sweep that also covers the paths where no
// cleanup runs, and the children that detached out of reach.
func Arm(t testing.TB) string {
	t.Helper()
	pkill, err := exec.LookPath("pkill")
	if err != nil {
		t.Fatalf("testreap: no pkill to sweep this test's processes with: %v", err)
	}
	root := filepath.Dir(t.TempDir()) + string(os.PathSeparator)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("testreap: pipe: %v", err)
	}
	watcher := exec.Command("/bin/sh", "-c", watcherScript, "testreap", pkill, regexp.QuoteMeta(root))
	watcher.Stdin = r
	// Its own process group: a Ctrl-C to the terminal, or a gate that kills
	// the test's group, must not take the watcher down before it sweeps.
	watcher.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := watcher.Start(); err != nil {
		r.Close()
		w.Close()
		t.Fatalf("testreap: start watcher: %v", err)
	}
	// Only the watcher may hold the read end. The write end is close-on-exec,
	// so no child the test starts inherits it and keeps the pipe open.
	r.Close()
	done := make(chan struct{})
	go func() { _ = watcher.Wait(); close(done) }()

	t.Cleanup(func() {
		w.Close()
		select {
		case <-done:
		case <-time.After(cleanupWait):
			t.Errorf("testreap: watcher did not finish sweeping %s within %s", root, cleanupWait)
		}
	})
	return root
}

// ReapUnder stops detached fixture processes by executable path, not parent
// process or current listener. A bounce can leave a previous daemon alive
// without a port, and macOS ps comm truncates long executable paths. Arm
// remains the fallback when the test binary exits without running cleanups.
func ReapUnder(t testing.TB, dir string) {
	t.Helper()
	prefix := dir + string(os.PathSeparator)
	live := func() []int {
		out, err := exec.Command("ps", "-Ao", "pid=,args=").Output()
		if err != nil {
			t.Errorf("testreap: census %s: %v", dir, err)
			return nil
		}
		var pids []int
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) < 2 || !strings.HasPrefix(fields[1], prefix) {
				continue
			}
			pid, err := strconv.Atoi(fields[0])
			if err == nil && pid != os.Getpid() {
				pids = append(pids, pid)
			}
		}
		return pids
	}
	for range 10 {
		pids := live()
		if len(pids) == 0 {
			return
		}
		for _, pid := range pids {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if left := live(); len(left) > 0 {
		t.Errorf("testreap: leaked processes from %s: %v", dir, left)
	}
}
