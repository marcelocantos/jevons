// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // Apple's sqlite3 -readonly cannot open Cursor state.vscdb (exit 14)
)

func defaultCursorAuthPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home for cursor auth: %w", err)
	}
	mac := filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb")
	if _, err := os.Stat(mac); err == nil {
		return mac, nil
	}
	return filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb"), nil
}

// loadCursorAccessToken reads cursorAuth/accessToken from the IDE state db.
// Never log the token. Empty path uses the desktop-app default.
func loadCursorAccessToken(path string) (string, error) {
	if path == "" {
		p, err := defaultCursorAuthPath()
		if err != nil {
			return "", err
		}
		path = p
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("read cursor auth (set %s or sign in to Cursor): %w", CursorAPIKeyEnv, err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return "", fmt.Errorf("open cursor auth db: %w", err)
	}
	defer db.Close()
	var tok string
	err = db.QueryRow(`SELECT value FROM ItemTable WHERE key = 'cursorAuth/accessToken' LIMIT 1`).Scan(&tok)
	if err != nil {
		return "", fmt.Errorf("read cursor auth db: %w", err)
	}
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return "", fmt.Errorf("no cursorAuth/accessToken in %s (set %s or sign in to Cursor)", path, CursorAPIKeyEnv)
	}
	return tok, nil
}

// CursorAPIKeyEnv is the claudia / Cursor CLI token override.
const CursorAPIKeyEnv = "CURSOR_API_KEY"
