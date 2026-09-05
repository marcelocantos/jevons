// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"database/sql"
	"fmt"
	"net/url"

	"github.com/marcelocantos/jevons/internal/statedb"
)

// Inspect the store the mux actually writes. Read-only mode cannot manufacture
// a missing database or migrate an empty fixture into plausible evidence.
func assertIsolateTranscript(stateDir string) error {
	uri := url.URL{Scheme: "file", Path: statedb.DefaultPath(stateDir), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return err
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM transcript_events WHERE agent = ?", overseerName).Scan(&n); err != nil {
		return fmt.Errorf("isolate transcript store: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("isolate transcript store has no %s events", overseerName)
	}
	return nil
}

// Inspect actual canonical rows rather than the retired JSONL file or a
// generic row count. Opening read-only cannot manufacture missing evidence.
func assertStoredOwnerRoundTrip(stateDir, prompt, reply string) error {
	uri := url.URL{Scheme: "file", Path: statedb.DefaultPath(stateDir), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.Query("SELECT body FROM transcript_events WHERE agent = ? ORDER BY idx", overseerName)
	if err != nil {
		return err
	}
	defer rows.Close()
	var bodies [][]byte
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return err
		}
		bodies = append(bodies, []byte(body))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return assertRecordedOwnerReply(bodies, prompt, reply)
}
