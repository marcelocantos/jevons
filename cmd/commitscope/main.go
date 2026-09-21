// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command commitscope is the pre-commit half of the shared-index guard
// (🎯T377) and the shared-ledger content-scope warning (🎯T748). It reads
// which index git is committing from, asks internal/commitscope whether
// that index can contain paths this worker never named, and refuses the
// commit when it can. A path-scoped commit of bullseye.yaml is allowed
// (the PO must close targets) but is told when that file's current diff
// contains target rows the actor did not write.
//
// Exit status is the hook contract:
//
//	0  commit may proceed
//	1  the guard itself could not run (never blocks work silently)
//	2  refused — the commit would sweep the shared index
//
// git treats any non-zero as a refusal; the split exists so a broken guard
// is distinguishable from a working one at a glance.
//
// Run as `commitscope --install` it instead installs that hook into the
// current clone, which is how `make` makes the guard present in a fresh
// checkout rather than in whichever clones someone remembered to set up.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/commitattrib"
	"github.com/marcelocantos/jevons/internal/commitscope"
)

const (
	exitAllow  = 0
	exitBroken = 1
	exitRefuse = 2
)

// installFlag is the only argument this command takes. git passes none to a
// pre-commit hook, so there is nothing for it to collide with.
const (
	installFlag       = "--install"
	stampTrailersFlag = "--stamp-trailers"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == installFlag {
		install()
		return
	}
	if len(os.Args) > 1 && os.Args[1] == stampTrailersFlag {
		if len(os.Args) != 3 {
			fmt.Fprintf(os.Stderr, "commitscope: usage: %s %s <commit-msg-file>\n", os.Args[0], stampTrailersFlag)
			os.Exit(exitBroken)
		}
		if err := stampTrailers(os.Args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "commitscope: stamp trailers: %v\n", err)
			os.Exit(exitBroken)
		}
		os.Exit(exitAllow)
	}
	staged, err := stagedPaths()
	if err != nil {
		// A guard that cannot read the index must say so rather than wave
		// the commit through: silence here is the failure mode the target
		// exists to remove.
		fmt.Fprintf(os.Stderr, "commitscope: cannot read the staged set: %v\n", err)
		os.Exit(exitBroken)
	}
	contents, err := stagedLedgerContents(staged)
	if err != nil {
		fmt.Fprintf(os.Stderr, "commitscope: cannot read staged ledger content: %v\n", err)
		os.Exit(exitBroken)
	}
	v := commitscope.Decide(&commitscope.Request{
		IndexFile: os.Getenv("GIT_INDEX_FILE"),
		Staged:    staged,
		Disabled:  commitscope.OffValue(os.Getenv(commitscope.DisableEnv)),
		Contents:  contents,
		Claimed:   commitscope.ParseClaimed(os.Getenv(commitscope.ClaimEnv)),
	})
	if v.Message != "" {
		fmt.Fprint(os.Stderr, v.Message)
	}
	if v.Refused {
		os.Exit(exitRefuse)
	}
	os.Exit(exitAllow)
}

// install puts the hook where git will exec it. The hooks directory comes
// from `git rev-parse --git-path hooks`, which resolves core.hooksPath — a
// redirected hooks path would otherwise make an install into .git/hooks look
// successful while git ran something else entirely.
//
// A clone whose pre-commit hook belongs to someone else is reported and left
// alone; that is a fact about the clone, not a build failure, so `make` still
// succeeds. Only a guard that could not be installed at all exits non-zero.
func install() {
	hooksDir, err := one("git", "rev-parse", "--git-path", "hooks")
	if err == nil {
		var root string
		if root, err = one("git", "rev-parse", "--show-toplevel"); err == nil {
			var broken bool
			for _, hook := range []string{"pre-commit", "prepare-commit-msg"} {
				var outcome commitscope.InstallOutcome
				source := filepath.Join(root, "scripts", "hooks", hook)
				if outcome, err = commitscope.InstallHook(hooksDir, source, hook); err != nil {
					fmt.Fprintf(os.Stderr, "commitscope: cannot install %s: %v\n", hook, err)
					broken = true
					continue
				}
				fmt.Fprint(os.Stderr, commitscope.InstallReport(outcome, hooksDir, hook))
			}
			if broken {
				os.Exit(exitBroken)
			}
			os.Exit(exitAllow)
		}
	}
	fmt.Fprintf(os.Stderr, "commitscope: cannot install the shared-index guard: %v\n", err)
	os.Exit(exitBroken)
}

func stampTrailers(msgPath string) error {
	return commitattrib.StampFile(msgPath)
}

// one runs a command expected to print a single line.
func one(name string, args ...string) (string, error) {
	lines, err := run(name, args...)
	if err != nil {
		return "", err
	}
	if len(lines) != 1 {
		return "", fmt.Errorf("%s %s: expected one line, got %d", name, strings.Join(args, " "), len(lines))
	}
	return strings.TrimSpace(lines[0]), nil
}

// stagedPaths lists what the commit would contain, read through the index
// git is actually committing (GIT_INDEX_FILE is inherited by these calls,
// so `--only` reports only the named paths).
func stagedPaths() ([]string, error) {
	// Before the first commit there is no HEAD to diff against, and every
	// tracked path is "staged".
	if err := exec.Command("git", "rev-parse", "--verify", "--quiet", "HEAD").Run(); err != nil {
		return run("git", "ls-files", "--cached", "-z")
	}
	return run("git", "diff", "--cached", "--name-only", "-z")
}

// stagedLedgerContents reads HEAD vs index blobs for each staged shared
// ledger path. `git show :path` inherits GIT_INDEX_FILE, so `--only`
// sees only the named file's staged bytes.
func stagedLedgerContents(staged []string) ([]commitscope.FileContent, error) {
	var out []commitscope.FileContent
	for _, p := range staged {
		if !commitscope.IsSharedLedger(p) {
			continue
		}
		stagedBytes, err := gitShow(":" + p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		headBytes, _ := gitShow("HEAD:" + p) // missing HEAD path = new file
		out = append(out, commitscope.FileContent{Path: p, Head: headBytes, Staged: stagedBytes})
	}
	return out, nil
}

func gitShow(revPath string) ([]byte, error) {
	out, err := exec.Command("git", "show", revPath).Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

func run(name string, args ...string) ([]string, error) {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	var paths []string
	for p := range strings.SplitSeq(string(out), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}
