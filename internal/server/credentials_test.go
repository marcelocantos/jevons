// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/marcelocantos/pigeon"
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

func TestCredentialStoreSaveIfAbsentNeverReplacesPairing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.json")
	existing := testPairingRecord(t, "existing-ipad")
	if err := NewCredentialStore(path).SaveIfAbsent(existing); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Two independent processes/stores can race; the filesystem is the authority.
	if err := NewCredentialStore(path).SaveIfAbsent(testPairingRecord(t, "fold")); err == nil {
		t.Fatal("silently replaced existing pairing")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatalf("existing credential changed: %v", err)
	}
}

func TestPairingEntryPointsAddWithoutReplacingPrimary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.json")
	store := NewCredentialStore(path)
	existing := testPairingRecord(t, "existing-ipad")
	if err := store.SaveIfAbsent(existing); err != nil {
		t.Fatal(err)
	}
	s := &Server{creds: store}
	artifact, err := s.MintArtifact("https://relay.invalid", "fold")
	if err != nil || artifact.Record.PeerInstanceID != "fold" {
		t.Fatalf("MintArtifact: %v", err)
	}
	if got := store.Get(); got == nil || got.PeerInstanceID != "existing-ipad" || !bytes.Equal(got.LocalPrivateKey, existing.LocalPrivateKey) {
		t.Fatal("primary pairing changed")
	}
	if store.GetPeer("fold") == nil {
		t.Fatal("fold not stored")
	}
	if _, err := s.MintArtifact("https://relay.invalid", "fold"); err == nil {
		t.Fatal("duplicate pair replaced channel")
	}
	other := testPairingRecord(t, "third")
	raw, err := json.Marshal(other)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "third.json")
	if err := os.WriteFile(source, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.AddCredential(source); err != nil {
		t.Fatal(err)
	}
	if store.GetPeer("third") == nil {
		t.Fatal("third peer not stored")
	}
	if _, err := NewCredentialStore(path).Load(); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialStoreLegacyMigrationAndMalformedHardError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credential.json")
	old := testPairingRecord(t, "original")
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	store := NewCredentialStore(path)
	if got, err := store.Load(); err != nil || got.PeerInstanceID != "original" {
		t.Fatalf("legacy Load: %v %v", got, err)
	}
	if err := store.Add(testPairingRecord(t, "fold")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var migrated credentialFile
	if err := json.Unmarshal(got, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.Version != 2 || migrated.Primary != "original" || len(migrated.Peers) != 2 || !bytes.Equal(migrated.Peers["original"].LocalPrivateKey, old.LocalPrivateKey) {
		t.Fatal("migration lost old peer")
	}
	for _, bad := range []string{`{`, `null`, `{}`, `{"version":2,"primary":"original","peers":{"original":{"peer_instance_id":"other"}}}`, `{"version":3,"primary":"original","peers":{}}`} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(path)
		if err := store.Add(testPairingRecord(t, "new")); err == nil {
			t.Fatalf("malformed %s accepted", bad)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("malformed credential overwritten")
		}
	}
}

func TestCredentialStoreConcurrentAddsNoLostPeer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.json")
	const n = 12
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		rec := testPairingRecord(t, fmt.Sprintf("peer-%02d", i))
		wg.Add(1)
		go func() { defer wg.Done(); errs <- NewCredentialStore(path).Add(rec) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	store := NewCredentialStore(path)
	if _, err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if len(store.PeerIDs()) != n {
		t.Fatalf("lost peer: %d/%d", len(store.PeerIDs()), n)
	}
}

func testPairingRecord(t *testing.T, id string) *crypto.PairingRecord {
	t.Helper()
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	return crypto.NewPairingRecord(id, "https://relay.invalid", kp, kp.Public)
}

func TestCredentialStoreChannelsAreIsolatedPerPeer(t *testing.T) {
	store := NewCredentialStore(filepath.Join(t.TempDir(), "credential.json"))
	artifacts := make(map[string]*pigeon.PairingArtifact)
	host := pigeon.NewPairingHost("https://relay.invalid")
	for _, id := range []string{"existing-ipad", "fold"} {
		artifact, record, err := host.Mint(id)
		if err != nil {
			t.Fatal(err)
		}
		artifacts[id] = artifact
		if err := store.Add(record); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Load(); err != nil {
		t.Fatal(err)
	}
	ipad := store.GetPeer("existing-ipad")
	fold := store.GetPeer("fold")
	if ipad == nil || fold == nil || ipad.PeerInstanceID == fold.PeerInstanceID {
		t.Fatal("peer routing collapsed")
	}
	ipadServer, err := ipad.DeriveChannel([]byte("server-to-client"), []byte("client-to-server"))
	if err != nil {
		t.Fatal(err)
	}
	foldServer, err := fold.DeriveChannel([]byte("server-to-client"), []byte("client-to-server"))
	if err != nil {
		t.Fatal(err)
	}
	foldClient, err := artifacts["fold"].Record.DeriveChannel([]byte("client-to-server"), []byte("server-to-client"))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := foldClient.Encrypt([]byte(`{"type":"plan_usage_request","id":"fold"}`))
	if _, err := ipadServer.Decrypt(ciphertext); err == nil {
		t.Fatal("iPad channel decrypted Fold frame")
	}
	if plaintext, err := foldServer.Decrypt(ciphertext); err != nil || !bytes.Contains(plaintext, []byte(`"id":"fold"`)) {
		t.Fatalf("Fold channel failed: %s %v", plaintext, err)
	}
}

func TestCredentialStoreRejectsMismatchedPublicKey(t *testing.T) {
	rec := testPairingRecord(t, "peer")
	rec.LocalPublicKey[0] ^= 1
	store := NewCredentialStore(filepath.Join(t.TempDir(), "credential.json"))
	if err := store.Add(rec); err == nil {
		t.Fatal("stored inconsistent X25519 keypair")
	}
}

func TestCredentialStoreRejectsUnroutablePeerIDs(t *testing.T) {
	store := NewCredentialStore(filepath.Join(t.TempDir(), "credential.json"))
	for _, id := range []string{strings.Repeat("x", 65), "bad:relay", "peer/other"} {
		if err := store.Add(testPairingRecord(t, id)); err == nil {
			t.Fatalf("stored unroutable peer %q", id)
		}
	}
}
