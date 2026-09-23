// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// started-model.json is written by claudia next to the Cursor session's
// store.db when a process starts with no --model pin. model is the CLI
// default at exec. pid is that process. The badge shows it only while the
// seat's connect_pid is still that process. It is not a launch pin.

const cursorStartedModelFile = "started-model.json"

type cursorStartedModelJSON struct {
	Model string `json:"model"`
	PID   int    `json:"pid"`
}

func cursorStartedModel(sessionID string) (model string, pid int, ok bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || sessionID == "." || sessionID == ".." || strings.ContainsAny(sessionID, `/\`) {
		return "", 0, false
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", 0, false
	}
	b, err := os.ReadFile(filepath.Join(home, ".cursor", "acp-sessions", sessionID, cursorStartedModelFile))
	if err != nil {
		return "", 0, false
	}
	var rec cursorStartedModelJSON
	if err := json.Unmarshal(b, &rec); err != nil {
		return "", 0, false
	}
	rec.Model = strings.TrimSpace(rec.Model)
	if rec.Model == "" || rec.PID <= 0 {
		return "", 0, false
	}
	return rec.Model, rec.PID, true
}
