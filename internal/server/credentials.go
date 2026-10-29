// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"crypto/ecdh"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/marcelocantos/pigeon/crypto"
)

// CredentialStore preserves the legacy single-record file and, when a second
// peer is added, upgrades it to a versioned map keyed by peer instance ID.
// The first peer remains the primary (the existing iOS connection). No read
// ever treats a malformed file as an empty store.
type CredentialStore struct {
	mu      sync.RWMutex
	path    string
	primary string
	records map[string]*crypto.PairingRecord
}

type credentialFile struct {
	Version int                              `json:"version"`
	Primary string                           `json:"primary"`
	Peers   map[string]*crypto.PairingRecord `json:"peers"`
}

func NewCredentialStore(path string) *CredentialStore { return &CredentialStore{path: path} }

func decodeCredential(data []byte) (*credentialFile, error) {
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(data, &shape); err != nil {
		return nil, fmt.Errorf("parse credential: %w", err)
	}
	if shape == nil {
		return nil, errors.New("parse credential: expected object")
	}
	if _, v2 := shape["version"]; !v2 {
		var rec crypto.PairingRecord
		if err := strictJSON(data, &rec); err != nil {
			return nil, fmt.Errorf("parse legacy credential: %w", err)
		}
		if err := validateRecord(&rec); err != nil {
			return nil, fmt.Errorf("invalid legacy credential: %w", err)
		}
		return &credentialFile{Version: 2, Primary: rec.PeerInstanceID, Peers: map[string]*crypto.PairingRecord{rec.PeerInstanceID: &rec}}, nil
	}
	var state credentialFile
	if err := strictJSON(data, &state); err != nil {
		return nil, fmt.Errorf("parse credential: %w", err)
	}
	if state.Version != 2 || len(state.Peers) == 0 || state.Primary == "" || state.Peers[state.Primary] == nil {
		return nil, errors.New("invalid credential store: version, primary or peers")
	}
	for id, rec := range state.Peers {
		if id == "" || rec == nil || id != rec.PeerInstanceID {
			return nil, fmt.Errorf("invalid credential peer %q: key mismatch", id)
		}
		if err := validateRecord(rec); err != nil {
			return nil, fmt.Errorf("invalid credential peer %q: %w", id, err)
		}
	}
	return &state, nil
}

func strictJSON(data []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return errors.New("trailing credential data")
	}
	return nil
}

func validateRecord(rec *crypto.PairingRecord) error {
	if rec == nil || rec.PeerInstanceID == "" || rec.RelayURL == "" || len(rec.LocalPrivateKey) != 32 || len(rec.LocalPublicKey) != 32 || len(rec.PeerPublicKey) != 32 {
		return errors.New("missing peer, relay or X25519 keys")
	}
	// CONNECT accepts at most 64 bytes of instance ID. Restrict IDs here so
	// adding a peer can never persist a record that cannot be reached.
	if len(rec.PeerInstanceID) > 64 {
		return errors.New("peer instance ID exceeds relay limit")
	}
	for _, r := range rec.PeerInstanceID {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return errors.New("peer instance ID contains unsafe character")
		}
	}
	private, err := ecdh.X25519().NewPrivateKey(rec.LocalPrivateKey)
	if err != nil {
		return fmt.Errorf("invalid local key: %w", err)
	}
	if !bytes.Equal(private.PublicKey().Bytes(), rec.LocalPublicKey) {
		return errors.New("local X25519 public/private keys do not match")
	}
	if _, err := rec.DeriveChannel([]byte("server-to-client"), []byte("client-to-server")); err != nil {
		return fmt.Errorf("invalid channel keys: %w", err)
	}
	return nil
}

func readCredentials(path string) (*credentialFile, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read credential: %w", err)
	}
	return decodeCredential(data)
}

func cloneRecord(rec *crypto.PairingRecord) *crypto.PairingRecord {
	v := *rec
	v.LocalPrivateKey = append([]byte(nil), rec.LocalPrivateKey...)
	v.LocalPublicKey = append([]byte(nil), rec.LocalPublicKey...)
	v.PeerPublicKey = append([]byte(nil), rec.PeerPublicKey...)
	return &v
}

func (s *CredentialStore) cache(state *credentialFile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.primary = ""
	s.records = nil
	if state != nil {
		s.primary = state.Primary
		s.records = make(map[string]*crypto.PairingRecord, len(state.Peers))
		for id, rec := range state.Peers {
			s.records[id] = cloneRecord(rec)
		}
	}
}

// Load reads disk, failing closed on malformed data. Its legacy return is the
// original primary record, never a randomly selected second peer.
func (s *CredentialStore) Load() (*crypto.PairingRecord, error) {
	state, err := readCredentials(s.path)
	if err != nil {
		return nil, err
	}
	s.cache(state)
	return s.Get(), nil
}

// Get returns the existing primary peer for callers using the legacy API.
func (s *CredentialStore) Get() *crypto.PairingRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.records == nil {
		return nil
	}
	rec := s.records[s.primary]
	if rec == nil {
		return nil
	}
	return cloneRecord(rec)
}

// GetPeer returns one exact peer; a missing ID never falls back to primary.
func (s *CredentialStore) GetPeer(id string) *crypto.PairingRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.records == nil || s.records[id] == nil {
		return nil
	}
	return cloneRecord(s.records[id])
}

func (s *CredentialStore) PeerIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.records))
	for id := range s.records {
		ids = append(ids, id)
	}
	return ids
}

// Add writes a distinct peer without replacing the existing peer. An OS file
// lock serialises independent daemon/one-shot processes; a unique temporary
// 0600 file is fsynced and renamed over credential.json atomically. A malformed
// existing store is a hard error, not an invitation to overwrite it.
func (s *CredentialStore) Add(rec *crypto.PairingRecord) error { return s.write(rec, false) }

// SaveIfAbsent retains the old refusal contract for callers explicitly
// requiring an empty store.
func (s *CredentialStore) SaveIfAbsent(rec *crypto.PairingRecord) error { return s.write(rec, true) }

// Save is only safe when no peer exists; replacement requires an explicit
// migration operation, never a silent overwrite of the owner's other device.
func (s *CredentialStore) Save(rec *crypto.PairingRecord) error { return s.SaveIfAbsent(rec) }

func (s *CredentialStore) write(rec *crypto.PairingRecord, emptyOnly bool) error {
	if err := validateRecord(rec); err != nil {
		return fmt.Errorf("save credential: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("mkdir credential dir: %w", err)
	}
	lock, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open credential lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock credential: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	state, err := readCredentials(s.path)
	if err != nil {
		return err
	}
	if state != nil {
		if emptyOnly {
			return errors.New("pairing would replace existing credential.json; add another peer explicitly or remove/backup the existing pairing")
		}
		if state.Peers[rec.PeerInstanceID] != nil {
			return fmt.Errorf("peer %q already paired; refusing to replace its channel", rec.PeerInstanceID)
		}
	} else {
		state = &credentialFile{Version: 2, Primary: rec.PeerInstanceID, Peers: make(map[string]*crypto.PairingRecord)}
	}
	state.Peers[rec.PeerInstanceID] = cloneRecord(rec)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".credential-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("rename credential: %w", err)
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	s.cache(state)
	return nil
}

func (s *CredentialStore) Path() string { return s.path }
