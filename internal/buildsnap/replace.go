// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package buildsnap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 🎯T473 — a relative replace directive means something different inside the
// snapshot, so the snapshot must say what the clone meant.
//
// THE FAULT. go.mod carries `replace github.com/marcelocantos/claudia =>
// ../claudia`, and go resolves that relative to the MODULE ROOT it is
// building. In the shared clone the module root is
// ~/work/github.com/marcelocantos/jevons, so `..` is the org directory and the
// sibling checkout is there. In the 🎯T254.2 snapshot the module root is
// ~/.jevons/build-snapshot, so `..` is ~/.jevons, where nothing of the sort
// lives. Every daemon rebuild through the snapshot failed with
// `replacement directory ../claudia does not exist` — on 2026-08-15 it was
// unblocked by hand with a symlink at ~/.jevons/claudia, which is host state:
// a fresh machine, a moved checkout or a `rm` reintroduces the outage. A
// guarantee that depends on somebody having hand-patched the host is not a
// guarantee (the 🎯T434 pattern, one layer down).
//
// CHOSEN: rewrite the snapshot's own go.mod so each relative filesystem
// replacement carries the absolute path the CLONE's root resolves it to, and
// put the original bytes back when the build is done. The rewrite says exactly
// what the clone said — no new policy about where dependencies live — and
// restoring keeps the worktree clean, which is what lets prepareSnapshot reuse
// it (a permanently dirty snapshot is recreated on every restart and every
// build starts cold).
//
// REJECTED: materialising the replacement inside the snapshot (a symlink at
// ~/.jevons/claudia) is the hand-patch written in Go — it writes outside the
// snapshot into a directory buildsnap does not own, and multiplies with every
// future replace. Also rejected: `-modfile` through GOFLAGS, which leaves the
// module root's go.mod untouched but silently depends on every `go` invocation
// in the Makefile inheriting an environment variable the Makefile is free to
// overwrite.
//
// ACCEPTED FAILURE, stated. An absolute replacement points at the sibling
// clone's WORKING TREE, so the daemon is built from committed HEAD of this
// repo and from whatever is currently checked out next door. That is exactly
// the behaviour the hand-made symlink had, so this is not a regression — but
// it is not 🎯T254.2's guarantee either, and extending the snapshot across
// sibling modules is a bigger change than this defect warrants.

// resolveLocalReplaces rewrites relative filesystem replacements in
// snapDir/go.mod to absolute paths resolved against repoRoot, and returns a
// function that puts the original bytes back.
//
// The returned restore is always safe to call, including after an error and
// when nothing was rewritten. A module with no local replacements is left
// byte-identical and never written at all.
func resolveLocalReplaces(cfg Config, snapDir, repoRoot string) (restore func(), err error) {
	noop := func() {}

	path := filepath.Join(snapDir, "go.mod")
	original, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return noop, nil // not a Go module; nothing to resolve
		}
		return noop, fmt.Errorf("read %s: %w", path, err)
	}

	rewritten, changes, err := rewriteLocalReplaces(string(original), repoRoot)
	if err != nil {
		return noop, err
	}
	if len(changes) == 0 {
		return noop, nil
	}

	for _, c := range changes {
		cfg.logf("🎯T473 snapshot replace: %s => %s resolved to %s", c.module, c.from, c.to)
	}
	if err := os.WriteFile(path, []byte(rewritten), 0o644); err != nil {
		return noop, fmt.Errorf("rewrite %s: %w", path, err)
	}
	return func() {
		if err := os.WriteFile(path, original, 0o644); err != nil {
			cfg.logf("WARNING: could not restore %s (%v); the snapshot will be recreated on the next build", path, err)
		}
	}, nil
}

// replaceChange records one rewritten directive, for the log.
type replaceChange struct {
	module, from, to string
}

// rewriteLocalReplaces is the pure half: it returns body with every relative
// filesystem replacement made absolute against repoRoot.
//
// Deliberately conservative. Only a directive whose replacement token starts
// with "./" or "../" (or is "." / "..") is touched — a version replacement
// (`=> other/mod v1.2.3`) and an already-absolute path are left byte-identical,
// as is every other line in the file including comments and formatting. The
// point is to change what one token means, not to reformat somebody's go.mod.
func rewriteLocalReplaces(body, repoRoot string) (string, []replaceChange, error) {
	lines := strings.Split(body, "\n")
	inBlock := false
	var changes []replaceChange

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case inBlock && trimmed == ")":
			inBlock = false
			continue
		case !inBlock && strings.HasPrefix(trimmed, "replace") && strings.HasSuffix(trimmed, "("):
			inBlock = true
			continue
		case !inBlock && !strings.HasPrefix(trimmed, "replace "):
			continue
		}

		arrow := strings.Index(line, "=>")
		if arrow < 0 {
			continue
		}
		head, tail := line[:arrow+2], line[arrow+2:]

		// The replacement token is the first field after the arrow; anything
		// after it is a version or a comment and is preserved verbatim.
		lead := len(tail) - len(strings.TrimLeft(tail, " \t"))
		rest := tail[lead:]
		end := strings.IndexAny(rest, " \t")
		if end < 0 {
			end = len(rest)
		}
		target := rest[:end]
		if !isLocalPath(target) {
			continue
		}

		abs, err := filepath.Abs(filepath.Join(repoRoot, filepath.FromSlash(target)))
		if err != nil {
			return "", nil, fmt.Errorf("resolve replacement %q against %s: %w", target, repoRoot, err)
		}
		module := replacedModule(trimmed)
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			return "", nil, fmt.Errorf(
				"go.mod replaces %s with %s, which resolves to %s and is not there. "+
					"The snapshot builds from a detached worktree, so a relative replacement "+
					"is resolved against the clone at %s rather than against the snapshot. "+
					"Check out %s at %s, or point the replace at where it actually lives — "+
					"do NOT symlink it next to the snapshot, which is host state a fresh "+
					"machine will not have (🎯T473)",
				module, target, abs, repoRoot, module, abs)
		}

		lines[i] = head + tail[:lead] + abs + rest[end:]
		changes = append(changes, replaceChange{module: module, from: target, to: abs})
	}
	return strings.Join(lines, "\n"), changes, nil
}

// isLocalPath reports whether a replacement token is a filesystem path
// relative to the module root, which is the only kind whose meaning moves with
// the module root. An absolute path already means the same thing everywhere.
func isLocalPath(s string) bool {
	return s == "." || s == ".." ||
		strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../")
}

// replacedModule pulls the replaced module path out of a directive, for error
// messages and the log. Best effort: the caller only reads it.
func replacedModule(trimmed string) string {
	fields := strings.Fields(strings.TrimPrefix(trimmed, "replace "))
	if len(fields) == 0 {
		return "a module"
	}
	return fields[0]
}
