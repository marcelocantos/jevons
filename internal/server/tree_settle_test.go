// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 🎯T375 — 🎯T374 made an ABSENT module loud. A module that is present but
// mid-write is the same owner fault (truncated JS → throw at inline-script top
// level → aborted boot → recurring TDZ) and passes every existence check, so
// it needs its own decision. These tests pin that decision.

var settleEpoch = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

// stampAt is a stamp for a file that exists, of the given size, written at
// epoch+age.
func stampAt(size int64, age time.Duration) fileStamp {
	return fileStamp{exists: true, size: size, mod: settleEpoch.Add(-age)}
}

func TestMovingPathsSpotsEveryWayAFileChanges(t *testing.T) {
	cases := []struct {
		name          string
		before, after map[string]fileStamp
		want          []string
	}{{
		name:   "quiet tree moves nothing",
		before: map[string]fileStamp{"a.js": stampAt(10, time.Second)},
		after:  map[string]fileStamp{"a.js": stampAt(10, time.Second)},
		want:   nil,
	}, {
		// The truncation case: a writer had emitted 4 bytes when we looked.
		name:   "size change",
		before: map[string]fileStamp{"a.js": stampAt(4, time.Second)},
		after:  map[string]fileStamp{"a.js": stampAt(900, time.Second)},
		want:   []string{"a.js"},
	}, {
		// Same length, rewritten — mtime is the only tell.
		name:   "in-place rewrite at identical size",
		before: map[string]fileStamp{"a.js": stampAt(900, 2*time.Second)},
		after:  map[string]fileStamp{"a.js": stampAt(900, time.Second)},
		want:   []string{"a.js"},
	}, {
		name:   "appeared between samples",
		before: map[string]fileStamp{"a.js": {}},
		after:  map[string]fileStamp{"a.js": stampAt(12, 0)},
		want:   []string{"a.js"},
	}, {
		name:   "removed between samples",
		before: map[string]fileStamp{"a.js": stampAt(12, 0)},
		after:  map[string]fileStamp{"a.js": {}},
		want:   []string{"a.js"},
	}, {
		// Absent in both samples is T374's territory, not this one: the
		// banner must call it missing, never "still being written".
		name:   "absent throughout is not moving",
		before: map[string]fileStamp{"gone.js": {}},
		after:  map[string]fileStamp{"gone.js": {}},
		want:   nil,
	}, {
		name: "reports every moving path, sorted",
		before: map[string]fileStamp{
			"z.js": stampAt(1, time.Second), "a.js": stampAt(1, time.Second),
			"m.js": stampAt(1, time.Second),
		},
		after: map[string]fileStamp{
			"z.js": stampAt(2, time.Second), "a.js": stampAt(9, time.Second),
			"m.js": stampAt(1, time.Second),
		},
		want: []string{"a.js", "z.js"},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := movingPaths(tc.before, tc.after); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("movingPaths = %v, want %v", got, tc.want)
			}
		})
	}
}

// stampScript replays a canned sequence of stamps for one path, one entry per
// sample, so a settle scenario is stated as data rather than timing.
func stampScript(path string, seq ...fileStamp) (func(string) fileStamp, *int32) {
	var n int32
	return func(ref string) fileStamp {
		if ref != path {
			return stampAt(1, time.Hour) // an unrelated, long-quiet file
		}
		i := int(atomic.AddInt32(&n, 1)) - 1
		if i >= len(seq) {
			return seq[len(seq)-1]
		}
		return seq[i]
	}, &n
}

func TestSettleTreeReturnsOnceTheTreeHoldsStill(t *testing.T) {
	// Written, written again, then quiet: two matching samples end the wait.
	stamp, _ := stampScript("a.js",
		stampAt(10, 0), stampAt(40, 0), stampAt(90, 0), stampAt(90, 0))
	var slept time.Duration
	got := settleTree([]string{"a.js"}, stamp,
		settleQuiet, settleBudget, settleRecent,
		func(d time.Duration) { slept += d }, func() time.Time { return settleEpoch })

	if len(got) != 0 {
		t.Errorf("settleTree = %v, want none — the tree stopped moving", got)
	}
	if slept > settleBudget {
		t.Errorf("slept %v, over the %v budget", slept, settleBudget)
	}
	if slept == 0 {
		t.Errorf("did not wait at all on a tree that was being written")
	}
}

func TestSettleTreeGivesUpOnAPerpetualWriter(t *testing.T) {
	// A worker rewriting in a loop must not hold the owner's page load open:
	// past the budget the honest answer is the banner, not more waiting.
	var size int64
	stamp := func(ref string) fileStamp {
		if ref != "a.js" {
			return stampAt(1, time.Hour)
		}
		size++
		return fileStamp{exists: true, size: size, mod: settleEpoch}
	}
	var slept time.Duration
	got := settleTree([]string{"a.js", "b.js"}, stamp,
		settleQuiet, settleBudget, settleRecent,
		func(d time.Duration) { slept += d }, func() time.Time { return settleEpoch })

	if !reflect.DeepEqual(got, []string{"a.js"}) {
		t.Errorf("settleTree = %v, want [a.js] — only the moving file is named", got)
	}
	if slept > settleBudget {
		t.Errorf("slept %v, over the %v budget", slept, settleBudget)
	}
}

