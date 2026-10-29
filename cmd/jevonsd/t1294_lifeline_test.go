// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"os"
	"syscall"
	"testing"
	"time"
)

// 🎯T1294: the daemon requests a normal stop when the lifeline closes, and
// not before.
func TestT1294LifelineCloseRequestsNormalStop(t *testing.T) {
	r, w := io.Pipe()
	stop := make(chan os.Signal, 1)
	go watchLifeline(r, stop)

	if _, err := w.Write([]byte("still here\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case sig := <-stop:
		t.Fatalf("stop requested while the lifeline is open: %v", sig)
	case <-time.After(50 * time.Millisecond):
	}

	_ = w.Close()
	select {
	case sig := <-stop:
		if sig != syscall.SIGTERM {
			t.Fatalf("lifeline close sent %v, want SIGTERM (normal stop, not upgrade)", sig)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no stop requested after the lifeline closed")
	}
}
