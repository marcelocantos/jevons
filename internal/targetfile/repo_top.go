// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"path/filepath"
	"strings"
)

// RepositoryTop is the nearest ancestor of abs (inclusive) that holds a `.git`
// entry, or "" outside any git working tree.
func RepositoryTop(abs string) string {
	return gitTop(abs)
}

// WriteRoots maps a caller cwd to where ledger mutations must run (the
// repository's main working tree) and an optional linked worktree top to
// refresh after a successful write (🎯T1151.2).
func WriteRoots(callerCWD string) (commitTop, mirrorTop string) {
	dir := strings.TrimSpace(callerCWD)
	if dir == "" {
		return "", ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Clean(dir), ""
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	top := gitTop(abs)
	if top == "" {
		return abs, ""
	}
	main, ok := gitMainRoot(top)
	if !ok || main == "" {
		return top, ""
	}
	if top != main {
		return main, top
	}
	return main, ""
}
