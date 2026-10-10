// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command integrate lands isolated workers' branches on the branch the shared
// clone has checked out (🎯T254.2). It is the integrator's one landing path:
// workers commit in their own worktrees, and this is how those commits reach
// local master without anyone merging inside the shared tree by hand.
//
//	go run ./cmd/integrate -repo ~/work/github.com/marcelocantos/jevons jv-t901-commitbase-index
//	go run ./cmd/integrate -repo . jv-a jv-b
//
// Each worker is landed in the order given; the first refusal stops the run,
// so a conflict never leaves a half-landed batch hidden behind later output.
// Nothing is pushed (🎯T104).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/marcelocantos/jevons/internal/worktree"
)

func main() {
	repo := flag.String("repo", ".", "the shared clone whose checked-out branch receives the landings")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: integrate [-repo DIR] [-attempts N] AGENT...\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	base, err := filepath.Abs(*repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integrate: %v\n", err)
		os.Exit(2)
	}

	// No local file, CLI actor or --approve flag is an authority. The daemon
	// redemption transport is not installed yet: refuse rather than silently
	// treating a worker's own claim as PO approval.
	_, err = worktree.IntegrateBatch(base, "", "", flag.Args(), nil)
	fmt.Fprintf(os.Stderr, "integrate: %v\n", err)
	os.Exit(1)
}
