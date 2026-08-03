// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
	_ "modernc.org/sqlite" // pure-Go sqlite driver, no cgo
)

// Store is jevonsd's persistence home for provider/registry state (🎯T27.2).
// It is the first dedicated provider datastore under StateDir: desired
// config rows, last-known manifests, and feed cursors. Schema uses
// CREATE IF NOT EXISTS and a schema_version row so later T27.* targets
// can add tables/columns without breaking older files.
//
// Not a second source of truth for desired config — that remains
// config.yaml. The store records what was last applied and runtime
// state (cursors, manifests) reconstructable from feeds when needed.
type Store struct {
	db   *sql.DB
	path string
}

// SchemaVersion is the store format version. Bump only for incompatible
// layout changes; additive tables/columns keep the same version when
// guarded by IF NOT EXISTS.
const SchemaVersion = 1

// DefaultStorePath returns StateDir/providers/registry.db.
func DefaultStorePath(stateDir string) string {
	return filepath.Join(stateDir, "providers", "registry.db")
}

const storeSchema = `
CREATE TABLE IF NOT EXISTS schema_meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS desired_providers (
	id           TEXT PRIMARY KEY,
	transport    TEXT NOT NULL,
	enable       INTEGER NOT NULL,
	params_json  TEXT NOT NULL DEFAULT '{}',
	updated_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS registry_state (
	id             TEXT PRIMARY KEY,
	version        TEXT NOT NULL DEFAULT '',
	contract       TEXT NOT NULL DEFAULT '',
	manifest_json  TEXT NOT NULL DEFAULT '',
	egress         INTEGER NOT NULL DEFAULT 0,
	updated_at     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS feed_cursors (
	provider_id TEXT NOT NULL,
	feed        TEXT NOT NULL,
	seq         INTEGER NOT NULL,
	updated_at  TEXT NOT NULL,
	PRIMARY KEY (provider_id, feed)
);
`

