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
//
// 🎯T1107: a worker branch does not land unless the gate store
// (~/.jevons/gates, 🎯T386/T396) holds a GREEN record naming its tip — or a
// tree-equal commit. -override-gate-check REASON is the owner's named escape
// hatch; every use is logged in the shared clone's git dir rather than
// vanishing when the process exits.
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
	gateDir := flag.String("gate-dir", "",
		"gate store to check for a GREEN record (default: ~/.jevons/gates, or $JEVONS_GATE_DIR)")
	overrideReason := flag.String("override-gate-check", "",
		"owner-only: land without a GREEN gate record, for the given reason (logged, 🎯T1107)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: integrate [-repo DIR] [-attempts N] [-gate-dir DIR] [-override-gate-check REASON] AGENT...\n")
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
		rec, err := worktree.RequireLandingGate(base, agent, *gateDir, *overrideReason)
		if err != nil {
			fmt.Fprintf(os.Stderr, "integrate: %s: refused: %v\n", agent, err)
			os.Exit(1)
		}
		switch {
		case rec != nil:
			fmt.Printf("%s: gate %s\n", agent, rec.Attestation())
		case *overrideReason != "":
			fmt.Printf("%s: gate check overridden: %s (logged)\n", agent, *overrideReason)
		}

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
