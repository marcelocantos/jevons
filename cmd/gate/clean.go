// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/marcelocantos/jevons/internal/gate"
)

// cmdRunClean runs a gate against a clean checkout of one commit (🎯T397).
//
//	bin/gate -clean -- make test-go          # verify HEAD, alone
//	bin/gate -clean -commit abc1234 -- ...   # verify some other commit
//
// It exits with the gate's own status exactly as the shared-tree form does, so
// it substitutes into a recipe or an `&&` chain unchanged — the difference is
// entirely in which tree the command was handed, and in the `tree=clean@<sha>`
// token the attestation then carries.
//
// A failure to establish the clean tree exits exitError and records nothing.
// There is no fallback to the shared tree: a run that quietly measured the
// wrong tree under this flag's name would be the defect wearing the fix's
// clothes, and its record would carry a clean-tree claim it never earned.
func cmdRunClean(argv []string, name, dir, commit, storeDir string, quiet, allowSuspect, keep bool) int {
	store, err := gate.OpenStore(storeDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gate:", err)
		return exitError
	}
	var out io.Writer = os.Stdout
	var errOut io.Writer = os.Stderr
	if quiet {
		out, errOut = nil, nil
	}
	res, err := gate.RunClean(&gate.CleanArgs{
		Command:  argv,
		Repo:     dir,
		Commit:   commit,
		Keep:     keep,
		Name:     name,
		Stdout:   out,
		Stderr:   errOut,
		Store:    store,
		Explicit: true,
	})
	if res == nil || res.Record == nil {
		fmt.Fprintln(os.Stderr, "gate -clean:", err)
		return exitError
	}
	if err != nil {
		// The command ran; only the record keeping failed. Say so loudly — an
		// unrecorded run cannot be cited — but still report the status.
		fmt.Fprintln(os.Stderr, "gate: record not saved:", err)
	}
	rec := res.Record
	fmt.Fprintln(os.Stderr, rec.Summary())
	if res.Kept {
		fmt.Fprintln(os.Stderr, "  worktree kept at", res.Worktree)
	}

	switch {
	case rec.Verdict == gate.VerdictSuspect && !allowSuspect:
		return exitSuspect
	case !rec.StatusKnown:
		return exitError
	default:
		return rec.ExitStatus
	}
}
