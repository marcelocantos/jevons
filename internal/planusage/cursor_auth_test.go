// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestLoadCursorAccessTokenReadsItemTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE ItemTable (key TEXT, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ItemTable VALUES ('cursorAuth/accessToken', 'test-cursor-token')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	tok, err := loadCursorAccessToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "test-cursor-token" {
		t.Fatalf("token %q", tok)
	}
}

func TestLoadCursorAccessTokenMissingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE ItemTable (key TEXT, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCursorAccessToken(path); err == nil {
		t.Fatal("expected error for empty ItemTable")
	}
}
