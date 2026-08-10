// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"sync"

	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T386: the overseer is the independent gate for oracle-first completion
// (🎯T31.1), but it can only judge the evidence a worker chose to send. When
// that evidence contradicts itself — a green claimed over a timeout panic, a
// status read off a pipeline, a shell array that expanded to nothing — the
// contradiction is decidable here, on the way past, and saying so costs one
// banner. Two reports in one session nearly retired targets on such a green.
//
// This is a warning, not a block. Reaping and delivery are unchanged: the
// report still reaches the overseer whole, with the flags in front of it, and
// the overseer decides. A hard refusal would be the wrong trade — the
// classifier is a heuristic, and an agent unable to report at all is a worse
// failure than one whose report arrives annotated.

// gateStore is opened once for the daemon's lifetime. A store that cannot be
// opened degrades to textual checking, which needs no disk: the flags that
// matter most (pipeline masking, the zsh array trap, output that contradicts
// the claim) are all decidable from the report alone.
var gateStore = sync.OnceValue(func() *gate.Store {
	store, err := gate.OpenStore("")
	if err != nil {
		return nil
	}
	return store
})

// FalseGreenFlags reports the ways a worker's finish report fails to support
// the pass it claims. Empty for the overwhelming majority of reports.
func FalseGreenFlags(report string) []gate.Flag {
	var lookup func(string) (*gate.Record, bool)
	if store := gateStore(); store != nil {
		lookup = store.Lookup
	}
	return gate.FlagFalseGreen(report, lookup)
}

// FalseGreenBanner is the note prepended to a flagged report on its way to
// the overseer, or "" when the report does not contradict itself.
func FalseGreenBanner(report string) string {
	return gate.Banner(FalseGreenFlags(report))
}

// falseGreenKinds names the flags for the lifecycle log, which is read as a
// stream rather than as prose.
func falseGreenKinds(flags []gate.Flag) []string {
	kinds := make([]string, 0, len(flags))
	for _, f := range flags {
		kinds = append(kinds, string(f.Kind))
	}
	return kinds
}
