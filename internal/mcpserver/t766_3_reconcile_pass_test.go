// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestT7663ReconcilePassesDoNotOverlap(t *testing.T) {
	s := &Server{registry: regWithTree(t)}
	var n atomic.Int32
	first := make(chan struct{})
	release := make(chan struct{})
	s.reconcileBlock = func() {
		if n.Add(1) == 1 {
			close(first)
			<-release
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.Reconcile()
	}()
	<-first
	if s.reconcileMu.TryLock() {
		t.Fatal("the running pass does not hold reconcileMu")
	}
	go func() {
		defer wg.Done()
		s.Reconcile()
	}()
	if s.reconcileMu.TryLock() {
		t.Fatal("a second pass took the lock while the first was inside")
	}
	close(release)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reconcile passes did not finish")
	}
	if got := n.Load(); got != 2 {
		t.Fatalf("passes inside the lock = %d", got)
	}
}
