// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package butler_test

// Lifecycle tier of the butler-loop oracle (🎯T30): spawn, direct with
// transparent rehydrate (NO SILENT-FAIL), and process-as-cache GC. The
// disposable process behind a thread is stubbed by a fakeFleet so the
// butler's *policy* is exercised deterministically; the live claudia
// Fleet is verified separately at the live tier.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/thread"
	"github.com/marcelocantos/jevons/internal/transcript"
)

// fakeFleet is an in-memory stand-in for the claudia-backed process
// fleet: it tracks liveness, counts launches, and echoes directed turns.
type fakeFleet struct {
	alive     map[string]bool
	launches  int
	launchErr error  // when set, Launch fails (unreachable process)
	mintID    string // session id assigned on first launch
}

func newFakeFleet() *fakeFleet { return &fakeFleet{alive: map[string]bool{}} }

func (f *fakeFleet) Launch(t *thread.Thread) error {
	f.launches++
	if f.launchErr != nil {
		return f.launchErr
	}
	if t.SessionID == "" {
		t.SessionID = f.mintID
	}
	f.alive[t.ID] = true
	return nil
}

func (f *fakeFleet) Send(id, text string) (string, error) {
	if !f.alive[id] {
		return "", fmt.Errorf("no live process for %q", id)
	}
	return "reply to: " + text, nil
}

func (f *fakeFleet) Alive(id string) bool { return f.alive[id] }
func (f *fakeFleet) Stop(id string)       { f.alive[id] = false }

