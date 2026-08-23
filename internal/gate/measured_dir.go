// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"path/filepath"
	"strings"
)

// This file is 🎯T467: a gate's tree-provenance line describes the tree the
// command actually ran in, not the directory the gate process was launched
// from.
//
// # The defect
//
// Observed live while gating 🎯T443: a detached worktree at HEAD was verified
// clean (dirty=0), and `bin/gate -- go test -C $WT ./internal/gate/` was run
// from the shared clone carrying 119 uncommitted files. The suite passed —
// and the gate stamped `tree=dirty+119@…` with a 🎯T397 caveat naming files
// that were not in the tree under test. Provenance was read from the
// invoking cwd. In that direction it only understates confidence. The inverse
// launders evidence: `cd` to a clean checkout, gate a command that tests the
// dirty shared tree via `-C`, and the record reads clean. That is the
// substitution 🎯T397 exists to prevent, so the provenance line has to be as
// trustworthy as the exit status beside it.
//
// # The rule
//
//  1. An explicit chdir in the argv (`go`/`make`/`git` `-C`, make
//     `--directory`, …) wins — that is where the command looked.
//  2. Otherwise an explicit gate Dir (`-dir`, RunClean's worktree) wins —
//     that is the process cwd the command was handed.
//  3. Otherwise a plain command with no self-chdir runs in the process cwd,
//     which is known.
//  4. An opaque shell (`sh -c …`) or a malformed chdir flag is
//     undeterminable: leave Tree nil. Never stamp the launcher's own state
//     as a substitute (same rule as exit=unknown is not a pass).

// ResolveMeasuredDir returns the directory whose tree provenance should be
// recorded for a gated command.
//
// ok is false when the runner cannot determine which tree the command
// touched. The caller must then leave Record.Tree nil rather than probing
// the launcher's cwd.
func ResolveMeasuredDir(argv []string, gateDir string) (dir string, ok bool) {
	if d, declared, usable := toolChdir(argv); declared {
		if !usable {
			return "", false
		}
		return absAgainst(gateDir, d), true
	}
	if gateDir != "" {
		return gateDir, true
	}
	if opaqueShell(argv) {
		return "", false
	}
	// Empty gateDir means the process cwd, which is where a non-redirecting
	// command actually runs. ProbeTree("") reads that cwd via git.
	return "", true
}

// toolChdir extracts a directory a well-known tool was told to run in.
// declared is true when the argv asserted a chdir (even a broken one);
// usable is false when the assertion cannot be turned into a path.
func toolChdir(argv []string) (dir string, declared, usable bool) {
	if len(argv) == 0 {
		return "", false, false
	}
	base := strings.TrimSuffix(filepath.Base(argv[0]), ".exe")
	switch base {
	case "go", "git", "make", "gmake":
	default:
		return "", false, false
	}
	args := argv[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		switch {
		case a == "-C" || a == "--directory" || a == "--chdir":
			if i+1 >= len(args) {
				return "", true, false
			}
			d := args[i+1]
			if d == "" || strings.HasPrefix(d, "-") {
				return "", true, false
			}
			return d, true, true
		case strings.HasPrefix(a, "--directory="):
			d := strings.TrimPrefix(a, "--directory=")
			if d == "" {
				return "", true, false
			}
			return d, true, true
		case strings.HasPrefix(a, "--chdir="):
			d := strings.TrimPrefix(a, "--chdir=")
			if d == "" {
				return "", true, false
			}
			return d, true, true
		case base == "make" || base == "gmake":
			// make accepts the glued form -Cdir.
			if len(a) > 2 && strings.HasPrefix(a, "-C") {
				return a[2:], true, true
			}
		}
	}
	return "", false, false
}

// opaqueShell reports a command whose working directory cannot be read from
// the argv: a shell invoked with -c runs an opaque script that may cd.
func opaqueShell(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	base := strings.TrimSuffix(filepath.Base(argv[0]), ".exe")
	switch base {
	case "sh", "bash", "zsh", "dash", "ksh":
	default:
		return false
	}
	for _, a := range argv[1:] {
		if a == "-c" {
			return true
		}
		if a == "--" {
			break
		}
	}
	return false
}

// absAgainst resolves rel against base when rel is not absolute. An empty
// base means the process cwd — the same rule os/exec uses for cmd.Dir.
func absAgainst(base, rel string) string {
	if rel == "" || filepath.IsAbs(rel) {
		return rel
	}
	if base == "" {
		abs, err := filepath.Abs(rel)
		if err != nil {
			return rel
		}
		return abs
	}
	if filepath.IsAbs(base) {
		return filepath.Join(base, rel)
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return filepath.Join(base, rel)
	}
	return filepath.Join(absBase, rel)
}
