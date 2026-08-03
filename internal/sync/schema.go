// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sync

// SyncSchemaDDL is the table set for sqlpipe Peer state sync (🎯T10).
// Server owns transcript/sessions/scripts/state; client owns requests.
// Executed against a sqlpipe Database when Peer integration returns.
var SyncSchemaDDL = []string{
	`CREATE TABLE IF NOT EXISTS sync_transcript (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		role      TEXT    NOT NULL,
		content   TEXT    NOT NULL,
		timestamp TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS sessions (
		id       TEXT PRIMARY KEY,
		name     TEXT NOT NULL DEFAULT '',
		status   TEXT NOT NULL DEFAULT 'idle',
		workdir  TEXT NOT NULL DEFAULT '',
		active   INTEGER NOT NULL DEFAULT 0,
		score    REAL NOT NULL DEFAULT 0,
		mod_time TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS scripts (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		name       TEXT    NOT NULL UNIQUE,
		source     TEXT    NOT NULL,
		updated_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`CREATE TABLE IF NOT EXISTS server_state (
		id             INTEGER PRIMARY KEY DEFAULT 1,
		status         TEXT NOT NULL DEFAULT 'idle',
		version        TEXT NOT NULL DEFAULT '',
		streaming_text TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS requests (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		type       TEXT NOT NULL,
		payload    TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`,
	`INSERT OR IGNORE INTO server_state (id) VALUES (1)`,
}

// ServerOwnedTables are mastered by jevonsd under Peer ownership rules.
var ServerOwnedTables = []string{
	"sync_transcript",
	"sessions",
	"scripts",
	"server_state",
}

// ClientOwnedTables are mastered by the client (iOS SyncPeer / web peer).
var ClientOwnedTables = []string{
	"requests",
}