func newLifecycleButler(t *testing.T, dir string, f butler.Fleet) *butler.Butler {
	t.Helper()
	store, err := thread.NewStore(filepath.Join(dir, "threads.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	projectsDir := filepath.Join(dir, "projects")
	return butler.New(butler.Config{
		Store:   store,
		Scanner: discovery.NewScanner(projectsDir),
		Reader:  transcript.NewReader(projectsDir),
		Fleet:   f,
		Now:     func() time.Time { return fixedNow },
	})
}

// TestSpawnAndDirect covers T30 SPAWN + DIRECT: spawn an agent, send it
// a directed message, and get its reply back (the web round-trip).
func TestSpawnAndDirect(t *testing.T) {
	f := newFakeFleet()
	f.mintID = "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
	b := newLifecycleButler(t, t.TempDir(), f)

	th, err := b.Spawn(butler.SpawnArgs{ID: "maze-worker", WorkDir: "/work/multimaze2", Description: "rebuild"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if th.Kind != thread.KindSpawned || th.SessionID != f.mintID {
		t.Fatalf("spawned thread not recorded with minted session: %+v", th)
	}

	reply, err := b.Direct("maze-worker", "status?")
	if err != nil {
		t.Fatalf("Direct: %v", err)
	}
	if !strings.Contains(reply, "status?") {
		t.Fatalf("directed turn did not round-trip a reply: %q", reply)
	}
}

// TestDirectRehydratesStoppedProcess covers PROCESS-AS-CACHE + the happy
// half of NO SILENT-FAIL: directing a thread whose process was stopped
// transparently rehydrates it and delivers.
func TestDirectRehydratesStoppedProcess(t *testing.T) {
	f := newFakeFleet()
	f.mintID = "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
	b := newLifecycleButler(t, t.TempDir(), f)

	if _, err := b.Spawn(butler.SpawnArgs{ID: "w", WorkDir: "/work/x"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	launchesAfterSpawn := f.launches

	f.Stop("w") // simulate GC / process age-out
	if f.Alive("w") {
		t.Fatal("precondition: process should be stopped")
	}

	reply, err := b.Direct("w", "continue")
	if err != nil {
		t.Fatalf("Direct should transparently rehydrate, got error: %v", err)
	}
	if f.launches <= launchesAfterSpawn {
		t.Fatal("Direct did not rehydrate the stopped process")
	}
	if !f.Alive("w") || !strings.Contains(reply, "continue") {
		t.Fatalf("rehydrate+deliver failed: alive=%v reply=%q", f.Alive("w"), reply)
	}
}

// TestDirectNoSilentFail covers the load-bearing NO SILENT-FAIL edge:
// a thread whose process cannot be restarted returns a distinct,
// actionable error — never a silent timeout or a false success.
func TestDirectNoSilentFail(t *testing.T) {
	f := newFakeFleet()
	f.mintID = "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
	b := newLifecycleButler(t, t.TempDir(), f)

	if _, err := b.Spawn(butler.SpawnArgs{ID: "w", WorkDir: "/work/x"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	f.Stop("w")
	f.launchErr = fmt.Errorf("tmux server gone")

	_, err := b.Direct("w", "continue")
	if err == nil {
		t.Fatal("expected a distinct error when the process cannot be rehydrated")
	}
	if !strings.Contains(err.Error(), "rehydrate") {
		t.Fatalf("error should name the rehydrate failure, got: %v", err)
	}
}

// TestDirectRefusesAdoptedThread: an observe-only adopted thread must
// not be directed until taken over (the two-writer guard).
func TestDirectRefusesAdoptedThread(t *testing.T) {
	dir := t.TempDir()
	projectsDir := filepath.Join(dir, "projects")
	writeOwnerTranscript(t, projectsDir)
	b := newLifecycleButler(t, dir, newFakeFleet())

	if _, err := b.Adopt(butler.AdoptArgs{SessionID: ownerSession, ID: "po"}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if _, err := b.Direct("po", "do the thing"); err == nil {
		t.Fatal("directing an observe-only adopted thread should fail")
	}
}

// TestReapIdleStopsAndRehydrates covers PROCESS-AS-CACHE GC end to end:
// an idle spawned thread's process is stopped (freeing resources), the
// thread persists, and the next Direct rehydrates it.
func TestReapIdleStopsAndRehydrates(t *testing.T) {
	dir := t.TempDir()
	projectsDir := filepath.Join(dir, "projects")
	mint := "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"

	// A transcript whose last activity is well past the idle threshold,
	// so the spawned thread derives as idle and becomes eligible for GC.
	writeSessionTranscript(t, projectsDir, mint, fixedNow.Add(-30*time.Minute))

	f := newFakeFleet()
	f.mintID = mint
	b := newLifecycleButler(t, dir, f)

	if _, err := b.Spawn(butler.SpawnArgs{ID: "w", WorkDir: "/work/idle"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if !f.Alive("w") {
		t.Fatal("precondition: spawned process should be alive")
	}

	reaped := b.ReapIdle()
	if len(reaped) != 1 || reaped[0] != "w" {
		t.Fatalf("ReapIdle = %v, want [w]", reaped)
	}
	if f.Alive("w") {
		t.Fatal("idle process should have been stopped")
	}

	// Thread persists after GC and rehydrates on the next interaction.
	if _, ok := b.Status("w"); ok != nil {
		t.Fatalf("thread lost after GC: %v", ok)
	}
	if _, err := b.Direct("w", "resume please"); err != nil {
		t.Fatalf("Direct after GC should rehydrate: %v", err)
	}
	if !f.Alive("w") {
		t.Fatal("Direct after GC did not rehydrate the process")
	}
}

// writeSessionTranscript writes a minimal concluded-turn transcript for
// an arbitrary session id, timestamped at `at`.
func writeSessionTranscript(t *testing.T, projectsDir, sessionID string, at time.Time) {
	t.Helper()
	ts := at.Format(time.RFC3339)
	lines := []string{
		fmt.Sprintf(`{"type":"user","cwd":"/work/idle","timestamp":%q,"message":{"role":"user","content":"go"}}`, ts),
		fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"finished"}]}}`, ts),
	}
	projDir := filepath.Join(projectsDir, "-work-idle")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projDir, sessionID+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
}
