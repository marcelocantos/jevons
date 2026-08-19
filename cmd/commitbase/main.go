// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command commitbase is the blessed private-index commit recipe (🎯T432).
//
// Use it when `git commit --only` cannot: a shared hot file still holds
// another worker's uncommitted hunks, so the work tree is not a safe stage
// source. Commitbase seeds a private index from HEAD, stages only the paths
// (and optional exact blobs) you name, re-checks HEAD before writing the
// commit, and refuses when HEAD has moved — because a tree built from a
// stale seed deletes whatever landed in between, and update-ref CAS alone
// does not catch that.
//
//	commitbase -m "msg" -- path1 path2
//	commitbase -m "msg" --blob Makefile=/tmp/makefile.mine -- path1
//
// Escape hatch (deliberate stale-base commit): JEVONS_COMMIT_BASE=off.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/marcelocantos/jevons/internal/commitbase"
)

const (
	exitOK     = 0
	exitUsage  = 1
	exitRefuse = 2
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("commitbase", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	msg := fs.String("m", "", "commit message (required)")
	msgLong := fs.String("message", "", "commit message (required)")
	var blobs blobFlags
	fs.Var(&blobs, "blob", "stage exact content at path (path=/file/with/content); repeatable")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	message := *msg
	if message == "" {
		message = *msgLong
	}
	if message == "" {
		fmt.Fprintln(os.Stderr, "commitbase: -m <message> is required")
		usage()
		return exitUsage
	}
	paths := fs.Args()
	if len(paths) == 0 && len(blobs) == 0 {
		fmt.Fprintln(os.Stderr, "commitbase: pass one or more paths and/or --blob path=file")
		usage()
		return exitUsage
	}

	blobMap := map[string][]byte{}
	for _, b := range blobs {
		content, err := os.ReadFile(b.source)
		if err != nil {
			fmt.Fprintf(os.Stderr, "commitbase: read blob source %s: %v\n", b.source, err)
			return exitUsage
		}
		blobMap[b.path] = content
	}

	res, err := commitbase.Commit(&commitbase.CommitArgs{
		Message:  message,
		Paths:    paths,
		Blobs:    blobMap,
		Disabled: commitbase.OffValue(os.Getenv(commitbase.DisableEnv)),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		if _, ok := err.(*commitbase.RefuseError); ok {
			return exitRefuse
		}
		return exitUsage
	}
	fmt.Printf("%s\n", res.CommitSHA)
	return exitOK
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: commitbase -m <msg> [--blob path=file]... [--] <paths>...")
	fmt.Fprintln(os.Stderr, "  Stages only the named paths (work tree) and blobs (exact content),")
	fmt.Fprintln(os.Stderr, "  re-checks HEAD against the seed, refuses if it moved (🎯T432).")
}

// blobFlags parses repeated --blob path=file flags.
type blobFlags []blobFlag

type blobFlag struct {
	path   string
	source string
}

func (b *blobFlags) String() string {
	var parts []string
	for _, x := range *b {
		parts = append(parts, x.path+"="+x.source)
	}
	return strings.Join(parts, ",")
}

func (b *blobFlags) Set(v string) error {
	path, source, ok := strings.Cut(v, "=")
	if !ok || path == "" || source == "" {
		return fmt.Errorf("--blob wants path=file, got %q", v)
	}
	*b = append(*b, blobFlag{path: path, source: source})
	return nil
}
