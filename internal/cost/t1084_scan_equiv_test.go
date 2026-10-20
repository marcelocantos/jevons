// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// Every pass equals the full walk, through creation, deletion, appends,
// new directories, aging and the spool's derived .view — except the one
// documented residue: a transcript idle past the warm window that is then
// appended is found by the next full sweep, not by the pass after the append.
func TestT1084CostScanMatchesFullWalk(t *testing.T) {
	st, c, projects, spoolDir := newScanFixture(t)
	pass := func(label string) []string {
		t.Helper()
		got, err := c.ScanOnce()
		if err != nil {
			t.Fatal(err)
		}
		if h := c.Health(); !h.LastScan.Equal(st.now) {
			t.Fatalf("%s: LastScan %v, want this pass %v", label, h.LastScan, st.now)
		}
		return got
	}
	check := func(label string) {
		t.Helper()
		got := pass(label)
		if want := referenceScan(t, projects, st.now.Add(-DefaultActiveWindow)); !sameList(got, want) {
			t.Fatalf("%s: found %v, the full walk %v", label, got, want)
		}
	}
	tick := func() { st.now = st.now.Add(DefaultScanInterval) }

	st.write(filepath.Join(projects, "a", "old.jsonl"), 72*time.Hour)
	st.write(filepath.Join(projects, "a", "warm.jsonl"), 2*time.Hour)
	st.write(filepath.Join(projects, "a", "chat_history.jsonl"), time.Minute) // not billable
	st.write(filepath.Join(projects, "b", "live.jsonl"), time.Minute)
	st.write(filepath.Join(spoolDir, "events-2026-10-20.log"), time.Minute)
	st.write(filepath.Join(spoolDir, ".view", "w.jsonl"), time.Minute) // derived, never counted
	check("first pass (full sweep)")
	full := c.Health().LastFullScan

	tick()
	st.write(filepath.Join(projects, "a", "warm.jsonl"), 0)
	check("a warm transcript is appended")
	tick()
	st.write(filepath.Join(projects, "a", "new.jsonl"), 0)
	check("a new transcript in a known directory")
	tick()
	st.write(filepath.Join(projects, "c", "d", "subagents", "agent.jsonl"), 0)
	check("a new nested directory")
	tick()
	if err := os.Remove(filepath.Join(projects, "b", "live.jsonl")); err != nil {
		t.Fatal(err)
	}
	check("a transcript is removed")
	tick()
	st.write(filepath.Join(projects, "a", "imported.jsonl"), 48*time.Hour)
	check("a new transcript arrives already old")

	// The residue, pinned: old.jsonl has been idle past the warm window,
	// so a pass does not stat it, and its append is not seen ...
	tick()
	missing := filepath.Join(projects, "a", "old.jsonl")
	st.write(missing, 0)
	got := pass("a long-idle transcript is appended")
	want := referenceScan(t, projects, st.now.Add(-DefaultActiveWindow))
	var without []string
	for _, f := range want {
		if f != missing {
			without = append(without, f)
		}
	}
	if !sameList(got, without) || len(want) != len(got)+1 {
		t.Fatalf("residue: pass found %v; full walk %v; want the full walk minus %s only", got, want, missing)
	}
	if !c.Health().LastFullScan.Equal(full) {
		t.Fatalf("a full sweep ran inside the sweep interval")
	}
	// ... until the next full sweep, at most FullSweepInterval later, which
	// finds it while it is still active.
	st.now = full.Add(FullSweepInterval)
	check("the next full sweep finds the long-idle transcript")
	if h := c.Health(); !h.LastFullScan.Equal(st.now) {
		t.Fatalf("LastFullScan %v, want the sweep at %v", h.LastFullScan, st.now)
	}
	full = st.now
	tick()
	check("and the pass after it keeps it")
	st.now = st.now.Add(8 * time.Minute)
	check("active transcripts age out between sweeps")
	if !c.Health().LastFullScan.Equal(full) {
		t.Fatalf("a full sweep ran inside the sweep interval")
	}
}

