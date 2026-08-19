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
//	bin/gate check report.md        # flag a finish report's false green
//	bin/gate check report.json      # …same; JSON envelopes decode to text (🎯T468)
//	bin/gate check < report.md      # …the same, from stdin
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
//
// 🎯T453 closes the other half of that hole: arguments a *known* subcommand
// cannot honour. `gate last extra-junk-arg` printed the newest record and
// exited 0, discarding the argument silently — and the caller who typed it
// cites the output of a command that did not do what was asked. Worse,
// `gate check /path/to/report.md` — the form a reader naturally types after
// seeing a report on disk, and the form the stdin example above invites them
// to get wrong — ignored the path and blocked on stdin with nothing printed.
// Under a background gate that is indistinguishable from a hung suite: the
// harness reports a failure with no reason and nothing points at the command
// line. So `check` now reads a path when given one, and every subcommand
// refuses an argument it will not act on rather than proceeding beside it.
//
// A caveat this deliberately does not cover: bare `gate check` with no path
// still waits on stdin, which is correct for a filter and is what the honoured
// `gate check < report` form needs. The observed hang was not a terminal — it
// was a pipe that never closed — so a TTY check would not have caught it,
// while the path argument does.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/shaevidence"
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
		clean        bool
		commit       string
		keep         bool
	)
	flag.StringVar(&name, "name", "", "label for this gate in the attestation")
	flag.StringVar(&dir, "dir", "", "working directory for the command")
	flag.StringVar(&storeDir, "store", "", "record directory (default ~/.jevons/gates)")
	flag.BoolVar(&quiet, "quiet", false, "print only the attestation line, not the command's output")
	flag.BoolVar(&allowSuspect, "allow-suspect", false,
		"exit with the command's own status even when its output contradicts a pass")
	flag.BoolVar(&clean, "clean", false,
		"run the command in a fresh checkout of the commit, not in the shared working tree (🎯T397)")
	flag.StringVar(&commit, "commit", "",
		"with -clean: the commit to verify (default HEAD)")
	flag.BoolVar(&keep, "keep", false,
		"with -clean: leave the checkout behind for inspection")
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

	if clean {
		os.Exit(cmdRunClean(args, name, dir, commit, storeDir, quiet, allowSuspect, keep))
	}
	os.Exit(cmdRun(args, name, dir, storeDir, quiet, allowSuspect))
}

// subcommands is the allowlist. A word outside it is a typo, not a gate.
var subcommands = []string{"last", "show", "check", "sweep", "void", "help"}

// usageForms is the shape each subcommand accepts, quoted back at a caller
// whose arguments do not fit it. It lives next to the allowlist so that adding
// a subcommand without saying how to call it is visibly incomplete.
var usageForms = map[string]string{
	"last":  "gate last",
	"show":  "gate show <id>",
	"check": "gate check [report-path]   (or: gate check < report)",
	"sweep": "gate sweep [-void]",
	"void":  "gate void <id> <reason>",
}

// refuseSurplus rejects arguments a known subcommand cannot honour (🎯T453).
//
// The alternative is what the tool used to do: perform the part it understood
// and exit 0, discarding the rest. That is a false green in miniature — the
// caller reads a success from a command that did not do what they asked, and
// under 🎯T386 that output is evidence. Naming the argument is the whole
// remedy: the reader needs to be pointed at their own command line.
func refuseSurplus(sub string, extra []string) int {
	fmt.Fprintf(os.Stderr, "gate %s: unexpected argument %s\n  usage: %s\n",
		sub, strings.Join(quoteAll(extra), " "), usageForms[sub])
	return exitUsage
}

// refuseFlag rejects an unrecognised flag where an operand was expected. It is
// separate from refuseSurplus because `gate void -x id reason` used to read the
// flag as the record id and fail with "no such record" — an error that blames
// the store for what the command line got wrong.
func refuseFlag(sub, flag string) int {
	fmt.Fprintf(os.Stderr, "gate %s: %q is not a %s flag\n  usage: %s\n",
		sub, flag, sub, usageForms[sub])
	return exitUsage
}

func quoteAll(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = fmt.Sprintf("%q", a)
	}
	return out
}

