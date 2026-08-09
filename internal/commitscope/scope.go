// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package commitscope stops one fleet worker's commit from silently
// swallowing another worker's staged hunks (🎯T377).
//
// Several fleet workers run concurrently in one clone, and a clone has
// exactly one index. `git add` is therefore a write to shared mutable
// state: whatever worker A has staged is sitting in the same index that
// worker B's `git commit` is about to turn into a tree. Worker B gets no
// warning, because from git's point of view a bare commit doing exactly
// that is the normal case.
//
// It has already cost this repo a permanent misattribution. Commit
// 29e69e8, whose message reads "refactor(web): T372 one pending-owner-turn
// contract", contains the entire 🎯T370 feature — fleet_cycle.js, its
// hermetic test, the Playwright smoke, the index.html wiring, the embed
// line and the Makefile registrations. No commit in this history names
// T370; `git log --diff-filter=A -- web/scripts/fleet_cycle.js` answers
// 29e69e8. Reverting T372 would silently revert T370 with it.
//
// The detection here is index provenance, and it needs no ledger of who
// owns which path. Git tells a pre-commit hook, in GIT_INDEX_FILE, which
// index the commit is being built from, and that one string already
// distinguishes a commit whose contents were chosen from one that merely
// inherited whatever the shared index happened to hold:
//
//	git commit                    -> $GIT_DIR/index          (shared: sweeps)
//	git commit -a / -i <paths>    -> $GIT_DIR/index.lock     (shared: sweeps)
//	git commit --only <paths>     -> $GIT_DIR/next-index-<pid>.lock (scoped)
//	GIT_INDEX_FILE=… git commit   -> that path               (private)
//
// The first two forms take their content from state other processes can
// write. The last two cannot contain a path the committer did not name.
// So the rule is: a commit that would contain staged paths, built from the
// shared index, is refused — and the refusal names what would have gone in.
//
// This file is pure: no filesystem, no environment, no clock. The binary
// (cmd/commitscope) supplies the inputs and owns the exit status.
package commitscope

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// IndexKind classifies the index a commit is being built from.
type IndexKind int

const (
	// SharedIndex is $GIT_DIR/index — a bare `git commit`. Its contents are
	// whatever every worker in the clone has staged.
	SharedIndex IndexKind = iota
	// SharedLock is $GIT_DIR/index.lock — `git commit -a` or `-i`. Git
	// commits through a lockfile that will replace the shared index, so
	// the shared index's foreign content is carried along.
	SharedLock
	// ScopedIndex is $GIT_DIR/next-index-<pid> — `git commit --only <paths>`
	// (equivalently `git commit <paths>`). Git built it from HEAD plus the
	// named pathspec, so no unnamed path can be in it.
	ScopedIndex
	// PrivateIndex is a GIT_INDEX_FILE the worker chose. No other process
	// writes it.
	PrivateIndex
)

func (k IndexKind) String() string {
	switch k {
	case SharedIndex:
		return "shared"
	case SharedLock:
		return "shared-lock"
	case ScopedIndex:
		return "scoped"
	case PrivateIndex:
		return "private"
	}
	return "unknown"
}

// Sweeps reports whether a commit built from this kind of index takes its
// content from state other workers can write.
func (k IndexKind) Sweeps() bool { return k == SharedIndex || k == SharedLock }

// DisableEnv turns the guard off for one command. The owner committing by
// hand in a quiet tree is a single actor and does not need it; `--no-verify`
// works too. Named rather than silent so that a disabled guard is a
// deliberate act that shows up in the command line.
const DisableEnv = "JEVONS_COMMIT_SCOPE"

// nextIndexPrefix is how git names the temporary index it builds for a
// pathspec-scoped commit (builtin/commit.c writes "next-index-%d").
const nextIndexPrefix = "next-index-"

