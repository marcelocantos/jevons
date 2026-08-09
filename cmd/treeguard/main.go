// Command treeguard is the Claude Code hook that stops concurrent fleet
// workers from silently clobbering each other's edits to shared hot files
// (🎯T376).
//
// Wired from .claude/settings.json via scripts/hooks/treeguard:
//
//	PreToolUse  Write|Edit|MultiEdit|NotebookEdit → treeguard pre
//	PostToolUse Read|Write|Edit|MultiEdit|…       → treeguard post
//
// Both read one Claude Code hook payload as JSON on stdin.
//
// Exit codes follow the hook contract: 0 proceeds, 2 blocks the tool call and
// shows stderr to the agent, anything else is a non-blocking error. A write
// that would drop another worker's lines exits 2 and names those lines.
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
		fmt.Fprintln(os.Stderr, "usage: treeguard pre|post|doctor < hook.json")
		os.Exit(exitError)
	}
	mode := os.Args[1]

	if mode == "doctor" {
		doctor()
		return
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
		if err := env.Post(payload); err != nil {
			fmt.Fprintf(os.Stderr, "treeguard: %v\n", err)
			os.Exit(exitError)
		}
		// Best-effort housekeeping; a full disk must not block the fleet.
		_ = env.Store.Prune(treeguard.ObservationTTL, time.Now())
	default:
		fmt.Fprintf(os.Stderr, "treeguard: unknown mode %q\n", mode)
		os.Exit(exitError)
	}
	os.Exit(exitAllow)
}

func doctor() {
	env := treeguard.NewEnv(&treeguard.Payload{})
	fmt.Println("repo root:  ", env.RepoRoot)
	fmt.Println("store root: ", env.Store.Root)
	fmt.Println("enabled:    ", !env.Disabled)
	fmt.Println("guarded paths:")
	for _, p := range env.Guarded {
		fmt.Println("  ", p)
	}
}
