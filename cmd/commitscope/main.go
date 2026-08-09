// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command commitscope is the pre-commit half of the shared-index guard
// (🎯T377). It reads which index git is committing from, asks
// internal/commitscope whether that index can contain paths this worker
// never named, and refuses the commit when it can.
//
// Exit status is the hook contract:
//
//	0  commit may proceed
//	1  the guard itself could not run (never blocks work silently)
//	2  refused — the commit would sweep the shared index
//
// git treats any non-zero as a refusal; the split exists so a broken guard
// is distinguishable from a working one at a glance.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/marcelocantos/jevons/internal/commitscope"
)

const (
	exitAllow  = 0
	exitBroken = 1
	exitRefuse = 2
)

func main() {
	staged, err := stagedPaths()
	if err != nil {
		// A guard that cannot read the index must say so rather than wave
		// the commit through: silence here is the failure mode the target
		// exists to remove.
		fmt.Fprintf(os.Stderr, "commitscope: cannot read the staged set: %v\n", err)
		os.Exit(exitBroken)
	}
	v := commitscope.Decide(&commitscope.Request{
		IndexFile: os.Getenv("GIT_INDEX_FILE"),
		Staged:    staged,
		Disabled:  commitscope.OffValue(os.Getenv(commitscope.DisableEnv)),
	})
	if !v.Refused {
		os.Exit(exitAllow)
	}
	fmt.Fprint(os.Stderr, v.Message)
	os.Exit(exitRefuse)
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