// OpenStore opens (or creates) the provider registry database at path.
// Parent directories are created. Use ":memory:" for hermetic tests.
func OpenStore(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("provider store: mkdir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("provider store: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("provider store: %s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(storeSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("provider store: schema: %w", err)
	}
	s := &Store{db: db, path: path}
	if err := s.ensureSchemaVersion(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) ensureSchemaVersion() error {
	var v string
	err := s.db.QueryRow(`SELECT value FROM schema_meta WHERE key = 'schema_version'`).Scan(&v)
	if err == sql.ErrNoRows {
		_, err = s.db.Exec(
			`INSERT INTO schema_meta(key, value) VALUES ('schema_version', ?)`,
			fmt.Sprintf("%d", SchemaVersion),
		)
		return err
	}
	if err != nil {
		return fmt.Errorf("provider store: schema_version: %w", err)
	}
	// Forward-compatible: older or equal versions OK; refuse future majors
	// only if we cannot read (kept simple: any present value is accepted;
	// migrations land with T27.3+ when needed).
	return nil
}

// Path returns the on-disk path (or ":memory:").
func (s *Store) Path() string { return s.path }

// Close closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DesiredRow is one persisted desired-provider snapshot from config.
type DesiredRow struct {
	ID        string
	Transport string
	Enable    bool
	Params    map[string]any
	UpdatedAt time.Time
}

// ReplaceDesired writes the full desired set from config (atomic replace).
// Call after config load/reload so the store mirrors structured config.
func (s *Store) ReplaceDesired(decls []config.ProviderDecl) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("provider store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM desired_providers`); err != nil {
		return fmt.Errorf("provider store: clear desired: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, d := range decls {
		params := d.Params
		if params == nil {
			params = map[string]any{}
		}
		raw, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("provider store: marshal params %q: %w", d.ID, err)
		}
		en := 0
		if d.Enabled() {
			en = 1
		}
		_, err = tx.Exec(
			`INSERT INTO desired_providers(id, transport, enable, params_json, updated_at)
			 VALUES (?, ?, ?, ?, ?)`,
			d.ID, d.Transport, en, string(raw), now,
		)
		if err != nil {
			return fmt.Errorf("provider store: insert desired %q: %w", d.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("provider store: commit desired: %w", err)
	}
	return nil
}

// ListDesired returns persisted desired providers (order not significant).
func (s *Store) ListDesired() ([]DesiredRow, error) {
	rows, err := s.db.Query(
		`SELECT id, transport, enable, params_json, updated_at FROM desired_providers ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("provider store: list desired: %w", err)
	}
	defer rows.Close()
	var out []DesiredRow
	for rows.Next() {
		var r DesiredRow
		var en int
		var paramsJSON, updated string
		if err := rows.Scan(&r.ID, &r.Transport, &en, &paramsJSON, &updated); err != nil {
			return nil, err
		}
		r.Enable = en != 0
		r.Params = map[string]any{}
		if strings.TrimSpace(paramsJSON) != "" {
			if err := json.Unmarshal([]byte(paramsJSON), &r.Params); err != nil {
				return nil, fmt.Errorf("provider store: params %q: %w", r.ID, err)
			}
		}
		if t, err := time.Parse(time.RFC3339Nano, updated); err == nil {
			r.UpdatedAt = t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PutManifest persists last-known describe for a provider (runtime state).
func (s *Store) PutManifest(m Manifest) error {
	if err := ValidateManifest(m); err != nil {
		return err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("provider store: marshal manifest: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	eg := 0
	if m.Egress {
		eg = 1
	}
	_, err = s.db.Exec(
		`INSERT INTO registry_state(id, version, contract, manifest_json, egress, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   version=excluded.version,
		   contract=excluded.contract,
		   manifest_json=excluded.manifest_json,
		   egress=excluded.egress,
		   updated_at=excluded.updated_at`,
		m.ID, m.Version, m.Contract, string(raw), eg, now,
	)
	if err != nil {
		return fmt.Errorf("provider store: put manifest %q: %w", m.ID, err)
	}
	return nil
}

// GetManifest loads a previously stored manifest, if any.
func (s *Store) GetManifest(id string) (Manifest, bool, error) {
	var raw string
	err := s.db.QueryRow(
		`SELECT manifest_json FROM registry_state WHERE id = ?`, id,
	).Scan(&raw)
	if err == sql.ErrNoRows {
		return Manifest{}, false, nil
	}
	if err != nil {
		return Manifest{}, false, fmt.Errorf("provider store: get manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return Manifest{}, false, fmt.Errorf("provider store: parse manifest: %w", err)
	}
	return m, true, nil
}

// SetCursor persists a feed cursor (last folded seq).
func (s *Store) SetCursor(providerID, feed string, seq int64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`INSERT INTO feed_cursors(provider_id, feed, seq, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(provider_id, feed) DO UPDATE SET
		   seq=excluded.seq,
		   updated_at=excluded.updated_at`,
		providerID, feed, seq, now,
	)
	if err != nil {
		return fmt.Errorf("provider store: set cursor: %w", err)
	}
	return nil
}

// GetCursor returns the last stored seq, or 0 if none.
func (s *Store) GetCursor(providerID, feed string) (int64, error) {
	var seq int64
	err := s.db.QueryRow(
		`SELECT seq FROM feed_cursors WHERE provider_id = ? AND feed = ?`,
		providerID, feed,
	).Scan(&seq)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("provider store: get cursor: %w", err)
	}
	return seq, nil
}

// ListCursors returns all cursors for a provider (feed → seq).
func (s *Store) ListCursors(providerID string) (map[string]int64, error) {
	rows, err := s.db.Query(
		`SELECT feed, seq FROM feed_cursors WHERE provider_id = ?`, providerID,
	)
	if err != nil {
		return nil, fmt.Errorf("provider store: list cursors: %w", err)
	}
	defer rows.Close()
	out := make(map[string]int64)
	for rows.Next() {
		var feed string
		var seq int64
		if err := rows.Scan(&feed, &seq); err != nil {
			return nil, err
		}
		out[feed] = seq
	}
	return out, rows.Err()
}
