// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command integrate is deliberately closed until daemon-mediated PO grants
// are wired. The old positional worker path must not land even a single
// commit without current-process provenance and hold-epoch redemption.
// This binary is not an authorization service: CLI flags are self-assertions.
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
		fmt.Fprintf(flag.CommandLine.Output(), "usage: integrate [-repo DIR] AGENT... (currently refuses: grant transport unavailable)\n")
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
