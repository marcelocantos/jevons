// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/marcelocantos/jevons/internal/portown"
)

// stoppedProcessNote is 🎯T1068: the shape of the 2026-10-12 outage was
// `kill -STOP` against jevonsd. Its listening socket stayed open — a
// LISTEN socket is kernel state, not something the stopped process has
// to keep servicing — so the watchdog's /health probe only ever saw a
// generic "not serving", with nothing in the alarm to tell the owner the
// daemon was frozen rather than crashed, wedged, or simply slow to
// answer. This names the holder PID and its `ps` job-control state so a
// future stop reads as "pid 1234 is STOPPED" instead of a bare timeout.
//
// Empty when no loopback holder is found (the ordinary down-and-crashed
// case 🎯T405 already covers) or when the holder is not actually stopped.
func stoppedProcessNote(port int) string {
	listeners, err := portown.ListLSOF(port)
	if err != nil {
		return ""
	}
	for _, l := range listeners {
		if !portown.IsLoopback(l.Addr) {
			continue
		}
		stopped, stat, err := processIsStopped(l.PID)
		if err != nil || !stopped {
			continue
		}
		cmd := l.Cmd
		if cmd == "" {
			cmd = "?"
		}
		return fmt.Sprintf(
			"process pid %d (%s) is STOPPED (ps stat %q); it still holds the port but cannot answer until it is resumed, e.g. kill -CONT %d.",
			l.PID, cmd, stat, l.PID)
	}
	return ""
}

// processIsStopped reports whether `ps` currently shows pid in BSD
// job-control state T — frozen by SIGSTOP/SIGTSTP (or Ctrl-Z) and unable
// to run again until SIGCONT, as opposed to merely running (R), sleeping
// (S) or waiting on I/O (D), all of which are "slow", not "stopped".
func processIsStopped(pid int) (bool, string, error) {
	stat, err := psStat(pid)
	if err != nil {
		return false, "", err
	}
	return isStoppedStat(stat), stat, nil
}

// psStat shells out to the one place BSD/macOS expose process state.
func psStat(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isStoppedStat is the pure classifier: a `ps -o stat=` code's first
// character is the primary state, T meaning job-control-stopped. Trailing
// modifiers (+, <, s, N, …) never change that first character's meaning,
// so they are not consulted.
func isStoppedStat(stat string) bool {
	stat = strings.TrimSpace(stat)
	return len(stat) > 0 && stat[0] == 'T'
}
