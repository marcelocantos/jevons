// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command ledgercommit lands a scoped bullseye.yaml commit after a mutation
// (🎯T1147). Bullseye MCP and fleet hooks can invoke it post-write.
//
//	go run ./cmd/ledgercommit -repo . -m "achieve T1147"
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/marcelocantos/jevons/internal/ledgercommit"
)

func main() {
	repo := flag.String("repo", ".", "git repo root")
	msg := flag.String("m", "", "commit message")
	flag.Parse()
	if err := ledgercommit.CommitDirtyLedger(*repo, *msg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
