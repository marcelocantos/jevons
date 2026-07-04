// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

// Live tier of the butler oracle: exercises the real claudia-backed
// Fleet against a real `claude` process — spawn, direct-and-reply,
// stop, rehydrate (--resume), direct again with continuity. It spends
// API credit and needs the claude binary + tmux, so it is gated behind
// JEVONS_LIVE (mirroring claudia's CLAUDIA_LIVE convention) and is not
// part of CI; the deterministic fakeFleet oracle in internal/butler
// covers the policy in CI. Run it before a release, or when validating
// the fleet↔claudia integration:
//
//	JEVONS_LIVE=1 go test ./internal/fleet -run TestClaudiaFleetLive -v
//
// It isolates its own tmux server via CLAUDIA_TMUX_SOCKET so it never
// touches a running jevonsd's live agents.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/thread"
)

func TestClaudiaFleetLive(t *testing.T) {
	if os.Getenv("JEVONS_LIVE") == "" {
		t.Skip("JEVONS_LIVE not set (spawns a real claude agent and spends API credit)")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude binary not on PATH")
	}

	// Isolate the tmux server so this never touches a live jevonsd's
	// agents, then tear the isolated server down at the end.
	sock := filepath.Join(t.TempDir(), "tmux.sock")
	t.Setenv("CLAUDIA_TMUX_SOCKET", sock)
	t.Cleanup(func() { _ = exec.Command("tmux", "-S", sock, "kill-server").Run() })

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	f := NewClaudia(reg)

	th := &thread.Thread{ID: "jevons-live-smoke", Kind: thread.KindSpawned, WorkDir: t.TempDir()}
	if err := f.Launch(th); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t.Cleanup(func() { _ = reg.Remove(th.ID) })
	if th.SessionID == "" {
		t.Fatal("Launch did not populate the thread's session id")
	}

	// SPAWN + DIRECT: a directed turn round-trips a reply.
	reply, err := f.Send(th.ID, "Reply with exactly the two letters: OK")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(strings.ToUpper(reply), "OK") {
		t.Fatalf("directed turn did not round-trip: %q", reply)
	}

	// PROCESS-AS-CACHE + rehydrate: stop the process, then a fresh
	// Launch resumes the same session and a further turn still lands.
	f.Stop(th.ID)
	if f.Alive(th.ID) {
		t.Fatal("process still alive after Stop")
	}
	if err := f.Launch(th); err != nil {
		t.Fatalf("rehydrate Launch (--resume): %v", err)
	}
	reply2, err := f.Send(th.ID, "Reply with exactly: STILL HERE")
	if err != nil {
		t.Fatalf("Send after rehydrate: %v", err)
	}
	t.Logf("rehydrated reply (continuity check): %q", reply2)
}
