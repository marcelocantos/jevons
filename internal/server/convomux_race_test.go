// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"sync"
	"testing"

	"github.com/marcelocantos/jevons/internal/muxwin"
)

// 🎯T590. The 2026-08-31 crash: `fatal error: concurrent map writes` took the
// daemon down on the owner's development surface. The hub's live fan-out records
// delivered ids under the hub lock; the socket's own replay handler
// recorded them under no lock, because it must not hold the hub while it
// writes to a network. A reconnect landing while the fleet was talking was
// enough. Run under -race.
func TestMuxWatchSentIsSafeUnderConcurrentFanOutAndReplay(t *testing.T) {
	w := &muxWatch{}
	w.subscribeTo(muxwin.Resolved{Lo: 1, Hi: 0, Following: true}, muxwin.Resolved{Lo: 1, Hi: 0, Following: true})

	var wg sync.WaitGroup
	const n = 500
	// Fan-out: the hub marking live events delivered.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			id := "e:" + string(rune('a'+i%26))
			if !w.alreadySent(id) {
				w.markSent(id)
			}
		}
	}()
	// Replay: the socket handler recording the same ids as it writes them.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			w.markSent("r:" + string(rune('a'+i%26)))
			_ = w.sentSnapshot()
		}
	}()
	// Subscription churn: paging rewrites the window while both run.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			w.subscribeTo(muxwin.Resolved{Lo: i, Hi: i + 10}, muxwin.Resolved{Lo: i, Hi: i + 10})
			_, _ = w.window()
		}
	}()
	wg.Wait()

	if !w.alreadySent("r:a") {
		t.Fatal("replay-marked id missing from the delivered set")
	}
	if got := len(w.sentSnapshot()); got == 0 {
		t.Fatal("snapshot lost every delivered id")
	}
}

// A snapshot must be a copy: HaveFromIDs ranges over it, and ranging a map
// the hub is still writing is the same fault in a different costume.
func TestSentSnapshotIsACopy(t *testing.T) {
	w := &muxWatch{}
	w.markSent("e:1")
	snap := w.sentSnapshot()
	w.markSent("e:2")
	if _, ok := snap["e:2"]; ok {
		t.Fatal("snapshot aliases the live map")
	}
	if _, ok := snap["e:1"]; !ok {
		t.Fatal("snapshot lost an id it should hold")
	}
}
