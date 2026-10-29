// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

const projectionSchema = `
CREATE TABLE IF NOT EXISTS jv_plan_overrides (
 id INTEGER PRIMARY KEY, provider TEXT NOT NULL UNIQUE, override_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS jv_plan_history_legacy (
 id INTEGER PRIMARY KEY, provider TEXT NOT NULL, window TEXT NOT NULL,
 resets_key TEXT NOT NULL, fetched_at TEXT NOT NULL, remaining REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS jv_plan_history_series
 ON jv_plan_history_legacy(provider, window, resets_key, fetched_at);
CREATE TABLE IF NOT EXISTS jv_plan_migration (id INTEGER PRIMARY KEY, version INTEGER NOT NULL);
`

// OpenReplicaOverrides migrates the former JSON overrides and readings file
// once. Thereafter these local tables and Claudia's replicated observations
// are the query's only inputs. The source files remain untouched for rollback.
func OpenReplicaOverrides(replica Replica, stateDir string) (*OverrideStore, error) {
	err := replica.Write(func(db *sqlpipe.Database) error {
		if err := db.Exec(projectionSchema); err != nil {
			return err
		}
		qr, err := db.Query("SELECT version FROM jv_plan_migration WHERE id = 1")
		if err != nil {
			return err
		}
		if len(qr.Rows) != 0 {
			return nil
		}
		overrides, err := NewOverrideStore(filepath.Join(stateDir, OverrideFile)).Load()
		if err != nil {
			return err
		}
		legacy := DefaultReadingsPath(stateDir)
		_, statErr := os.Stat(legacy)
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for provider, override := range overrides {
			raw, err := json.Marshal(override)
			if err != nil {
				return err
			}
			if err := tx.Exec("INSERT INTO jv_plan_overrides(provider, override_json) VALUES (?, ?)", provider, string(raw)); err != nil {
				return err
			}
		}
		if statErr == nil {
			uri := url.URL{Scheme: "file", Path: legacy, RawQuery: "mode=ro"}
			source, err := sql.Open("sqlite", uri.String())
			if err != nil {
				return err
			}
			defer source.Close()
			rows, err := source.Query("SELECT id, provider, window, resets_key, fetched_at, remaining FROM plan_readings ORDER BY id")
			if err != nil {
				return fmt.Errorf("import plan usage history: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var id int64
				var provider, window, resets, fetched string
				var remaining float64
				if err := rows.Scan(&id, &provider, &window, &resets, &fetched, &remaining); err != nil {
					return err
				}
				if err := tx.Exec("INSERT INTO jv_plan_history_legacy(id,provider,window,resets_key,fetched_at,remaining) VALUES (?,?,?,?,?,?)", id, provider, window, resets, fetched, remaining); err != nil {
					return err
				}
			}
			if err := rows.Err(); err != nil {
				return err
			}
		}
		if err := tx.Exec("INSERT INTO jv_plan_migration(id, version) VALUES (1, 1)"); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, err
	}
	return &OverrideStore{replica: replica}, nil
}

func (o *OverrideStore) loadReplica() (map[string]Override, error) {
	out := map[string]Override{}
	err := o.replica.Read(func(db *sqlpipe.Database) error {
		qr, err := db.Query("SELECT provider, override_json FROM jv_plan_overrides ORDER BY provider")
		if err != nil {
			return err
		}
		for _, row := range qr.Rows {
			provider, ok := row[0].(string)
			if !ok {
				return fmt.Errorf("override provider has type %T", row[0])
			}
			raw, ok := row[1].(string)
			if !ok {
				return fmt.Errorf("override JSON has type %T", row[1])
			}
			var override Override
			if err := json.Unmarshal([]byte(raw), &override); err != nil {
				return err
			}
			out[provider] = override
		}
		return nil
	})
	return out, err
}
