// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/server"
	"github.com/marcelocantos/pigeon/crypto"
)

// 🎯T1093: `jevonsd --add-credential` (and --pair) wrote
// ~/.jevons/credential.json whatever state_dir said, while the daemon reads
// <state_dir>/credential.json. Pairing a daemon on a non-default state_dir
// wrote a file it never read, and an isolate's pairing wrote into
// development state.
func TestT1093OneShotWritesTheCredentialUnderStateDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stateDir := filepath.Join(t.TempDir(), "isolate-state")
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	rec := crypto.NewPairingRecord("peer-t1093", "https://relay.invalid", kp, kp.Public)
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	recPath := filepath.Join(t.TempDir(), "server-record.json")
	if err := os.WriteFile(recPath, b, 0o600); err != nil {
		t.Fatal(err)
	}

	code, err := runOneShot(stateDir, "", recPath, "")
	if err != nil || code != 0 {
		t.Fatalf("runOneShot = %d, %v", code, err)
	}

	// Where the daemon reads it.
	got, err := server.NewCredentialStore(filepath.Join(stateDir, "credential.json")).Load()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.PeerInstanceID != "peer-t1093" {
		t.Fatalf("the daemon's state_dir holds %+v, want the saved credential", got)
	}
	// And not in the development tree.
	if _, err := os.Stat(filepath.Join(home, ".jevons")); !os.IsNotExist(err) {
		t.Fatalf("the one-shot wrote under $HOME/.jevons (stat err %v)", err)
	}
}