func TestSettleTreeSkipsATreeNobodyHasTouched(t *testing.T) {
	// The common case is a quiet tree, and it must not pay the settle wait.
	stamp := func(string) fileStamp { return stampAt(100, time.Hour) }
	var waits int
	got := settleTree([]string{"index.html", "a.js"}, stamp,
		settleQuiet, settleBudget, settleRecent,
		func(time.Duration) { waits++ }, func() time.Time { return settleEpoch })

	if len(got) != 0 {
		t.Errorf("settleTree = %v, want none", got)
	}
	if waits != 0 {
		t.Errorf("waited %d times on a tree quiet for an hour; want 0", waits)
	}
}

func TestSettleTreeWithNoBudgetDoesNotWait(t *testing.T) {
	stamp := func(string) fileStamp { return stampAt(1, 0) }
	var waits int
	if got := settleTree([]string{"a.js"}, stamp,
		settleQuiet, 0, settleRecent,
		func(time.Duration) { waits++ }, func() time.Time { return settleEpoch }); got != nil {
		t.Errorf("settleTree = %v, want nil with a zero budget", got)
	}
	if waits != 0 {
		t.Errorf("waited %d times with a zero budget", waits)
	}
}

// TestDiskIndexFlagsMidEditModule is the acceptance oracle for the half T374
// could not see: every referenced file EXISTS, so the existence guard passes,
// and the module is still being rewritten under the request. The owner must
// be told, by name, rather than handed a truncated module.
func TestDiskIndexFlagsMidEditModule(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"index.html": `<!doctype html><html><head>` +
			`<script src="scripts/steady.js"></script>` +
			`<script src="scripts/churning.js"></script>` +
			`</head><body><div id="app"></div></body></html>`,
		"scripts/steady.js":   "// steady\n",
		"scripts/churning.js": "// churning\n",
	})

	// Stand in for a fleet worker mid-write. The body length alternates so
	// the change is visible in size alone, independent of mtime granularity.
	// The write error is carried out of the goroutine rather than reported
	// from it — t.Fatal off the test goroutine does not fail the test.
	stop := make(chan struct{})
	done := make(chan struct{})
	var writeErr atomic.Value
	go func() {
		defer close(done)
		churn := filepath.Join(dir, "scripts", "churning.js")
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			body := []byte("// " + strings.Repeat("x", 1+i%64) + "\n")
			if err := os.WriteFile(churn, body, 0o644); err != nil {
				writeErr.Store(err)
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	defer func() {
		close(stop)
		<-done
		if err, ok := writeErr.Load().(error); ok {
			t.Fatalf("churn writer failed, so the mid-edit tree was never simulated: %v", err)
		}
	}()

	code, body := serveIndexFromDir(t, dir)
	if code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200 (must still serve, with recovery)", code)
	}
	if !strings.Contains(body, assetErrorMarker) {
		t.Fatalf("a module being rewritten under the request was served silently")
	}
	if !strings.Contains(body, "scripts/churning.js") {
		t.Errorf("banner does not name the mid-write module")
	}
	if !strings.Contains(body, `id="app"`) {
		t.Errorf("original document body was dropped")
	}

	// Red-against-pre-fix, asserted as a standing fact rather than claimed
	// once: run the SAME document through T374's existence guard with no
	// settle result. Every file exists, so it passes clean — which is exactly
	// why this fault reached the daily cockpit under a guard that was working
	// correctly. If this ever starts flagging, the test above has stopped
	// testing the settle pass and is riding on the existence check.
	raw, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	preFix, missing := guardIndexAssets(raw, func(string) bool { return true }, nil)
	if len(missing) > 0 {
		t.Fatalf("existence guard reported %v; the mid-edit tree must have every file present", missing)
	}
	if strings.Contains(string(preFix), assetErrorMarker) {
		t.Errorf("existence guard alone flagged the mid-edit tree; the settle pass is no longer load-bearing here")
	}
}

// TestDiskIndexSettledTreeHasNoBanner keeps the settle guard from crying
// wolf. A tree that is merely RECENT — just written, then left alone, which
// is what every fleet worker's tree looks like a moment after it lands — must
// serve clean.
func TestDiskIndexSettledTreeHasNoBanner(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"index.html": `<!doctype html><html><head>` +
			`<script src="scripts/a.js"></script>` +
			`</head><body>ok</body></html>`,
		"scripts/a.js": "// a\n",
	})

	code, body := serveIndexFromDir(t, dir)
	if code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", code)
	}
	if strings.Contains(body, assetErrorMarker) {
		t.Errorf("a tree written a moment ago and then left alone was flagged:\n%s", body)
	}
	if !strings.Contains(body, "<body>ok</body>") {
		t.Errorf("settled tree body was rewritten: %q", body)
	}
}
