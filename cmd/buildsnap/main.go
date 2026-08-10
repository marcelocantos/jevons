// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// buildsnap builds a make target from the repository's committed HEAD in a
// throwaway worktree and installs the artifact into the shared clone (🎯T254.2).
//
//	buildsnap [-root DIR] [-snap DIR] [-target T] [-artifact P] [-dest P]
//
// It exists so that one fleet worker's uncommitted edits cannot stop another
// worker from rebuilding and activating a landed change. See package
// internal/buildsnap for the incident and the reasoning.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/marcelocantos/jevons/internal/buildsnap"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("buildsnap: ")

	root := flag.String("root", ".", "shared clone to read HEAD from")
	snap := flag.String("snap", "", "snapshot worktree directory (default $HOME/.jevons/build-snapshot)")
	target := flag.String("target", "bin/jevonsd", "make target to build in the snapshot")
	artifact := flag.String("artifact", "", "built file relative to the snapshot root (default: same as -target)")
	dest := flag.String("dest", "", "where to install the artifact (default: <root>/<artifact>)")
	flag.Parse()

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		log.Fatalf("resolve -root: %v", err)
	}
	if *artifact == "" {
		*artifact = *target
	}
	if *dest == "" {
		*dest = filepath.Join(absRoot, filepath.FromSlash(*artifact))
	}
	if *snap == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("resolve home for default -snap: %v", err)
		}
		*snap = filepath.Join(home, ".jevons", "build-snapshot")
	}

	res, err := buildsnap.Run(buildsnap.Config{
		RepoRoot: absRoot,
		SnapDir:  *snap,
		Target:   *target,
		Artifact: *artifact,
		Dest:     *dest,
		Log:      func(s string) { fmt.Fprintln(os.Stderr, "buildsnap: "+s) },
	})
	if err != nil {
		// Deliberately fatal. A caller that carried on would activate a stale
		// binary while reporting success — the exact thing 🎯T194 forbids.
		log.Fatalf("%v", err)
	}
	fmt.Println(res.HEAD)
}
