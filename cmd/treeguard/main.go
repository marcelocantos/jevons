// Command treeguard is the Claude Code hook that stops concurrent fleet
// workers from silently clobbering each other's edits to shared hot files
// (🎯T376).
//
// Wired from .claude/settings.json via scripts/hooks/treeguard:
//
//	PreToolUse  Write|Edit|MultiEdit|NotebookEdit|Bash → treeguard pre
//	PostToolUse Read|Write|Edit|MultiEdit|…|Bash       → treeguard post
//
// Both read one Claude Code hook payload as JSON on stdin. Bash is there for
// 🎯T391: `sed -i`, a `>` redirection and `git checkout -- <path>` reach the
// same bytes as Write and none of them reaches the tool boundary.
//
// The third mode takes no payload:
//
//	treeguard sweep   (scripts/hooks/pre-commit)
//
// It reports guarded files that changed without the guard seeing it. That is
// the half of 🎯T391 no hook can do — a worker under a provider with no hooks
// at all still runs git commit, and a command whose target the recognizer
// cannot decide statically still lands on disk.
//
// Exit codes follow the hook contract: 0 proceeds, 2 blocks the tool call and
// shows stderr to the agent, anything else is a non-blocking error. A write
// that would drop another worker's lines exits 2 and names those lines. Sweep
// never exits 2: the loss it reports has already happened, so refusing the
// commit would punish the wrong worker.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/marcelocantos/jevons/internal/treeguard"
)

// Hook contract exit codes (see package doc). A denial must be 2: 1 is only a
// tooling failure, which must never block the fleet.
const (
	exitAllow = 0
	exitError = 1
	exitDeny  = 2
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: treeguard pre|post < hook.json | treeguard sweep|doctor")
		os.Exit(exitError)
	}
	mode := os.Args[1]

	switch mode {
	case "doctor":
		doctor()
		return
	case "sweep":
		// The provider-independent boundary (🎯T391): git pre-commit is the one
		// point every worker passes through whatever harness it runs under, so
		// this mode takes no payload and reads only the working tree.
		os.Exit(sweep())
	}

	payload, err := treeguard.DecodePayload(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "treeguard: cannot read hook payload: %v\n", err)
		os.Exit(exitError)
	}
	env := treeguard.NewEnv(payload)

	switch mode {
	case "pre":
		decision, err := env.Pre(payload)
		if err != nil {
			fmt.Fprintf(os.Stderr, "treeguard: %v\n", err)
			os.Exit(exitError)
		}
		if decision.Verdict == treeguard.Deny {
			fmt.Fprintln(os.Stderr, decision.Message)
			os.Exit(exitDeny)
		}
	case "post":
		report, err := env.Post(payload)
		if err != nil {
			fmt.Fprintf(os.Stderr, "treeguard: %v\n", err)
			os.Exit(exitError)
		}
		// A detected unguarded change is reported, never blocked: it has
		// already landed, and exiting non-zero here would only fail a tool call
		// that is not the one that caused it (🎯T391).
		if report != "" {
			fmt.Fprint(os.Stderr, report)
		}
		// Best-effort housekeeping; a full disk must not block the fleet.
		_ = env.Store.Prune(treeguard.ObservationTTL, time.Now())
	default:
		fmt.Fprintf(os.Stderr, "treeguard: unknown mode %q\n", mode)
		os.Exit(exitError)
	}
	os.Exit(exitAllow)
}

// sweep reports guarded files that changed without the guard seeing it and
// lost lines doing so. It never refuses: the write has already landed, and a
// pre-commit hook that blocked here would punish whoever is committing rather
// than whoever clobbered. Exit is 0 even with findings, for the same reason.
func sweep() int {
	env := treeguard.NewEnv(&treeguard.Payload{})
	if env.Disabled {
		return exitAllow
	}
	now := time.Now()
	// The provider statement rides the same boundary as the sweep, because it
	// is the same problem: a worker whose harness has no hooks reaches this
	// point and no other (🎯T391 acceptance 2).
	fmt.Fprint(os.Stderr, env.CoverageNotice(treeguard.DetectProvider(os.Getenv), now))
	findings, err := env.Sweep(now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "treeguard: sweep: %v\n", err)
		return exitError
	}
	if err := env.RecordSweep(findings); err != nil {
		fmt.Fprintf(os.Stderr, "treeguard: recording sweep: %v\n", err)
		return exitError
	}
	fmt.Fprint(os.Stderr, treeguard.FormatSweepReport(findings))
	return exitAllow
}

func doctor() {
	env := treeguard.NewEnv(&treeguard.Payload{})
	fmt.Println("repo root:  ", env.RepoRoot)
	fmt.Println("store root: ", env.Store.Root)
	fmt.Println("enabled:    ", !env.Disabled)
	fmt.Println("shell half: ", !env.BashOff)
	fmt.Println("coverage:   ", treeguard.ProviderCoverage(treeguard.DetectProvider(os.Getenv)).BriefLine())
	fmt.Println("guarded paths:")
	for _, p := range env.Guarded {
		fmt.Println("  ", p)
	}
}
