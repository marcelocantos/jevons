// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/spool"
)

// referenceScan is the pre-🎯T1084 ScanOnce walk, verbatim in effect: every
// directory listed, every billable file stat'ed. The tiered scan is held to
// it.
func referenceScan(t *testing.T, projectsRoot string, cutoff time.Time) []string {
	t.Helper()
	var files []string
	seen := map[string]bool{}
	spoolRoot := spool.Dir()
	walk := func(root string) error {
		if strings.TrimSpace(root) == "" {
			return nil
		}
		viewRoot := ""
		if spoolRoot != "" {
			var err error
			viewRoot, err = filepath.Abs(filepath.Join(spoolRoot, ".view"))
			if err != nil {
				return err
			}
		}
		return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			identity, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			if d.IsDir() && viewRoot != "" && (identity == viewRoot || strings.HasPrefix(identity, viewRoot+string(filepath.Separator))) {
				return filepath.SkipDir
			}
			if d.IsDir() || !isBillableTranscript(path) || seen[identity] {
				return nil
			}
			if fi, err := d.Info(); err == nil && fi.ModTime().After(cutoff) {
				seen[identity] = true
				files = append(files, path)
			}
			return nil
		})
	}
	if err := walk(projectsRoot); err != nil {
		t.Fatal(err)
	}
	if err := walk(spoolRoot); err != nil {
		t.Fatal(err)
	}
	return files
}

type scanTree struct {
	t   *testing.T
	now time.Time
}

func (st *scanTree) write(path string, age time.Duration) {
	st.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		st.t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		st.t.Fatal(err)
	}
	if _, err := f.WriteString("{}\n"); err != nil {
		st.t.Fatal(err)
	}
	_ = f.Close()
	mt := st.now.Add(-age)
	if err := os.Chtimes(path, mt, mt); err != nil {
		st.t.Fatal(err)
	}
}

func sameList(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func newScanFixture(t *testing.T) (*scanTree, *Collector, string, string) {
	t.Helper()
	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	spoolDir := filepath.Join(root, "spool")
	t.Setenv(spool.DirEnv, spoolDir)
	if err := os.MkdirAll(filepath.Join(spoolDir, ".view"), 0o700); err != nil {
		t.Fatal(err)
	}
	st := &scanTree{t: t, now: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
	c := NewCollector(&CollectorArgs{ProjectsRoot: projects, Now: func() time.Time { return st.now }})
	return st, c, projects, spoolDir
}

// 🎯T1084: the scan walked ~/.claude/projects (66,598 transcripts in 5,066
// directories on the development host) every 30 s, listing every directory
// and stat'ing every transcript — about 0.65 s of CPU a pass, 11 of 40
// goroutine samples. Between full sweeps a pass now lists only directories
// that changed and stats only transcripts written within the warm window.
func TestT1084CostScanQuietPassStatsOnlyWarmFiles(t *testing.T) {
	st, c, projects, spoolDir := newScanFixture(t)
	const projectsN, filesN = 30, 40
	for p := 0; p < projectsN; p++ {
		for f := 0; f < filesN; f++ {
			st.write(filepath.Join(projects, fmt.Sprintf("p%02d", p), fmt.Sprintf("s%02d.jsonl", f)), 72*time.Hour)
		}
		st.write(filepath.Join(projects, fmt.Sprintf("p%02d", p), "s00", "subagents", "agent-1.jsonl"), 72*time.Hour)
	}
	st.write(filepath.Join(projects, "p03", "warm.jsonl"), 3*time.Hour)
	st.write(filepath.Join(projects, "p07", "active.jsonl"), time.Minute)
	st.write(filepath.Join(spoolDir, "events-2026-10-20.log"), time.Minute)
	st.write(filepath.Join(spoolDir, "events-2026-10-17.log"), 72*time.Hour)
	const warm = 3 // warm.jsonl, active.jsonl, today's spool file

	if _, err := c.ScanOnce(); err != nil {
		t.Fatal(err)
	}
	st.now = st.now.Add(DefaultScanInterval)
	listings, stats := scanListings.Load(), scanFileStats.Load()
	got, err := c.ScanOnce()
	if err != nil {
		t.Fatal(err)
	}
	listings, stats = scanListings.Load()-listings, scanFileStats.Load()-stats
	if want := referenceScan(t, projects, st.now.Add(-DefaultActiveWindow)); !sameList(got, want) {
		t.Fatalf("quiet pass found %v, the full walk %v", got, want)
	}
	if listings != 0 || stats > warm {
		t.Fatalf("a quiet pass over %d transcripts listed %d directories and stat'ed %d files; want 0 and at most %d",
			projectsN*(filesN+1)+4, listings, stats, warm)
	}
}