// Classify maps the GIT_INDEX_FILE a pre-commit hook was given to the kind
// of index it names. An empty value means git did not override the default,
// which is the shared index.
//
// Classification is by basename, so it holds whether git passes the path
// relative to the work tree (as it does for the shared index) or absolute
// (as it does for the temporaries). The consequence is that a worker's
// private index must not be named exactly "index"; a private index that is
// gets treated as shared, which errs toward refusing.
func Classify(indexFile string) IndexKind {
	if strings.TrimSpace(indexFile) == "" {
		return SharedIndex
	}
	base := path.Base(filepath.ToSlash(indexFile))
	switch {
	case base == "index":
		return SharedIndex
	case base == "index.lock":
		return SharedLock
	case strings.HasPrefix(base, nextIndexPrefix):
		return ScopedIndex
	default:
		return PrivateIndex
	}
}

// Request is everything the decision needs. The hook reads GIT_INDEX_FILE
// from its environment and the staged set from the index that variable names.
type Request struct {
	// IndexFile is GIT_INDEX_FILE as the hook saw it; empty when unset.
	IndexFile string
	// Staged is the set of paths the commit would contain, read from the
	// effective index — not from the shared one.
	Staged []string
	// Disabled is DisableEnv set to an off value.
	Disabled bool
}

// Verdict is the decision plus the text the worker sees.
type Verdict struct {
	Refused bool
	Kind    IndexKind
	// Message is owner-facing: what would have been committed and how to
	// commit only one's own work instead. Empty when nothing needed saying.
	Message string
}

// MaxNamed caps how many staged paths a refusal lists. A refusal is a
// diagnostic, not a diff; enough paths to recognise whose work is in the
// way is enough.
const MaxNamed = 20

// Decide applies the rule. A commit is refused when it would carry staged
// paths out of an index other workers can write into.
//
// Two cases pass that might look like they should not. An empty staged set
// cannot misattribute anything (git will reject the commit itself, or it is
// an `--amend` of the message alone). And a disabled guard passes by
// construction — the point of the escape hatch is that it is explicit.
func Decide(req *Request) Verdict {
	kind := Classify(req.IndexFile)
	switch {
	case req.Disabled:
		return Verdict{Kind: kind}
	case len(req.Staged) == 0:
		return Verdict{Kind: kind}
	case !kind.Sweeps():
		return Verdict{Kind: kind}
	}
	return Verdict{Refused: true, Kind: kind, Message: refusal(kind, req.Staged)}
}

// howBuilt names the command that produced each sweeping index, so the
// message tells the worker what it actually typed.
func howBuilt(kind IndexKind) string {
	if kind == SharedLock {
		return "`git commit -a` / `git commit -i`"
	}
	return "a bare `git commit`"
}

func refusal(kind IndexKind, staged []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "commitscope: refusing %s — it commits the SHARED index (🎯T377).\n\n", howBuilt(kind))
	fmt.Fprintf(&b, "Other fleet workers stage into this same index. This commit would contain %s:\n", plural(len(staged), "path"))
	for i, p := range staged {
		if i == MaxNamed {
			fmt.Fprintf(&b, "  … and %d more\n", len(staged)-MaxNamed)
			break
		}
		fmt.Fprintf(&b, "  %s\n", p)
	}
	b.WriteString("\nIf any of those are not yours, committing sweeps another worker's work into\n")
	b.WriteString("a commit whose message describes yours. That is how 29e69e8 came to contain\n")
	b.WriteString("all of 🎯T370 under a message about 🎯T372.\n\n")
	b.WriteString("Commit your own paths explicitly instead:\n")
	b.WriteString("  git commit --only <your paths> -m \"…\"\n")
	b.WriteString("then confirm with:\n")
	b.WriteString("  git show --stat HEAD\n\n")
	fmt.Fprintf(&b, "Deliberate whole-index commit (single-actor tree): %s=off git commit …\n", DisableEnv)
	return b.String()
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// OffValue reports whether a DisableEnv value turns the guard off. Anything
// unset or empty leaves it on, so the guard cannot be disabled by accident.
func OffValue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off", "0", "false", "no", "disable", "disabled":
		return true
	}
	return false
}
