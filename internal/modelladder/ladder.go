// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package modelladder answers one question: when a seat's model can no
// longer answer because its quota is gone, which model does it fall back
// to (🎯T585)?
//
// The fleet already classified this failure — agenterr returns
// ClassRateLimit — and then did the one thing that cannot work: re-brief
// the same seat on the same model. Against a weekly quota that has run
// out, re-pressure is an infinite loop, and on 2026-08-30 it ran for
// hours while the owner watched seats sit idle.
//
// Order is descending capability, not preference: the ladder exists to
// keep work moving, so each step is the next model that can still answer.
// A model absent from its provider's ladder has no fallback rather than a
// guessed one — inventing a model id fails the launch and costs a seat.
package modelladder

import "strings"

// ladders are the per-provider fallback orders. The first entry is the
// most capable; falling back always moves right.
var ladders = map[string][]string{
	"claude": {"claude-fable-5", "claude-opus-5", "claude-sonnet-5", "claude-haiku-4-5"},
	"grok":   {"grok-4.6", "grok-4.5", "grok-4"},
}

// Next returns the model to fall back to when `model` on `provider` is
// exhausted, or "" when there is nowhere left to go — an unknown
// provider, an unknown model, or the last rung. An empty result means
// the caller must escalate (another provider, or tell the owner), never
// that it should retry the same model.
func Next(provider, model string) string {
	rungs := ladders[strings.ToLower(strings.TrimSpace(provider))]
	model = strings.TrimSpace(model)
	if len(rungs) == 0 || model == "" {
		return ""
	}
	for i, m := range rungs {
		if m == model {
			if i+1 < len(rungs) {
				return rungs[i+1]
			}
			return ""
		}
	}
	return ""
}

// Known reports whether the provider has a ladder at all, so a caller can
// tell "no fallback configured" from "already on the last rung".
func Known(provider string) bool {
	return len(ladders[strings.ToLower(strings.TrimSpace(provider))]) > 0
}
