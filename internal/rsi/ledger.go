// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultLedgerTTL is how long a minted fingerprint suppresses re-mint.
const DefaultLedgerTTL = 14 * 24 * time.Hour

// LedgerEntry records a prior mint for noise control.
type LedgerEntry struct {
	Fingerprint string    `json:"fingerprint"`
	ID          string    `json:"id,omitempty"`
	Name        string    `json:"name,omitempty"`
	MintedAt    time.Time `json:"minted_at"`
}

// Ledger is a durable set of recent mint fingerprints under state_dir/rsi/.
type Ledger struct {
	path string
	ttl  time.Duration
}

// OpenLedger loads (or creates) the mint ledger at stateDir/rsi/minted.json.
func OpenLedger(stateDir string, ttl time.Duration) (*Ledger, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, fmt.Errorf("rsi ledger: stateDir required")
	}
	if ttl <= 0 {
		ttl = DefaultLedgerTTL
	}
	dir := filepath.Join(stateDir, "rsi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("rsi ledger: mkdir: %w", err)
	}
	return openLedgerAt(filepath.Join(dir, "minted.json"), ttl)
}

// openLedgerAt opens a fingerprint ledger at an explicit path (mint or judged).
func openLedgerAt(path string, ttl time.Duration) (*Ledger, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("rsi ledger: path required")
	}
	if ttl <= 0 {
		ttl = DefaultLedgerTTL
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("rsi ledger: mkdir: %w", err)
	}
	return &Ledger{path: path, ttl: ttl}, nil
}

// Path returns the ledger file path.
func (l *Ledger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// ActiveFingerprints returns non-expired mint fingerprints.
func (l *Ledger) ActiveFingerprints(now time.Time) ([]string, error) {
	entries, err := l.load()
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var out []string
	for _, e := range entries {
		if e.Fingerprint == "" {
			continue
		}
		if now.Sub(e.MintedAt) > l.ttl {
			continue
		}
		out = append(out, e.Fingerprint)
	}
	return out, nil
}

// Record appends filed mints and prunes expired entries (atomic write).
func (l *Ledger) Record(filed []Filed, now time.Time) error {
	if l == nil {
		return fmt.Errorf("rsi ledger: nil")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	entries, err := l.load()
	if err != nil {
		return err
	}
	// Prune expired.
	kept := entries[:0]
	for _, e := range entries {
		if e.Fingerprint == "" {
			continue
		}
		if now.Sub(e.MintedAt) > l.ttl {
			continue
		}
		kept = append(kept, e)
	}
	for _, f := range filed {
		if f.Fingerprint == "" {
			continue
		}
		kept = append(kept, LedgerEntry{
			Fingerprint: f.Fingerprint,
			ID:          f.ID,
			Name:        f.Name,
			MintedAt:    now,
		})
	}
	return l.save(kept)
}

type ledgerFile struct {
	Entries []LedgerEntry `json:"entries"`
}

func (l *Ledger) load() ([]LedgerEntry, error) {
	data, err := os.ReadFile(l.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("rsi ledger: read: %w", err)
	}
	if len(bytesTrimSpace(data)) == 0 {
		return nil, nil
	}
	var lf ledgerFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("rsi ledger: parse %s: %w", l.path, err)
	}
	return lf.Entries, nil
}

func (l *Ledger) save(entries []LedgerEntry) error {
	lf := ledgerFile{Entries: entries}
	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return fmt.Errorf("rsi ledger: marshal: %w", err)
	}
	data = append(data, '\n')
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("rsi ledger: write tmp: %w", err)
	}
	if err := os.Rename(tmp, l.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rsi ledger: rename: %w", err)
	}
	return nil
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
