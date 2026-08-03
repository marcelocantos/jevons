// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/pigeon/crypto"
)

func TestCredentialStoreLoadMissingIsNil(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewCredentialStore(filepath.Join(dir, "credential.json"))
	rec, err := store.Load()
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected nil record for missing file, got %+v", rec)
	}
	if store.Get() != nil {
		t.Fatal("Get should be nil before any Save/Load of a record")
	}
}

func TestCredentialStoreSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "credential.json")
	store := NewCredentialStore(path)

	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	// Peer public key can be any 32-byte X25519 public; reuse local for shape.
	want := crypto.NewPairingRecord("peer-instance-1", "https://carrier-pigeon.fly.dev", kp, kp.Public)

	if err := store.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := store.Get(); got == nil || got.PeerInstanceID != want.PeerInstanceID {
		t.Fatalf("Get after Save: got %+v want peer %s", got, want.PeerInstanceID)
	}

	// Fresh store from disk — atomic write must be readable.
	store2 := NewCredentialStore(path)
	loaded, err := store2.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil after Save")
	}
	if loaded.PeerInstanceID != want.PeerInstanceID {
		t.Errorf("PeerInstanceID: got %q want %q", loaded.PeerInstanceID, want.PeerInstanceID)
	}
	if loaded.RelayURL != want.RelayURL {
		t.Errorf("RelayURL: got %q want %q", loaded.RelayURL, want.RelayURL)
	}

	// File mode should be private (0o600).
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := st.Mode().Perm(); mode != 0o600 {
		t.Errorf("credential file mode: got %o want 0600", mode)
	}
}

func TestCredentialStoreAddCredentialFromJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewCredentialStore(filepath.Join(dir, "credential.json"))

	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	rec := crypto.NewPairingRecord("from-add-credential", "https://carrier-pigeon.fly.dev", kp, kp.Public)
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	src := filepath.Join(dir, "server-record.json")
	if err := os.WriteFile(src, raw, 0o600); err != nil {
		t.Fatalf("write src: %v", err)
	}

	// Server.AddCredential is a thin ReadFile+unmarshal+Save; exercise Save path.
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read src: %v", err)
	}
	var parsed crypto.PairingRecord
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := store.Save(&parsed); err != nil {
		t.Fatalf("Save from JSON: %v", err)
	}
	got := store.Get()
	if got == nil || got.PeerInstanceID != "from-add-credential" {
		t.Fatalf("after AddCredential-shaped Save: got %+v", got)
	}
}

func TestCredentialStoreSaveNilRejected(t *testing.T) {
	t.Parallel()
	store := NewCredentialStore(filepath.Join(t.TempDir(), "credential.json"))
	if err := store.Save(nil); err == nil {
		t.Fatal("expected error saving nil record")
	}
}
