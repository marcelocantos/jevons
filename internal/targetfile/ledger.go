// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"os"
	"path/filepath"
	"strings"
)

// 🎯T389: a target id is not an identity. Ids are allocated per ledger and
// are small integers, so 🎯T19 names a different piece of work in every repo
// that has one — claudia, orthograph and jevons all had a live T19 the day
// this was written. Anything that asks "who is working on T19?" must ask it
// of one ledger, or it will answer with another repo's worker: refusing a
// legitimate spawn in one direction, reaping a live implementer in the other.
//
// The scope key is derived, not declared. An agent's workdir already says
// which repo it works in, so the ledger that owns its target ids is the one
// that workdir resolves to — no new owner ceremony, and every agent already
// carries a workdir (agent_start refuses without one).

// LedgerKey returns a stable identity for the ledger that owns target ids
// used in workdir. Empty workdir → empty key.
//
// Resolution, most to least specific: the nearest bullseye.yaml walking up
// from workdir; else the ledger the nearest git repository would hold (its
// root + bullseye.yaml), so a repo whose ledger is not committed yet still
// scopes as itself; else the workdir path. The fallbacks never merge two
// repositories — that is the property that matters, since the failure being
// fixed is cross-repo bleed rather than intra-repo precision.
//
// 🎯T1105: the identity is the repository's, not the checkout's. A linked
// git worktree (`git worktree add`, the 🎯T254.2 worker isolation) carries
// its own copy of the tracked ledger file, and keying on that copy's path
// made every worktree-isolated worker invisible to engagement — the frontier
// sweep spawned jv-t1104-auto beside a live T1104 worker. A ledger inside a
// git working tree is therefore keyed as the same tracked file in the
// repository's main working tree: every worktree of one repo yields the main
// clone's key, and the main clone's key is unchanged.
func LedgerKey(workdir string) string {
	return ledgerKeyWith(workdir, gitMainRoot)
}

// mainRootResolver maps a working-tree top (the directory holding `.git`) to
// the main working tree of the repository it belongs to. ok=false means the
// top is not a linked worktree (or cannot be read as one) and is its own root.
type mainRootResolver func(top string) (root string, ok bool)

func ledgerKeyWith(workdir string, mainRoot mainRootResolver) string {
	dir := strings.TrimSpace(workdir)
	if dir == "" {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	// Resolve symlinks so /tmp and /private/tmp (macOS) are one scope; a
	// path that does not exist yet keeps its literal form.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	top := gitTop(abs)
	ledger, err := DiscoverLedgerPath(abs)
	if err != nil {
		if top == "" {
			return abs
		}
		ledger = filepath.Join(top, "bullseye.yaml")
	}
	return repoLedgerKey(ledger, top, mainRoot)
}

// repoLedgerKey re-roots ledger (a path inside the working tree top) onto the
// repository's main working tree. A ledger outside top, or a top that is not
// a linked worktree, keys as itself — so distinct repositories never merge.
func repoLedgerKey(ledger, top string, mainRoot mainRootResolver) string {
	if top == "" || mainRoot == nil {
		return ledger
	}
	rel, err := filepath.Rel(top, ledger)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ledger
	}
	root, ok := mainRoot(top)
	if !ok || root == "" {
		return ledger
	}
	return filepath.Join(root, rel)
}

// gitTop is the nearest ancestor of abs (inclusive) holding a `.git` entry,
// or "" outside any git working tree.
func gitTop(abs string) string {
	for d := abs; ; {
		if st, err := os.Stat(filepath.Join(d, ".git")); err == nil && (st.IsDir() || st.Mode().IsRegular()) {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// gitMainRoot is the default mainRootResolver: the answer
// `git rev-parse --git-common-dir` gives, read from the files git writes
// rather than by running git, because LedgerKey runs per registry row on
// every sweep. A linked worktree's `.git` is a file
// "gitdir: <common>/worktrees/<name>", and that admin dir's `commondir` file
// names the shared git dir. The main working tree is that dir's parent when
// it is named `.git`; a bare repository's worktrees key on the bare dir
// itself. A `.git` file with no `commondir` behind it (a submodule) is not a
// linked worktree and keys as itself.
func gitMainRoot(top string) (string, bool) {
	dotgit := filepath.Join(top, ".git")
	st, err := os.Stat(dotgit)
	if err != nil || st.IsDir() {
		return "", false
	}
	raw, err := os.ReadFile(dotgit)
	if err != nil {
		return "", false
	}
	line, _, _ := strings.Cut(string(raw), "\n")
	gitdir, found := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
	if !found {
		return "", false
	}
	gitdir = strings.TrimSpace(gitdir)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(top, gitdir)
	}
	cd, err := os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return "", false
	}
	common := strings.TrimSpace(string(cd))
	if common == "" {
		return "", false
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitdir, common)
	}
	common = filepath.Clean(common)
	if real, err := filepath.EvalSymlinks(common); err == nil {
		common = real
	}
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common), true
	}
	return common, true
}

// SameLedger reports whether two ledger keys name the same ledger.
//
// An empty key is unknown rather than distinct, and unknown matches: an
// agent row with no workdir at all keeps the pre-🎯T389 behaviour of being
// visible to every scope. That case cannot arise from agent_start, which
// requires a workdir; treating it as a match keeps a degenerate row reapable
// instead of stranding it as a permanent zombie no achieve can clear.
func SameLedger(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return true
	}
	return a == b
}

// SameLedgerAsWorkdirs is SameLedger over two workdirs.
func SameLedgerAsWorkdirs(a, b string) bool {
	return SameLedger(LedgerKey(a), LedgerKey(b))
}
