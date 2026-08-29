// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command buildident prints the source identity of a daemon build — this
// repo's HEAD and dirty state plus every go.work sibling's (🎯T580) — so
// the restart path can tell "already activated" from "stale binary" when
// the change landed in claudia rather than here.
//
// With -id it prints the bare hex identity and nothing else, which is what
// restart-daily-jevonsd.sh records and compares.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/marcelocantos/jevons/internal/buildident"
)

func main() {
	root := flag.String("root", ".", "repo root")
	idOnly := flag.Bool("id", false, "print only the bare hex identity")
	flag.Parse()

	r, err := buildident.Compute(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "buildident: %v\n", err)
		os.Exit(1)
	}
	if *idOnly {
		fmt.Println(r.Identity)
		return
	}
	fmt.Print(buildident.FormatHuman(r))
}
