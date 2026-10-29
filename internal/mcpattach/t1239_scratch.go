// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpattach

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// playwrightScratchRoot names the directory under which every seat's
// Playwright scratch dir is minted. Exported as a var (not a const) so a
// test can point it somewhere disposable without touching the real OS
// temp tree.
var playwrightScratchRoot = filepath.Join(os.TempDir(), "jevons-playwright-scratch")

// playwrightScratchDir is the per-seat directory Playwright MCP's
// workspace root is rewritten to (🎯T1239). It is deterministic in
// workDir (so repeated SessionServers calls for the same seat agree) and
// lives under the OS temp root, never inside a git checkout: a seat's
// screenshot — named or auto-named — resolves there instead of landing
// as untracked litter in a shared clone (2026-10-28 jv-t1130 incident).
func playwrightScratchDir(workDir string) string {
	sum := sha256.Sum256([]byte(workDir))
	return filepath.Join(playwrightScratchRoot, hex.EncodeToString(sum[:8]))
}

// wrapCommandWithCwd rewrites command/args so the process runs with dir as
// its working directory, without a shell re-interpreting any argument.
// Playwright MCP resolves both its --output-dir default and any
// explicit-filename screenshot path against its own cwd ("workspace
// root"); claudia.MCPServer has no Cwd field, so a cd-then-exec shell
// wrapper is the only way to move it (🎯T1239). Each original argv
// element stays a single, unexpanded "$@" slot — sh never sees the
// command or its arguments as a string to parse.
func wrapCommandWithCwd(command string, args []string, dir string) (string, []string) {
	if command == "" {
		return command, args
	}
	const script = `cd "$1" && shift && exec "$@"`
	out := make([]string, 0, len(args)+4)
	out = append(out, "-c", script, "sh", dir, command)
	out = append(out, args...)
	return "sh", out
}

// unwrapScratchCwd undoes wrapCommandWithCwd when command/args are already
// in that shape, so re-running ensurePlaywrightIsolated on an already-moved
// server rewraps the inner command fresh instead of nesting another "sh -c"
// layer around the previous one each time.
func unwrapScratchCwd(command string, args []string) (string, []string) {
	if command != "sh" || len(args) < 5 {
		return command, args
	}
	if args[0] != "-c" || args[1] != `cd "$1" && shift && exec "$@"` || args[2] != "sh" {
		return command, args
	}
	// args[3] is the scratch dir, args[4] is the inner command, the rest
	// are the inner command's own arguments.
	return args[4], append([]string(nil), args[5:]...)
}
