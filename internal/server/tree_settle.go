// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"os"
	"path/filepath"
	"sort"
	"time"
)

// 🎯T375, gap (1): the daily daemon serves web/ from disk while N fleet
// workers write into it, so GET / can read index.html — or a module it
// references — halfway through somebody's write. 🎯T374 answers the "absent"
// half of the acceptance ("files not yet present"); a truncated file is
// present, passes os.Stat, and still hands the cockpit half a module, a
// syntax error, and the boot abort behind the whole cascade. This file
// answers the "not yet complete" half.
//
// Whether an edit is semantically finished is not decidable from outside the
// writer at any cost — that is why the write-side answer is worktree
// isolation (🎯T254.2). What IS decidable from outside is whether the tree is
// still MOVING: stamp the reference set, wait, stamp it again. Paths whose
// stamp changed were being written across the gap. A tree that holds still is
// served normally; a tree still moving when the budget runs out is served
// with a banner naming the moving files — the acceptance's "fails loudly with
// owner-visible recovery" branch, matching the precedent T374 set for absent
// modules (a named, degraded page beats both a silent cascade and a blank
// error screen).

const (
	// settleQuiet is how long the tree must hold still to count as settled.
	// One fsnotify-scale write burst is milliseconds; 30ms clears a burst
	// without making the cockpit feel slow.
	settleQuiet = 30 * time.Millisecond
	// settleBudget bounds the total wait. A worker rewriting a file in a
	// loop would otherwise hold the owner's page load open forever; past
	// this the honest answer is the banner, not more waiting.
	settleBudget = 300 * time.Millisecond
	// settleRecent is how recently a path must have changed to be worth
	// waiting on at all. A tree nobody has touched for seconds is not
	// mid-edit, and charging every cockpit load a settleQuiet wait to guard
	// the rare case would tax the common one for nothing.
	settleRecent = 2 * time.Second
)

// fileStamp is everything an outside observer can cheaply learn about a path.
// Two stamps that differ mean the file changed between the samples; two that
// agree mean it probably did not (a rewrite that lands the same size within
// the filesystem's mtime granularity is invisible here — declared residual,
// and the boot sentinel from 842095a is the net underneath it).
type fileStamp struct {
	exists bool
	size   int64
	mod    time.Time
}

// diskStamp stamps ref (a slash-separated path relative to dir).
func diskStamp(dir string) func(ref string) fileStamp {
	return func(ref string) fileStamp {
		st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(ref)))
		if err != nil || st.IsDir() {
			return fileStamp{}
		}
		return fileStamp{exists: true, size: st.Size(), mod: st.ModTime()}
	}
}

// sampleTree stamps every path once.
func sampleTree(paths []string, stamp func(string) fileStamp) map[string]fileStamp {
	out := make(map[string]fileStamp, len(paths))
	for _, p := range paths {
		out[p] = stamp(p)
	}
	return out
}

// movingPaths returns the sorted paths whose stamp differs between two
// samples of the same path set: created, removed, resized, or rewritten in
// place. This comparison is the entire settle decision.
func movingPaths(before, after map[string]fileStamp) []string {
	var moving []string
	for p, b := range before {
		if a, ok := after[p]; !ok || a != b {
			moving = append(moving, p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			moving = append(moving, p)
		}
	}
	sort.Strings(moving)
	return moving
}

// anyRecent reports whether some path was modified within recent of now —
// the cheap precondition that keeps a quiet tree off the settle path.
func anyRecent(sample map[string]fileStamp, now time.Time, recent time.Duration) bool {
	for _, s := range sample {
		if !s.exists {
			// A path index.html names but that is not on disk is either
			// T374's missing module or a write in flight; either way the
			// tree deserves a look rather than a fast pass.
			return true
		}
		if now.Sub(s.mod) < recent {
			return true
		}
	}
	return false
}

// settleTree waits for paths to hold still and returns the ones that never
// did. Empty means the tree is quiescent and safe to serve.
//
// sleep and now are injected so the loop's timing is decidable in a test
// without a real clock: the budget is spent in sleep increments, not measured
// against wall time, so a test can count the waits it would have taken.
func settleTree(
	paths []string,
	stamp func(string) fileStamp,
	quiet, budget, recent time.Duration,
	sleep func(time.Duration),
	now func() time.Time,
) []string {
	if len(paths) == 0 || budget <= 0 || quiet <= 0 {
		return nil
	}
	before := sampleTree(paths, stamp)
	if !anyRecent(before, now(), recent) {
		return nil
	}
	var moving []string
	for waited := time.Duration(0); waited < budget; waited += quiet {
		sleep(quiet)
		after := sampleTree(paths, stamp)
		moving = movingPaths(before, after)
		if len(moving) == 0 {
			return nil
		}
		before = after
	}
	return moving
}

// settleServingTree is the production wiring: settle index.html together with
// every module it names, against the directory about to be served.
func settleServingTree(dir string, refs []string) []string {
	paths := make([]string, 0, len(refs)+1)
	paths = append(paths, indexRef)
	paths = append(paths, refs...)
	return settleTree(paths, diskStamp(dir),
		settleQuiet, settleBudget, settleRecent, time.Sleep, time.Now)
}

// indexRef is the cockpit document itself, named the same way index.html
// names its modules so the banner reads uniformly.
const indexRef = "index.html"
