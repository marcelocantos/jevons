// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

// Live tier of the butler oracle: exercises the real claudia-backed
// Fleet against a real Grok ACP session. Gated behind JEVONS_LIVE.
//
//	JEVONS_LIVE=1 go test ./internal/fleet -run TestClaudiaFleetLive -v

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
		t.Skip("JEVONS_LIVE not set (spawns a real Grok agent and spends API credit)")
	}
	if _, err := exec.LookPath("grok"); err != nil {
		// Also accept ~/.grok/bin/grok
		if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".grok", "bin", "grok")); err != nil {
			t.Skip("grok binary not available")
		}
	}

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

	reply, err := f.Send(th.ID, "Reply with exactly the two letters: OK")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(strings.ToUpper(reply), "OK") {
		t.Fatalf("reply %q does not contain OK", reply)
	}
}
