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
func LedgerKey(workdir string) string {
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
	if ledger, err := DiscoverLedgerPath(abs); err == nil {
		return ledger
	}
	for d := abs; ; {
		if st, err := os.Stat(filepath.Join(d, ".git")); err == nil && (st.IsDir() || st.Mode().IsRegular()) {
			return filepath.Join(d, "bullseye.yaml")
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return abs
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
