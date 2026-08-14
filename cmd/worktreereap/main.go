// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command worktreereap sweeps a clone's verification worktrees by hand
// (🎯T440). The standing sweeper lives in jevonsd; this is for looking at what
// a sweep would do before letting it, and for clones no daemon watches.
//
//	go run ./cmd/worktreereap -dry-run
//	go run ./cmd/worktreereap
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/marcelocantos/jevons/internal/worktreereap"
)

func main() {
	repo := flag.String("repo", ".", "clone whose worktrees are swept")
	dryRun := flag.Bool("dry-run", false, "decide everything, remove nothing")
	sessionGrace := flag.Duration("session-grace", worktreereap.DefaultSessionGrace,
		"how long a worktree whose session has no live process must be idle before it is reaped")
	idleGrace := flag.Duration("idle-grace", worktreereap.DefaultIdleGrace,
		"the same waiting period for a worktree nothing identifies")
	flag.Parse()

	root, err := filepath.Abs(*repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "worktreereap: %v\n", err)
		os.Exit(2)
	}
	// git reports resolved paths, so resolve ours before comparing.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}

	protected := flag.Args()
	if home, err := os.UserHomeDir(); err == nil {
		// The daemon's build snapshot, which is a permanent worktree and is
		// never anybody's to reap.
		protected = append(protected, filepath.Join(home, ".jevons", "build-snapshot"))
	}

	res, err := worktreereap.Sweep(worktreereap.Config{
		Policy: worktreereap.Policy{
			RepoRoot:     root,
			Protected:    protected,
			SessionGrace: *sessionGrace,
			IdleGrace:    *idleGrace,
		},
		DryRun: *dryRun,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "worktreereap: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(res.Report())

	// A failed removal is the one outcome a caller must be able to notice
	// without reading the prose, since it means the entry is still there.
	if res.Failed > 0 {
		os.Exit(1)
	}
}
