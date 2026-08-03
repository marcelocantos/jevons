// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package harnessusage

import (
	"os"
	"path/filepath"
	"strings"
)

// resolveClaudeProjects picks the Claude projects tree.
// Roots[claude] may be ~/.claude (live) or a fixture parent that contains
// projects/, or the projects directory itself.
func resolveClaudeProjects(root string) string {
	if root == "" {
		return root
	}
	cand := filepath.Join(root, "projects")
	if isDir(cand) {
		return cand
	}
	return root
}

// resolveGrokSessions picks the Grok sessions tree.
// Roots[grok] may be ~/.grok or a fixture parent with sessions/, or the
// sessions directory itself.
func resolveGrokSessions(root string) string {
	if root == "" {
		return root
	}
	cand := filepath.Join(root, "sessions")
	if isDir(cand) {
		return cand
	}
	return root
}

// resolveCodexHome picks the Codex home containing sessions/.
func resolveCodexHome(root string) string {
	return root
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func sessionIDFromClaudePath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}

func sessionIDFromGrokPath(path string) string {
	// …/<session-id>/updates.jsonl
	return filepath.Base(filepath.Dir(path))
}
