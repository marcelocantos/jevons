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
		name    string
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

// J3 waits for the post-boot sweep to reach the overseer and for the turn it
// started to finish before sending its long request (gates 12cd6ca6,
// 0bb5f13a: the restart event ran ahead of the replacement after the cancel).

func TestT840BootSweepHandledNamesTheOverseer(t *testing.T) {
	for _, tc := range []struct {
		name string
		logs string
		want bool
	}{
		{"delivered to the overseer",
			`time=x level=INFO msg="daemon restart resume event delivered" target=jevons event=daemon-restarted workers=0`, true},
		{"delivered to the overseer at end of line",
			`time=x level=INFO msg="daemon restart resume event delivered" workers=0 target=jevons`, true},
		{"deliver failed for the overseer",
			`time=x level=WARN msg="daemon restart resume event deliver failed" target=jevons event=daemon-restarted err=x`, true},
		{"broker holds the fleet",
			`time=x level=INFO msg="claudia daemon holds fleet; reclaimed seats stay silent"`, true},
		{"a PO delivery does not answer for the overseer",
			`time=x level=INFO msg="daemon restart resume event delivered" target=jevons-po event=daemon-restarted`, false},
		{"a PO skip is not the overseer's event",
			`time=x level=INFO msg="daemon restart skip sleeping coordinator" target=jevons-po reason=sleeping_po`, false},
		{"intent read precedes the send",
			`time=x level=INFO msg="no recoverable open owner intent after restart" overseer=jevons`, false},
		{"empty log", ``, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bootSweepHandled([]byte(tc.logs)); got != tc.want {
				t.Fatalf("bootSweepHandled=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestT840BootSweepQuietWaitsForTheSweepTurn(t *testing.T) {
	const delivered = `time=x level=INFO msg="daemon restart resume event delivered" target=jevons event=daemon-restarted` + "\n"
	const quiet = 200 * time.Millisecond
	for _, tc := range []struct {
		name    string
		logs    string
		frames  [][]byte
		wantErr string // "" wants success
	}{
		{"sweep handed over and the overseer is idle", delivered, nil, ""},
		{"sweep turn ran and finished",
			delivered, [][]byte{phaseMeta(t, "thinking"), phaseMeta(t, "idle")}, ""},
		{"a window meta is not a phase sample",
			delivered, [][]byte{windowMeta(t)}, ""},

		// The 12cd6ca6 shape: the sweep has not reached the overseer yet, so
		// a held turn started now would have the restart event queued behind it.
		{"sweep not yet sent", "", nil, "never settled"},
		{"only the PO's event was sent",
			`time=x level=INFO msg="daemon restart resume event delivered" target=jevons-po` + "\n", nil, "never settled"},
		// The sweep's own turn is still running.
		{"sweep turn still working", delivered, [][]byte{phaseMeta(t, "thinking")}, "never settled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "jevonsd.log")
			if err := os.WriteFile(logPath, []byte(tc.logs), 0o600); err != nil {
				t.Fatal(err)
			}
			frames := make(chan []byte, len(tc.frames))
			for _, f := range tc.frames {
				frames <- f
			}
			err := waitBootSweepQuiet(context.Background(), frames, logPath, quiet, 1500*time.Millisecond)
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

// feedEvery sends one frame from next every 100ms until the test ends.
func feedEvery(t *testing.T, next func(i int) []byte) <-chan []byte {
	frames := make(chan []byte)
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for i := 1; ; i++ {
			select {
			case <-stop:
				return
			case <-tick.C:
				select {
				case frames <- next(i):
				case <-stop:
					return
				}
			}
		}
	}()
	return frames
}

// The quiet window restarts on every sign of life: a turn that is still
// streaming transcript frames is not quiescent even between phase samples.
// The converge loop's repeated idle sample is not a sign of life (4da3b8ae
// waited out its whole deadline on an idle overseer).
func TestT840BootSweepQuietRestartsOnActivity(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "jevonsd.log")
	if err := os.WriteFile(logPath, []byte(`msg="daemon restart resume event delivered" target=jevons`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		next    func(i int) []byte
		wantErr string // "" wants success
	}{
		{"still streaming", func(i int) []byte {
			return ownerMuxFixture(t, ownerMuxChannel, "assistant", i, "x", "", "", "append")
		}, "never settled"},
		{"still working", func(int) []byte { return phaseMeta(t, "tool") }, "never settled"},
		{"idle level republished", func(int) []byte { return phaseMeta(t, "idle") }, ""},
		{"another seat streaming", func(i int) []byte {
			return ownerMuxFixture(t, "transcript:jevons-po", "assistant", i, "x", "", "", "append")
		}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := waitBootSweepQuiet(context.Background(), feedEvery(t, tc.next), logPath, 400*time.Millisecond, 1500*time.Millisecond)
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
