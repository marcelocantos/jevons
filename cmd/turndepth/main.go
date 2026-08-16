// Command turndepth is the Claude Code hook that keeps one turn from running
// an unbounded tool loop (🎯T392.4).
//
// Wired from .claude/settings.json via scripts/hooks/turndepth:
//
//	PreToolUse       *  → turndepth observe   (counts, and asks at the ceiling)
//	UserPromptSubmit    → turndepth observe   (turn boundary: reset)
//	Stop                → turndepth observe   (turn boundary: reset)
//
// All three read one hook payload as JSON on stdin and dispatch on its
// hook_event_name, so one mode serves every wiring and a payload arriving on
// the wrong matcher is still handled correctly.
//
// Exit codes follow the hook contract: 0 proceeds, 2 refuses the tool call
// and shows stderr TO THE MODEL, anything else is a non-blocking error. The
// checkpoint ask exits 2 exactly once per turn — that is the only way this
// system has of putting a sentence in front of a turn that is already
// running (see internal/turndepth/hook.go for why).
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/marcelocantos/jevons/internal/turndepth"
)

// Hook contract exit codes (see package doc). The ask must be 2: 1 is only a
// tooling failure, which must never impede the fleet.
const (
	exitAllow = 0
	exitError = 1
	exitAsk   = 2
)

// prunePeriod bounds the counter directory. Cheap enough to run on every
// turn boundary and never on the hot per-tool-call path.
const prunePeriod = 7 * 24 * time.Hour

func main() {
	mode := "observe"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode == "doctor" {
		doctor()
		return
	}
	if mode != "observe" {
		fmt.Fprintf(os.Stderr, "turndepth: unknown mode %q (want observe|doctor)\n", mode)
		os.Exit(exitError)
	}

	payload, err := turndepth.DecodePayload(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "turndepth: cannot read hook payload: %v\n", err)
		os.Exit(exitError)
	}
	env := turndepth.NewEnv()

	res, err := env.Observe(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "turndepth: %v\n", err)
		os.Exit(exitError)
	}
	if payload.HookEventName != turndepth.EventPreToolUse {
		// Best-effort housekeeping on the cold path; a full disk must not
		// impede the fleet.
		_ = env.Store.Prune(prunePeriod, time.Now())
	}
	if res.Ask != "" {
		fmt.Fprintln(os.Stderr, res.Ask)
		os.Exit(exitAsk)
	}
	os.Exit(exitAllow)
}

func doctor() {
	env := turndepth.NewEnv()
	fmt.Println("store root: ", env.Store.Root)
	fmt.Println("enabled:    ", !env.Policy.Disabled)
	fmt.Println("ceiling:    ", env.Policy.EffectiveCeiling())
	fmt.Println("grace:      ", env.Policy.EffectiveGrace())
	fmt.Println("interrupt fallback (daemon side): configured in jevonsd, not here")
}
