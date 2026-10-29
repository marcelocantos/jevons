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
// tree-equal commit.
// 🎯T1146: that record must be from a pristine checkout (bin/gate -clean,
// GOWORK=off); a workspace build does not count. Tips whose go.mod commits a
// replace of a github.com/marcelocantos/… module are refused (🎯T448). When
// the landing creates a merge commit, integrate also runs go build ./... and
// go test -count=1 on every package the branch touched (🎯T1191), plus
// ./scripts/docratchet/ on every merge (🎯T1197), on that merge result in a
// pristine checkout and rolls the branch back if either fails — per-tip
// green is not enough when trunk and branch both moved, and a ratchet that
// inspects the rest of the tree is not skipped just because the branch
// never edited it.
// 🎯T1249: if the merge commit contains .jevons-integrate-prereq.yaml,
// that prerequisite (optional git submodule update --init --recursive,
// then a repo-relative script + args) runs in the same pristine checkout
// before go build / tests. It is not a bypass: a failing prerequisite
// refuses the landing; a passing one still has to pass the gate.
// 🎯T1255: the same file may declare test_tags (e.g. [csdke2e]); the
// touched-package go test then runs with -tags <declared> so a package
// build-tagged out of the default build is actually tested. Not a bypass:
// real tests still run under those tags; undeclared tagged-out packages
// still fail as "matched no packages".
// -override-gate-check REASON is the owner's named escape
// hatch; every use is logged in the shared clone's git dir rather than
// vanishing when the process exits.
// -detach runs the same landing in its own session and returns at once;
// -result AGENT reports how the latest one ended (🎯T1259, detach.go).
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/landhold"
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
	overrideLandHold := flag.String("override-land-hold", "",
		"owner-only: integrate despite an active no-merge hold (logged, 🎯T1040)")
	detach := flag.Bool("detach", false,
		"run the landing in its own session and return at once; poll with -result (🎯T1259)")
	result := flag.String("result", "",
		"print the latest detached landing's outcome for AGENT and exit with its status (🎯T1259)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: integrate [-repo DIR] [-attempts N] [-gate-dir DIR] [-override-gate-check REASON] [-detach] AGENT...\n       integrate -result AGENT\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *result != "" {
		os.Exit(printResult(*result))
	}
	if *detach {
		if flag.NArg() == 0 {
			flag.Usage()
			os.Exit(2)
		}
		os.Exit(startDetached(flag.Args()))
	}
	recordStart(flag.Args())
	code := run(*repo, *attempts, *gateDir, *overrideReason, *overrideLandHold)
	recordResult(flag.Args(), code)
	os.Exit(code)
}

func run(repo string, attempts int, gateDir, overrideReason, overrideLandHold string) int {
	landOverride := strings.TrimSpace(overrideLandHold)
	if landOverride == "" {
		landOverride = landhold.OverrideReason()
	}
	if ref, err := landhold.CheckSeat(nil, landhold.SeatFromEnv(), landOverride, landhold.ActionIntegrate); err != nil {
		fmt.Fprintf(os.Stderr, "integrate: %v\n", err)
		return 1
	} else if ref != nil {
		landhold.LogRefusal(ref, "cmd/integrate")
		fmt.Fprintf(os.Stderr, "integrate: %s\n", ref.Message())
		return 1
	}

	if flag.NArg() == 0 {
		flag.Usage()
		return 2
	}
	base, err := filepath.Abs(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integrate: %v\n", err)
		return 2
	}

	for _, agent := range flag.Args() {
		rec, err := worktree.RequireLandingGate(base, agent, gateDir, overrideReason)
		if err != nil {
			fmt.Fprintf(os.Stderr, "integrate: %s: refused: %v\n", agent, err)
			return 1
		}
		switch {
		case rec != nil:
			fmt.Printf("%s: gate %s\n", agent, rec.Attestation())
		case overrideReason != "":
			fmt.Printf("%s: gate check overridden: %s (logged)\n", agent, overrideReason)
		}

		res, err := worktree.Integrate(&worktree.IntegrateArgs{
			BaseWorkdir:   base,
			AgentName:     agent,
			MaxAttempts:   attempts,
			GateStoreRoot: gateDir,
			LandingGate:   rec,
		})
		if err != nil {
			var fault *worktree.TrunkFault
			if errors.As(err, &fault) {
				if encodeErr := json.NewEncoder(os.Stdout).Encode(fault); encodeErr != nil {
					fmt.Fprintf(os.Stderr, "integrate: encode outcome: %v\n", encodeErr)
				}
			}
			fmt.Fprintf(os.Stderr, "integrate: %s: %v\n", agent, err)
			return 1
		}
		switch {
		case len(res.Landed) == 0:
			fmt.Printf("%s: nothing to land; %s already has %s\n", agent, res.BaseBranch, res.WorkerBranch)
		case res.Merge != "":
			fmt.Printf("%s: landed %d commit(s) on %s by merge %s (%s..%s)\n",
				agent, len(res.Landed), res.BaseBranch, res.Merge, res.From, res.To)
			if res.PostMergeGate != nil {
				fmt.Printf("%s: post-merge gate %s\n", agent, res.PostMergeGate.Attestation())
			}
		default:
			fmt.Printf("%s: landed %d commit(s) on %s by fast-forward (%s..%s)\n",
				agent, len(res.Landed), res.BaseBranch, res.From, res.To)
		}
	}
	return 0
}
