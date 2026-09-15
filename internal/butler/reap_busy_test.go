// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package butler_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/thread"
	"github.com/marcelocantos/jevons/internal/transcript"
)

type unsupportedTranscriptFleet struct{ *fakeFleet }

func (f unsupportedTranscriptFleet) IdleTranscript(*thread.Thread, int) ([]transcript.Entry, error) {
	return nil, fmt.Errorf("current successor has no supported JSONL transcript")
}

func TestReapIdleDoesNotFallBackToPredecessorHistory(t *testing.T) {
	dir := t.TempDir()
	mint := "eeeeeeee-ffff-0000-1111-333333333333"
	writeSessionTranscript(t, filepath.Join(dir, "projects"), mint, fixedNow.Add(-30*time.Minute))
	f := unsupportedTranscriptFleet{newFakeFleet()}
	f.mintID = mint
	b := newLifecycleButler(t, dir, f)
	if _, err := b.Spawn(butler.SpawnArgs{ID: "w", WorkDir: dir}); err != nil {
		t.Fatal(err)
	}
	if got := b.ReapIdle(); len(got) != 0 {
		t.Fatalf("used predecessor history after provider rejected it: %v", got)
	}
	if !f.Alive("w") {
		t.Fatal("stopped successor using old transcript")
	}
}

// Canonical browser sends need not pass through Butler.Direct. In particular,
// Cursor can be working while this JSONL reader has no usable transcript.
func TestReapIdleRequiresKnownActivity(t *testing.T) {
	for _, state := range []string{"missing", "empty", "unreadable-shape", "unknown-time"} {
		t.Run(state, func(t *testing.T) {
			dir := t.TempDir()
			mint := "eeeeeeee-ffff-0000-1111-222222222222"
			projects := filepath.Join(dir, "projects")
			if state != "missing" {
				writeSessionTranscript(t, projects, mint, fixedNow.Add(-30*time.Minute))
				files, err := filepath.Glob(filepath.Join(projects, "*", mint, "updates.jsonl"))
				if err != nil || len(files) != 1 {
					t.Fatalf("fixture: %v %v", files, err)
				}
				contents := ""
				if state == "unreadable-shape" {
					contents = "not a transcript\n"
				}
				if state == "unknown-time" {
					contents = `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":"finished"}}` + "\n"
				}
				if err := os.WriteFile(files[0], []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			f := newFakeFleet() // No synchronous Direct counter: mirrors the bypass.
			f.mintID = mint
			b := newLifecycleButler(t, dir, f)
			if _, err := b.Spawn(butler.SpawnArgs{ID: "w", WorkDir: dir}); err != nil {
				t.Fatal(err)
			}
			if got := b.ReapIdle(); len(got) != 0 {
				t.Fatalf("reaped %v without known idle activity", got)
			}
			if !f.Alive("w") {
				t.Fatal("stopped a process without idle evidence")
			}
		})
	}
}

// busyFleet is a fakeFleet that reports turns in flight (butler.BusyFleet)
// and runs a hook while a Send is outstanding, so a test can observe the
// exact interleaving the process-as-cache sweep used to get wrong.
type busyFleet struct {
	*fakeFleet
	inFlight   map[string]bool
	duringSend func()
}

func newBusyFleet() *busyFleet {
	return &busyFleet{fakeFleet: newFakeFleet(), inFlight: map[string]bool{}}
}

func (f *busyFleet) Send(id, text string) (string, error) {
	f.inFlight[id] = true
	defer delete(f.inFlight, id)
	if f.duringSend != nil {
		f.duringSend()
	}
	return f.fakeFleet.Send(id, text)
}

func (f *busyFleet) Busy(id string) bool { return f.inFlight[id] }

// TestReapIdleSpareRhBusyThreadMidTurn: a directed worker whose transcript
// shows no recent activity — the normal state of a Claude worker whose
// first turn has not written JSONL yet — must survive the idle sweep for
// as long as its turn is in flight. Before 🎯T282 the sweep stopped the
// process mid-turn and the direct hung until its client timed out.
func TestReapIdleSparesBusyThreadMidTurn(t *testing.T) {
	dir := t.TempDir()
	projectsDir := filepath.Join(dir, "projects")
	mint := "cccccccc-dddd-eeee-ffff-000000000000"
	// Transcript last touched long ago → DeriveStatus says idle.
	writeSessionTranscript(t, projectsDir, mint, fixedNow.Add(-30*time.Minute))

	f := newBusyFleet()
	f.mintID = mint
	b := newLifecycleButler(t, dir, f)

	if _, err := b.Spawn(butler.SpawnArgs{ID: "w", WorkDir: "/work/busy"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	var reapedMidTurn []string
	f.duringSend = func() { reapedMidTurn = b.ReapIdle() }

	if _, err := b.Direct("w", "do the long thing"); err != nil {
		t.Fatalf("Direct: %v", err)
	}
	if len(reapedMidTurn) != 0 {
		t.Fatalf("idle sweep reaped %v during an in-flight turn; want none", reapedMidTurn)
	}
	if !f.Alive("w") {
		t.Fatal("worker process was stopped mid-turn")
	}

	// Once the turn is done the same thread is reapable again — the fix
	// defers process-as-cache GC, it does not disable it.
	if reaped := b.ReapIdle(); len(reaped) != 1 || reaped[0] != "w" {
		t.Fatalf("ReapIdle after the turn = %v, want [w]", reaped)
	}
}

// TestReapIdleWithoutBusyFleet: a Fleet that does not implement BusyFleet
// keeps the historical behaviour (no panic, idle threads still reaped).
func TestReapIdleWithoutBusyFleet(t *testing.T) {
	dir := t.TempDir()
	projectsDir := filepath.Join(dir, "projects")
	mint := "dddddddd-eeee-ffff-0000-111111111111"
	writeSessionTranscript(t, projectsDir, mint, fixedNow.Add(-30*time.Minute))

	f := newFakeFleet()
	f.mintID = mint
	b := newLifecycleButler(t, dir, f)

	if _, err := b.Spawn(butler.SpawnArgs{ID: "w", WorkDir: "/work/plain"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if reaped := b.ReapIdle(); len(reaped) != 1 || reaped[0] != "w" {
		t.Fatalf("ReapIdle = %v, want [w]", reaped)
	}
}

var _ butler.Fleet = (*busyFleet)(nil)
var _ butler.BusyFleet = (*busyFleet)(nil)
var _ = thread.KindSpawned