// isFlag reports whether an argument was meant as a flag rather than an
// operand. A bare "-" is a conventional stdin operand, not a flag.
func isFlag(arg string) bool { return strings.HasPrefix(arg, "-") && arg != "-" }

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
	sub, rest := args[0], args[1:]
	switch sub {
	case "last":
		if len(rest) > 0 {
			return refuseSurplus(sub, rest)
		}
		return cmdLast(storeDir)
	case "show":
		if len(rest) == 0 {
			fmt.Fprintln(os.Stderr, "gate show: need a record id\n  usage:", usageForms[sub])
			return exitUsage
		}
		if isFlag(rest[0]) {
			return refuseFlag(sub, rest[0])
		}
		if len(rest) > 1 {
			return refuseSurplus(sub, rest[1:])
		}
		return cmdShow(storeDir, rest[0])
	case "check":
		return cmdCheck(storeDir, rest)
	case "sweep":
		return cmdSweep(rest, storeDir)
	case "void":
		if len(rest) > 0 && isFlag(rest[0]) {
			return refuseFlag(sub, rest[0])
		}
		if len(rest) < 2 {
			fmt.Fprintln(os.Stderr, "gate void: need a record id and a reason\n  usage:", usageForms[sub])
			return exitUsage
		}
		return cmdVoid(storeDir, rest[0], strings.Join(rest[1:], " "))
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
  gate -clean -- <command> [args...]    …in a fresh checkout of HEAD (🎯T397)
  gate last                             show the most recent run
  gate show <id>                        show one run
  gate check [report-path]              flag a finish report's false greens (stdin if no path)
  gate sweep [-void]                    list records that attest nothing; -void quarantines them
  gate void <id> <reason>               take one record out of the citable store

The -- is required to run a command: without it a mistyped subcommand would be
executed and recorded as though it were a gate (🎯T441). A subcommand given an
argument it cannot honour is refused rather than performed in part (🎯T453).

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
		// fs has already named the flag it did not recognise; add the form that
		// would have worked, since the reader is here because they guessed.
		fmt.Fprintln(os.Stderr, "  usage:", usageForms["sweep"])
		return exitUsage
	}
	// A sweep decides which evidence stops being citable. An operand it does
	// not understand might have been meant to narrow that, so proceeding over
	// the whole store would be the opposite of what was asked.
	if fs.NArg() > 0 {
		return refuseSurplus("sweep", fs.Args())
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

// cmdCheck reads a finish report — from a named file, or from stdin when no
// file is named — and flags a green its own cited evidence does not support.
//
// The path form exists because it is what a reader types (🎯T453). Taking the
// path silently and then waiting on stdin was the worst shape available: no
// output, no progress, and a wait with no bound, which under a background gate
// reads as a hung suite rather than as a misuse. Reading the file cannot block.
func cmdCheck(storeDir string, args []string) int {
	if len(args) > 0 && isFlag(args[0]) {
		return refuseFlag("check", args[0])
	}
	if len(args) > 1 {
		return refuseSurplus("check", args[1:])
	}
	read := func() ([]byte, error) { return io.ReadAll(os.Stdin) }
	if len(args) == 1 && args[0] != "-" {
		read = func() ([]byte, error) { return os.ReadFile(args[0]) }
	}
	raw, err := read()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gate check:", err)
		return exitError
	}
	store, err := gate.OpenStore(storeDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gate check:", err)
		return exitError
	}
	// 🎯T468: stored reports are JSON envelopes; scan the decoded text, not
	// the escaped file bytes. Same decoder the daemon's report readers use.
	body := agentreport.DecodeBody(raw)
	flags := gate.FlagFalseGreen(body, store.Lookup)
	// 🎯T427: a cited evidence SHA must still be an ancestor of HEAD. When
	// check is running inside (or beside) a git repo, compose the reachability
	// flags; offline / non-git callers keep the textual false-green set alone.
	if root := gitRootNear(checkPath(args)); root != "" {
		flags = append(flags, gate.FlagUnreachableSHAs(body, shaevidence.CheckInRepo(root, "HEAD"))...)
	}
	if len(flags) == 0 {
		fmt.Println("no false-green flags")
		return 0
	}
	fmt.Println(strings.TrimSpace(gate.Banner(flags)))
	return exitFlagged
}

// checkPath is the report path named on the command line, or "" for stdin.
func checkPath(args []string) string {
	if len(args) == 1 && args[0] != "-" {
		return args[0]
	}
	return ""
}

// gitRootNear returns the git toplevel containing path (or the process cwd
// when path is empty). Empty when git is missing or the location is not a
// work tree — callers then skip the 🎯T427 reachability check.
func gitRootNear(path string) string {
	dir := path
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return ""
		}
		dir = wd
	} else if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
		dir = filepath.Dir(dir)
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
