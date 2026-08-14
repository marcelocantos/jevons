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
//	bin/gate sweep [-void]          # records that attest nothing (🎯T441)
//	bin/gate void <id> <reason>     # take one record out of the citable store
//
// gate exits with the command's own status, so it drops into a Makefile
// recipe or a `&&` chain unchanged. A run that exits zero while printing a
// timeout panic exits 3 (SUSPECT) rather than passing the lie along;
// -allow-suspect downgrades that to the command's own status for the cases
// where the output legitimately quotes a failure.
//
// There is no shell between gate and the command. That is the point: a shell
// is what introduces the pipeline whose status is not the gate's.
//
// `--` is required to run anything (🎯T441). It used to be optional: a first
// argument that was not a known subcommand was simply executed, so `gate ls`
// ran ls and filed `GATE ls exit=0 GREEN`, and `gate list` found an unrelated
// program of that name on PATH and filed a RED. Under 🎯T386 those records are
// citable evidence, and `gate last` hands back whichever is newest — so a typo
// could mint a green that attested nothing, in the one tool built to stop
// exactly that. The separator is the only thing that can tell "run this" from
// "I meant a subcommand", so it is no longer optional.
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

	// The separator decides which of gate's two jobs this is, and nothing else
	// does. With it, args is a command whatever it looks like — `gate -- last`
	// runs a program called last, deliberately. Without it, args[0] has to name
	// a subcommand, because the alternative is the 🎯T441 defect: running the
	// user's typo and filing the result as evidence.
	if !separated(os.Args, args) {
		os.Exit(cmdSubcommand(args, storeDir))
	}

	os.Exit(cmdRun(args, name, dir, storeDir, quiet, allowSuspect))
}

// subcommands is the allowlist. A word outside it is a typo, not a gate.
var subcommands = []string{"last", "show", "check", "sweep", "void", "help"}

// separated reports whether the caller used `--`. flag.Parse stops at the
// separator and consumes it, so the arguments it left are the tail of os.Args
// and the separator, if there was one, is the element immediately before them.
func separated(osArgs, rest []string) bool {
	i := len(osArgs) - len(rest) - 1
	return i >= 1 && osArgs[i] == "--"
}

// cmdSubcommand dispatches a bare first argument, or refuses it. Refusing is
// the point: it exits non-zero, says which word it did not recognise, and
// writes nothing to the store.
func cmdSubcommand(args []string, storeDir string) int {
	switch args[0] {
	case "last":
		return cmdLast(storeDir)
	case "show":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "gate show: need a record id")
			return exitUsage
		}
		return cmdShow(storeDir, args[1])
	case "check":
		return cmdCheck(storeDir)
	case "sweep":
		return cmdSweep(args[1:], storeDir)
	case "void":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "gate void: need a record id and a reason")
			return exitUsage
		}
		return cmdVoid(storeDir, args[1], strings.Join(args[2:], " "))
	case "help":
		usage()
		return 0
	}
	fmt.Fprintf(os.Stderr,
		"gate: %q is not a gate subcommand (known: %s).\n"+
			"To run it as a gate, say so: gate -- %s\n",
		args[0], strings.Join(subcommands, ", "), strings.Join(args, " "))
	return exitUsage
}

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  gate [flags] -- <command> [args...]   run a gate and attest its real status
  gate last                             show the most recent run
  gate show <id>                        show one run
  gate check                            read a finish report on stdin, flag false greens
  gate sweep [-void]                    list records that attest nothing; -void quarantines them
  gate void <id> <reason>               take one record out of the citable store

The -- is required to run a command: without it a mistyped subcommand would be
executed and recorded as though it were a gate (🎯T441).

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
		// Reaching here means the caller wrote `--`, so the record can say
		// that it was a deliberate run rather than a word gate decided to
		// execute — which is how a later sweep tells the two apart (🎯T441).
		Explicit: true,
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
	// Voided records answer here too, and that is the difference between
	// quarantine and deletion: `gate show <id>` on a swept record says what it
	// was and why it attests nothing, rather than "no such record" — which
	// would be indistinguishable from evidence having been tidied away.
	rec, ok := store.Lookup(id)
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

// cmdSweep reports the records in the store that attest nothing, and with
// -void moves them out of it. Dry by default: this is the one command that
// takes evidence away, so it shows its work first.
func cmdSweep(args []string, storeDir string) int {
	fs := flag.NewFlagSet("sweep", flag.ContinueOnError)
	doVoid := fs.Bool("void", false, "move the listed records out of the citable store")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	store, err := gate.OpenStore(storeDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gate sweep:", err)
		return exitError
	}
	results, err := store.Sweep(*doVoid, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gate sweep:", err)
		return exitError
	}
	if len(results) == 0 {
		fmt.Println("no records that attest nothing")
		return 0
	}
	failed := false
	for _, r := range results {
		fmt.Println(r.Record.Attestation())
		fmt.Println("  " + r.Reason)
		switch {
		case r.Err != nil:
			fmt.Fprintln(os.Stderr, "  NOT voided:", r.Err)
			failed = true
		case r.Voided:
			fmt.Println("  voided → " + r.Record.OutputPath)
		}
	}
	if failed {
		return exitError
	}
	if !*doVoid {
		fmt.Printf("\n%d record(s) would be voided; re-run with -void to do it.\n", len(results))
		return exitFlagged
	}
	return 0
}

// cmdVoid takes one record out of the citable store by hand, for the cases the
// sweep's predicate does not cover.
func cmdVoid(storeDir, id, reason string) int {
	store, err := gate.OpenStore(storeDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gate void:", err)
		return exitError
	}
	rec, err := store.Void(id, reason, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gate void:", err)
		return exitError
	}
	fmt.Println(rec.Summary())
	return 0
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
