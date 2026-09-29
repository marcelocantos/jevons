// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 🎯T840: J3 holds its long turn on a shell tool call that only the journey
// can release. These peers pin the two halves the journey leans on: the
// hold really blocks until released (and says so on ready/completed), and
// the hold wait refuses a turn that ended before the hold was running.

func TestT840CancelHoldBlocksUntilReleased(t *testing.T) {
	dir := t.TempDir()
	// A quote in the path proves the command survives shell quoting.
	dir = filepath.Join(dir, "it's")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(dir, "ready")
	release := filepath.Join(dir, "release")
	completed := filepath.Join(dir, "completed")
	const nonce = "journey-cancel-long-nonce"

	cmd := exec.Command("/bin/sh", "-c", cancelHoldCommand(ready, release, completed, nonce))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		_ = cmd.Process.Kill()
	}()

	waitFor := func(path string) string {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if body, err := os.ReadFile(path); err == nil {
				return strings.TrimSpace(string(body))
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("%s never appeared", path)
		return ""
	}
	if got := waitFor(ready); got != nonce {
		t.Fatalf("ready=%q, want the request nonce", got)
	}

	// Unreleased, the hold keeps running across more than one poll step.
	select {
	case err := <-done:
		t.Fatalf("hold returned before release: %v", err)
	case <-time.After(1500 * time.Millisecond):
	}
	if _, err := os.Stat(completed); !os.IsNotExist(err) {
		t.Fatalf("completed written while the hold was unreleased: %v", err)
	}

	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("hold exited %v after release", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("hold did not return after release")
	}
	if got := waitFor(completed); got != nonce {
		t.Fatalf("completed=%q, want the request nonce", got)
	}
}

func TestT840HoldWaitRefusesATurnThatEndedFirst(t *testing.T) {
	const nonce = "journey-cancel-long-nonce"
	const ownerIndex = 2
	stream := func(index int, stop string) []byte {
		return ownerMuxFixture(t, ownerMuxChannel, "assistant", index, "x", stop, "", "append")
	}
	for _, tc := range []struct {
		name   string
		ready   string // "" leaves the marker absent
		frames  [][]byte
		wantErr string // "" wants success
	}{
		{"hold running with this request's nonce", nonce, nil, ""},
		// A previous turn's terminal at or before the echo is not this
		// request ending: the wait runs on to its own deadline.
		{"a previous turn's terminal is not ours", "", [][]byte{stream(2, "end_turn")}, "never started"},
		{"a tool_use row is not a terminal", "", [][]byte{stream(3, "tool_use")}, "never started"},

		// The a80d7b58 failure: the provider answered without running the
		// hold, so there is no live turn left to cancel.
		{"this request ended before the hold", "", [][]byte{stream(3, "end_turn")}, "ended before"},
		{"this request stopped by stop_sequence", "", [][]byte{stream(3, "stop_sequence")}, "ended before"},
		{"another request's nonce is not this hold", "journey-cancel-long-other", nil, "never started"},
		{"no hold at all", "", nil, "never started"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ready := filepath.Join(t.TempDir(), "ready")
			if tc.ready != "" {
				if err := os.WriteFile(ready, []byte(tc.ready+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			frames := make(chan []byte, len(tc.frames))
			for _, f := range tc.frames {
				frames <- f
			}
			err := waitOwnerMuxHold(context.Background(), frames, ownerIndex, ready, nonce, 400*time.Millisecond)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("err=%v, want success", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v, want %q", err, tc.wantErr)
			}
		})
	}
}
