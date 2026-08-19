// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package commitbase is the blessed private-index commit recipe (🎯T432).
//
// `git commit --only` rebuilds from current HEAD, so it cannot silently
// delete paths another worker landed while you were staging. A worker-owned
// GIT_INDEX_FILE seeded by `git read-tree HEAD` does not: the tree it writes
// is a snapshot of whatever HEAD was at seed time, and when that HEAD has
// moved by commit time the new commit's parent is usually the *new* HEAD
// while its tree is still the old one — which means every path the interloper
// added is deleted. `git update-ref <ref> <new> <old>` does not catch this,
// because it only guards the ref move; if the worker re-reads HEAD for the
// CAS and for the commit parent, both match the interloper and the stale
// tree still lands.
//
// The recipe here records the seed SHA, stages only the caller's paths
// (including blob content for a shared hot file whose worktree still holds
// another worker's uncommitted hunks), re-checks HEAD against that seed
// immediately before writing the commit, and refuses when they differ —
// naming the paths the stale tree would have overwritten. The escape hatch
// is the same shape as commitscope's: JEVONS_COMMIT_BASE=off.
package commitbase

import (
	"fmt"
	"strings"
)

// DisableEnv turns the HEAD-staleness check off for one command. A worker who
// genuinely intends to rewrite history from a chosen base must say so; the
// default is to refuse.
const DisableEnv = "JEVONS_COMMIT_BASE"

// MaxNamed caps how many at-risk paths a refusal lists. Enough to recognise
// whose work is in the way; not a full diff.
const MaxNamed = 20

// Check is the pure decision: may this private-index commit proceed?
type Check struct {
	// BaseSHA is the commit the private index was seeded from (read-tree).
	BaseSHA string
	// HeadSHA is HEAD at the moment before commit-tree / update-ref.
	HeadSHA string
	// LostPaths are paths present in HeadSHA's tree that a tree built from
	// BaseSHA would not carry forward — the interloper's additions and
	// modifications. Empty when BaseSHA == HeadSHA.
	LostPaths []string
	// Disabled is DisableEnv set to an off value.
	Disabled bool
}

// Verdict is the decision plus the text the worker sees.
type Verdict struct {
	Refused bool
	Message string
}

// Decide applies the rule. A commit is refused when HEAD has moved since the
// read-tree that seeded the private index, unless the guard is explicitly
// disabled. update-ref CAS against current HEAD is not a substitute: it
// permits exactly the tree-from-stale-base shape this target forbids.
func Decide(c Check) Verdict {
	if c.Disabled {
		return Verdict{}
	}
	base := strings.TrimSpace(c.BaseSHA)
	head := strings.TrimSpace(c.HeadSHA)
	if base == "" {
		return Verdict{Refused: true, Message: "commitbase: refusing — no seed SHA recorded (🎯T432).\n\n" +
			"Seed the private index with commitbase (or record HEAD at read-tree time)\n" +
			"before staging and committing. A tree with no known base cannot be checked\n" +
			"for staleness, and that is how e66e934 silently reverted 🎯T405.\n"}
	}
	if base == head {
		return Verdict{}
	}
	return Verdict{Refused: true, Message: refusal(base, head, c.LostPaths)}
}

func refusal(base, head string, lost []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "commitbase: refusing — HEAD moved since read-tree (🎯T432).\n\n")
	fmt.Fprintf(&b, "  seeded from: %s\n", short(base))
	fmt.Fprintf(&b, "  HEAD now:    %s\n\n", short(head))
	b.WriteString("A tree built from the older seed does not omit what landed in between —\n")
	b.WriteString("it DELETES it. That is how e66e934, whose message named only gate work,\n")
	b.WriteString("silently reverted cmd/detach, cmd/jevons-watchdog and internal/supervise.\n")
	b.WriteString("`git update-ref` CAS against current HEAD does not catch this: it guards\n")
	b.WriteString("the ref move, not the tree's base.\n\n")
	if len(lost) > 0 {
		fmt.Fprintf(&b, "Re-read-tree and re-apply, or the commit would overwrite %s:\n", plural(len(lost), "path"))
		for i, p := range lost {
			if i == MaxNamed {
				fmt.Fprintf(&b, "  … and %d more\n", len(lost)-MaxNamed)
				break
			}
			fmt.Fprintf(&b, "  %s\n", p)
		}
		b.WriteByte('\n')
	}
	b.WriteString("Recover:\n")
	b.WriteString("  re-seed the private index from current HEAD, re-stage your paths, commit again.\n\n")
	fmt.Fprintf(&b, "Deliberate stale-base commit (you mean to revert): %s=off …\n", DisableEnv)
	return b.String()
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// OffValue reports whether a DisableEnv value turns the guard off. Unset or
// empty leaves it on, so the guard cannot be disabled by accident.
func OffValue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off", "0", "false", "no", "disable", "disabled":
		return true
	}
	return false
}
