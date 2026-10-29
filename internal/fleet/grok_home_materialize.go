// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/discovery"
)

// MaterializeExclusiveGrokHome ensures $XDG_STATE_HOME/claudia/grok-homes/<session>
// exists with the minimal exclusive-MCP layout Claudia expects before a
// materialized Grok seat resumes (🎯T838 destination side of 🎯T625.9).
// Idempotent when the home is already present and valid.
func MaterializeExclusiveGrokHome(sessionID string) error {
	sid := strings.TrimSpace(sessionID)
	if sid == "" || sid == "." || sid == ".." || strings.ContainsAny(sid, `/\`) {
		return fmt.Errorf("exclusive GROK_HOME: invalid session identity")
	}
	root := discovery.ClaudiaGrokHomesRoot()
	if root == "" {
		return fmt.Errorf("exclusive GROK_HOME: state home unknown")
	}
	home := filepath.Join(root, sid)
	if err := exclusiveGrokHomeResumeCheck(home, sid); err != nil {
		return err
	}
	if dirExists(home) {
		return nil
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("exclusive GROK_HOME unavailable for session %s: %w", sid, err)
	}
	return writeExclusiveGrokHomeLayout(home)
}

// exclusiveGrokHomeResumeCheck mirrors claudia exclusiveGrokHomeForStart's
// requireResume=true refusal when the path exists but is unusable.
func exclusiveGrokHomeResumeCheck(home, sid string) error {
	if home == "" {
		return nil
	}
	info, err := os.Stat(home)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("exclusive GROK_HOME unavailable for session %s: %w", sid, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("exclusive GROK_HOME is not a directory: %s", home)
	}
	return nil
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// writeExclusiveGrokHomeLayout matches claudia mcp_exclusive.writeExclusiveGrokHome
// (compat MCP discovery off; auth copied when present).
func writeExclusiveGrokHomeLayout(dest string) error {
	for _, name := range []string{"auth.json", "config.toml"} {
		path := filepath.Join(dest, name)
		if info, err := os.Lstat(path); err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("exclusive GROK_HOME configuration is not a regular file: %s", path)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	auth, err := os.ReadFile(filepath.Join(userHome, ".grok", "auth.json"))
	if err == nil {
		if err := atomicWriteRegular(dest, "auth.json", auth); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	body := "# claudia MCPExclusive\n" +
		"[compat.claude]\nmcps = false\n\n" +
		"[compat.cursor]\nmcps = false\n"
	return atomicWriteRegular(dest, "config.toml", []byte(body))
}

func atomicWriteRegular(home, name string, body []byte) error {
	path := filepath.Join(home, name)
	f, err := os.CreateTemp(home, ".config-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
