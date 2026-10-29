package main

import (
	"bytes"
	"encoding/json"
	"github.com/marcelocantos/jevons/internal/server"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/pigeon/crypto"
)

func TestT1244PairingAddsSecondPeerWithoutReplacingExisting(t *testing.T) {
	state := t.TempDir()
	path := filepath.Join(state, "credential.json")
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	original := crypto.NewPairingRecord("existing-ipad", "https://relay.invalid", kp, kp.Public)
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	kp2, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	fold := crypto.NewPairingRecord("fold", "https://relay.invalid", kp2, kp2.Public)
	foldRaw, err := json.Marshal(fold)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "fold.json")
	if err := os.WriteFile(source, foldRaw, 0600); err != nil {
		t.Fatal(err)
	}
	code, err := runOneShot(state, "", source, "")
	if code != 0 || err != nil {
		t.Fatalf("add second peer: %d %v", code, err)
	}
	store := server.NewCredentialStore(path)
	primary, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if primary.PeerInstanceID != "existing-ipad" || !bytes.Equal(primary.LocalPrivateKey, original.LocalPrivateKey) || store.GetPeer("fold") == nil {
		t.Fatal("multi-device migration lost a peer")
	}
	code, err = runOneShot(state, "", source, "")
	if code == 0 || err == nil || !strings.Contains(err.Error(), "already paired") {
		t.Fatalf("duplicate peer: %d %v", code, err)
	}
}
