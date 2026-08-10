// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command gate runs a gate command so that its exit status survives being
// reported (🎯T386), and records that status where it can be read back even
// if the harness relays it wrongly (🎯T396).
//
//	bin/gate -- make test-go        # run a gate, print its GATE attestation
//	bin/gate -name web -- make test-web
//	bin/gate last                   # the most recent run's attestation
//	bin/gate show <id>              # one run, with the reason it is not green
//	bin/gate check < report.md      # flag a finish report's false green
//
// gate exits with the command's own status, so it drops into a Makefile
// recipe or a `&&` chain unchanged. A run that exits zero while printing a
// timeout panic exits 3 (SUSPECT) rather than passing the lie along;
// -allow-suspect downgrades that to the command's own status for the cases
// where the output legitimately quotes a failure.
//
// There is no shell between gate and the command. That is the point: a shell
// is what introduces the pipeline whose status is not the gate's.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/marcelocantos/jevons/internal/gate"
)

// Exit codes for gate's own outcomes. A gate that ran keeps the command's
// status, so these only apply when gate itself has something to say.
const (
	exitUsage   = 2
	exitSuspect = 3
	exitFlagged = 4
	exitError   = 70
)

func main() {
	var (
		name         string
		dir          string
		storeDir     string
		quiet        bool
		allowSuspect bool
	)
	flag.StringVar(&name, "name", "", "label for this gate in the attestation")
	flag.StringVar(&dir, "dir", "", "working directory for the command")
	flag.StringVar(&storeDir, "store", "", "record directory (default ~/.jevons/gates)")
	flag.BoolVar(&quiet, "quiet", false, "print only the attestation line, not the command's output")
	flag.BoolVar(&allowSuspect, "allow-suspect", false,
		"exit with the command's own status even when its output contradicts a pass")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(exitUsage)
	}

	switch args[0] {
	case "last":
		os.Exit(cmdLast(storeDir))
	case "show":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "gate show: need a record id")
			os.Exit(exitUsage)
		}
		os.Exit(cmdShow(storeDir, args[1]))
	case "check":
		os.Exit(cmdCheck(storeDir))
	}

	os.Exit(cmdRun(args, name, dir, storeDir, quiet, allowSuspect))
}

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  gate [flags] -- <command> [args...]   run a gate and attest its real status
  gate last                             show the most recent run
  gate show <id>                        show one run
  gate check                            read a finish report on stdin, flag false greens

flags:
`)
	flag.PrintDefaults()
}

func cmdRun(argv []string, name, dir, storeDir string, quiet, allowSuspect bool) int {
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
	rec, err := gate.Run(&gate.RunArgs{
		Command: argv,
		Dir:     dir,
		Name:    name,
		Stdout:  out,
		Stderr:  errOut,
		Store:   store,
	})
	if rec == nil {
		fmt.Fprintln(os.Stderr, "gate:", err)
		return exitError
	}
	if err != nil {
		// The command ran; only the record keeping failed. Say so loudly —
		// an unrecorded run cannot be cited — but still report the status.
		fmt.Fprintln(os.Stderr, "gate: record not saved:", err)
	}
	fmt.Fprintln(os.Stderr, rec.Summary())

	switch {
	case rec.Verdict == gate.VerdictSuspect && !allowSuspect:
		return exitSuspect
	case !rec.StatusKnown:
		return exitError
	default:
		return rec.ExitStatus
	}
}

func cmdLast(storeDir string) int {
	store, err := gate.OpenStore(storeDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gate:", err)
		return exitError
	}
	recs, err := store.Recent(1)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gate:", err)
		return exitError
	}
	if len(recs) == 0 {
		fmt.Fprintln(os.Stderr, "gate: no runs recorded in", store.Root)
		return exitError
	}
	fmt.Println(recs[0].Summary())
	if recs[0].Verdict.IsGreen() {
		return 0
	}
	return 1
}

func cmdShow(storeDir, id string) int {
	store, err := gate.OpenStore(storeDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gate:", err)
		return exitError
	}
	rec, ok, err := store.Load(id)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gate:", err)
		return exitError
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "gate: no record %s in %s\n", id, store.Root)
		return exitError
	}
	fmt.Println(rec.Summary())
	if rec.Verdict.IsGreen() {
		return 0
	}
	return 1
}

func cmdCheck(storeDir string) int {
	report, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gate check:", err)
		return exitError
	}
	store, err := gate.OpenStore(storeDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gate check:", err)
		return exitError
	}
	flags := gate.FlagFalseGreen(string(report), store.Lookup)
	if len(flags) == 0 {
		fmt.Println("no false-green flags")
		return 0
	}
	fmt.Println(strings.TrimSpace(gate.Banner(flags)))
	return exitFlagged
}
