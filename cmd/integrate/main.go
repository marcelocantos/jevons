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
	attempts := flag.Int("attempts", worktree.DefaultIntegrateAttempts,
		"how many times to recompute when the shared branch moves under a landing")
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

	for _, agent := range flag.Args() {
		res, err := worktree.Integrate(&worktree.IntegrateArgs{
			BaseWorkdir: base,
			AgentName:   agent,
			MaxAttempts: *attempts,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "integrate: %s: %v\n", agent, err)
			os.Exit(1)
		}
		switch {
		case len(res.Landed) == 0:
			fmt.Printf("%s: nothing to land; %s already has %s\n", agent, res.BaseBranch, res.WorkerBranch)
		case res.Merge != "":
			fmt.Printf("%s: landed %d commit(s) on %s by merge %s (%s..%s)\n",
				agent, len(res.Landed), res.BaseBranch, res.Merge, res.From, res.To)
		default:
			fmt.Printf("%s: landed %d commit(s) on %s by fast-forward (%s..%s)\n",
				agent, len(res.Landed), res.BaseBranch, res.From, res.To)
		}
	}
}
