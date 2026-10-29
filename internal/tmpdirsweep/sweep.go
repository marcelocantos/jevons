// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package tmpdirsweep removes stale directories under the system temp dir
// when the process that created them is gone (🎯T1168). SIGKILL and gate
// timeouts skip normal teardown; the next run of the same suite sweeps them.
package tmpdirsweep

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// MinStaleAge is how old a directory must be before SweepPrefixes treats it
// as a dead run's. Generous: the sweep is cleanup, and a tree spared once is
// swept by a later run.
const MinStaleAge = 30 * time.Minute

// SweepPrefixes removes immediate children of tmpDir whose names begin with
// any of prefixes. A directory is removed only when no live process names it
// in argv (pgrep -f). prepare runs on each candidate before removal — for
// example stopping a private tmux server so its socket does not pin the tree.
func SweepPrefixes(tmpDir string, prefixes []string, prepare func(string)) {
	if tmpDir == "" || len(prefixes) == 0 {
		return
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return
	}
	pgrep, err := exec.LookPath("pgrep")
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !hasPrefix(name, prefixes) {
			continue
		}
		dir := filepath.Join(tmpDir, name)
		// 🎯T1302: a concurrent suite's state dir exists before any process
		// names it — its daemon has not started yet — and three journey
		// shards starting together swept each other's. A killed run's tree
		// is never this young.
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) < MinStaleAge {
			continue
		}
		if alive(pgrep, dir) {
			continue
		}
		if prepare != nil {
			prepare(dir)
		}
		_ = os.RemoveAll(dir)
	}
}

func hasPrefix(name string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func alive(pgrep string, dir string) bool {
	out, err := exec.Command(pgrep, "-f", regexp.QuoteMeta(dir)).Output()
	if err != nil && len(out) == 0 {
		return false
	}
	return len(strings.Fields(string(out))) > 0
}

// StopOrphans sends SIGTERM to processes whose parent is gone (ppid 1) and
// whose executable lives in an immediate child of tmpDir named with one of
// prefixes (🎯T1294). An isolate daemon or broker copied into a journey tree
// outlives a SIGKILL'd suite; while it runs, SweepPrefixes keeps its tree, and
// on 2026-10-29 such orphans had been running seats and paid audits for 15
// hours. A concurrent run's processes still have their suite as parent, so
// they are never selected. It returns the pids it signalled.
func StopOrphans(tmpDir string, prefixes []string) []int {
	if tmpDir == "" || len(prefixes) == 0 {
		return nil
	}
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,args=").Output()
	if err != nil {
		return nil
	}
	pids := OrphanPIDs(string(out), tmpDir, prefixes)
	for _, pid := range pids {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Signal(syscall.SIGTERM)
		}
	}
	return pids
}

// OrphanPIDs selects StopOrphans' targets from `ps -axo pid=,ppid=,args=`
// output.
func OrphanPIDs(ps, tmpDir string, prefixes []string) []int {
	root := filepath.Clean(tmpDir) + string(filepath.Separator)
	var pids []int
	for _, line := range strings.Split(ps, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[1] != "1" {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		exe := filepath.Clean(f[2])
		if !strings.HasPrefix(exe, root) {
			continue
		}
		tree, _, ok := strings.Cut(strings.TrimPrefix(exe, root), string(filepath.Separator))
		if !ok || !hasPrefix(tree, prefixes) {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}
