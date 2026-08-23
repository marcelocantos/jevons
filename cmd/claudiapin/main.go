// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command claudiapin reports whether go.mod's claudia pin contains the
// fleet-needed commits, and names sibling commits the pin is missing
// (🎯T448). Exit 2 on a hard pin gap (required commit absent); exit 0
// with a LOUD line when the sibling is merely ahead of a sufficient pin
// (daily path still builds via go.work / buildsnap inject).
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/marcelocantos/jevons/internal/claudiapin"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "claudiapin: %v\n", err)
		os.Exit(1)
	}
	r, err := claudiapin.Check(abs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "claudiapin: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(claudiapin.FormatHuman(r))
	if claudiapin.HardFail(r) {
		os.Exit(2)
	}
}
