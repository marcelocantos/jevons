// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSeatGateSendBlocksReap(t *testing.T) {
	var g SeatGate
	done := g.BeginSend("aside")
	if _, ok := g.TryBeginReap("aside"); ok {
		t.Fatal("reap admitted while send held the seat")
	}
	if !g.Sending("aside") {
		t.Fatal("send admission not visible")
	}
	done()
	release, ok := g.TryBeginReap("aside")
	if !ok {
		t.Fatal("reap refused after send released")
	}
	release()
}

func TestSeatGateReapMakesSendWait(t *testing.T) {
	var g SeatGate
	release, ok := g.TryBeginReap("aside")
	if !ok {
		t.Fatal("first reap")
	}
	var started atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		done := g.BeginSend("aside")
		started.Store(true)
		done()
	}()
	time.Sleep(30 * time.Millisecond)
	if started.Load() {
		t.Fatal("send ran while reap held the seat")
	}
	release()
	wg.Wait()
	if !started.Load() {
		t.Fatal("send did not proceed after reap released")
	}
}

func TestSeatGateNestedSends(t *testing.T) {
	var g SeatGate
	outer := g.BeginSend("w")
	inner := g.BeginSend("w")
	if _, ok := g.TryBeginReap("w"); ok {
		t.Fatal("reap admitted under nested sends")
	}
	inner()
	if _, ok := g.TryBeginReap("w"); ok {
		t.Fatal("reap admitted while outer send still held")
	}
	outer()
	release, ok := g.TryBeginReap("w")
	if !ok {
		t.Fatal("reap refused after nested sends released")
	}
	release()
}

func TestSeatGateConcurrentSendAndReap(t *testing.T) {
	var g SeatGate
	var reapedWhileSending atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			done := g.BeginSend("race")
			if g.Sending("race") {
				if _, ok := g.TryBeginReap("race"); ok {
					reapedWhileSending.Add(1)
				}
			}
			done()
		}()
		go func() {
			defer wg.Done()
			if release, ok := g.TryBeginReap("race"); ok {
				if g.Sending("race") {
					reapedWhileSending.Add(1)
				}
				release()
			}
		}()
	}
	wg.Wait()
	if n := reapedWhileSending.Load(); n != 0 {
		t.Fatalf("reap overlapped a held send %d times", n)
	}
}