// Random trees, random edits (any edit but the documented residue), random
// clock steps: every pass equals the full walk.
func TestT1084CostScanMatchesFullWalkOnRandomEdits(t *testing.T) {
	fulls, tiered := 0, 0
	for seed := int64(0); seed < 12; seed++ {
		r := rand.New(rand.NewSource(seed))
		st, c, projects, spoolDir := newScanFixture(t)
		var paths []string
		for i := 0; i < 60; i++ {
			p := filepath.Join(projects, fmt.Sprintf("p%d", r.Intn(6)), fmt.Sprintf("s%d.jsonl", i))
			if r.Intn(4) == 0 {
				p = filepath.Join(projects, fmt.Sprintf("p%d", r.Intn(6)), fmt.Sprintf("sess%d", r.Intn(3)), "subagents", fmt.Sprintf("a%d.jsonl", i))
			}
			paths = append(paths, p)
			st.write(p, time.Duration(r.Intn(96))*time.Hour)
		}
		st.write(filepath.Join(spoolDir, "events-2026-10-20.log"), 0)
		for step := 0; step < 60; step++ {
			switch r.Intn(6) {
			case 0: // a new transcript, of any age
				p := filepath.Join(projects, fmt.Sprintf("p%d", r.Intn(8)), fmt.Sprintf("n%d-%d.jsonl", seed, step))
				paths = append(paths, p)
				st.write(p, time.Duration(r.Intn(48))*time.Hour)
			case 1: // remove one
				if i := r.Intn(len(paths)); paths[i] != "" {
					_ = os.Remove(paths[i])
					paths[i] = ""
				}
			case 2, 3: // append to a transcript written within the warm window
				var warm []string
				for _, p := range paths {
					if fi, err := os.Stat(p); err == nil && fi.ModTime().After(st.now.Add(-WarmWindow)) {
						warm = append(warm, p)
					}
				}
				if len(warm) > 0 {
					st.write(warm[r.Intn(len(warm))], 0)
				}
			case 4:
				st.write(filepath.Join(spoolDir, "events-2026-10-20.log"), 0)
			}
			// Mostly passes between sweeps; now and then a long gap.
			if r.Intn(10) == 0 {
				st.now = st.now.Add(time.Duration(r.Intn(40)) * time.Minute)
			} else {
				st.now = st.now.Add(time.Duration(r.Intn(180)) * time.Second)
			}
			if c.Health().LastFullScan.Before(st.now.Add(-FullSweepInterval)) {
				fulls++
			} else {
				tiered++
			}
			got, err := c.ScanOnce()
			if err != nil {
				t.Fatal(err)
			}
			want := referenceScan(t, projects, st.now.Add(-DefaultActiveWindow))
			if !sameList(got, want) {
				sort.Strings(got)
				sort.Strings(want)
				t.Fatalf("seed %d step %d: found %v, full walk %v", seed, step, got, want)
			}
		}
	}
	if tiered < 4*fulls {
		t.Fatalf("only %d of %d passes were tiered; the oracle is not exercising the tiered path", tiered, tiered+fulls)
	}
	t.Logf("%d tiered passes and %d full sweeps, all equal to the full walk", tiered, fulls)
}

// TestT1084CostScanRealTree measures a full sweep and a tiered pass over a
// real projects tree, read-only, and holds both to the full walk. Opt-in
// (JEVONS_T1084_REAL_PROJECTS) because it reads a live tree; the spool root
// is an empty temp dir.
func TestT1084CostScanRealTree(t *testing.T) {
	projects := os.Getenv("JEVONS_T1084_REAL_PROJECTS")
	if projects == "" {
		t.Skip("set JEVONS_T1084_REAL_PROJECTS to a projects tree to measure the scan")
	}
	t.Setenv("JEVONS_SPOOL_DIR", t.TempDir())
	c := NewCollector(&CollectorArgs{ProjectsRoot: projects})
	measure := func(label string) {
		t.Helper()
		l, s, d := scanListings.Load(), scanFileStats.Load(), scanDirStats.Load()
		start := time.Now()
		got, err := c.ScanOnce()
		if err != nil {
			t.Fatal(err)
		}
		took := time.Since(start)
		want := referenceScan(t, projects, time.Now().Add(-DefaultActiveWindow))
		t.Logf("%s: %v, %d listings, %d file stats, %d dir stats, %d active (full walk %d)",
			label, took, scanListings.Load()-l, scanFileStats.Load()-s, scanDirStats.Load()-d, len(got), len(want))
		if !sameList(got, want) {
			t.Errorf("%s: found %v, full walk %v (a live tree can move between the two reads)", label, got, want)
		}
	}
	measure("full sweep")
	measure("tiered pass")
	measure("tiered pass")
}
