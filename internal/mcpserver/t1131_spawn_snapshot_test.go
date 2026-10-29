// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/spawnorder"
)

// 🎯T1131: a stale spawn-order cache refresh does not hold the mutex across
// journal I/O — concurrent readers return the prior snapshot immediately.
func TestT1131SpawnOrderEvidenceRefreshDoesNotBlockReaders(t *testing.T) {
	s := &Server{}
	block := make(chan struct{})
	var tailers atomic.Int32
	s.eventLogTail = func(eventlog.TailOptions) ([]eventlog.Event, string, error) {
		if tailers.Add(1) == 1 {
			block <- struct{}{}
			time.Sleep(200 * time.Millisecond)
		}
		return nil, "", nil
	}
	s.spawnOrderEvidence = spawnorder.Evidence{Attempts: []spawnorder.Attempt{{Name: "cached"}}}
	s.spawnOrderReadAt = time.Now().Add(-spawnOrderAttemptsTTL - time.Second)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = s.spawnOrderJournalEvidence(time.Now())
	}()

	select {
	case <-block:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start journal read")
	}

	done := make(chan spawnorder.Evidence, 1)
	go func() {
		done <- s.spawnOrderJournalEvidence(time.Now())
	}()
	select {
	case ev := <-done:
		if len(ev.Attempts) != 1 || ev.Attempts[0].Name != "cached" {
			t.Fatalf("concurrent read did not return stale snapshot: %+v", ev)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("concurrent spawnOrderJournalEvidence blocked on journal I/O")
	}
	close(block)
	wg.Wait()
}
