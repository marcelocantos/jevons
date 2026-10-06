// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"strings"

	"github.com/marcelocantos/claudia"
)

// unsteerableReasonFn backs UnsteerableReason. It is a var (not a
// hardcoded call) so SetUnsteerableForTest can swap in a synthetic
// answer without this package maintaining its own data table.
var unsteerableReasonFn = claudiaSteerableReason

// claudiaSteerableReason asks claudia whether provider's seats can
// receive jevons_* tool bindings, via [claudia.Steerable] /
// [claudia.CapabilityMCPTools] (🎯T1013.3).
//
// This used to be backed by a hand-maintained map in this package
// (🎯T791): a provider listed there had seats that could not receive
// jevons_* tools or accept steering, so plan headroom never made it a
// mint destination. That table tracked facts about claudia's own
// wiring and known provider defects by hand — "codex left with claudia
// v0.42.0", "cursor left once [a bug] was fixed" — which is a second
// copy of a fact claudia itself is in a better position to know and
// keep current. The capability table now lives in claudia; this
// function is a thin adapter.
func claudiaSteerableReason(provider string) string {
	ok, reason := claudia.Steerable(claudia.Provider(strings.ToLower(strings.TrimSpace(provider))))
	if ok {
		return ""
	}
	return reason
}

// UnsteerableReason returns why provider's seats cannot be steered, or ""
// when they can (or the provider is unknown).
func UnsteerableReason(provider string) string {
	return unsteerableReasonFn(strings.ToLower(strings.TrimSpace(provider)))
}

// SetUnsteerableForTest swaps the steerability answer for the duration of
// a test and returns a restore func. m maps a (lower-cased, trimmed)
// provider name to its unsteerable reason; a provider absent from m is
// reported steerable. This exists so mint-exclusion mechanism tests can
// exercise the "a provider is excluded" code path without depending on
// which providers claudia currently reports as unsteerable for real.
func SetUnsteerableForTest(m map[string]string) (restore func()) {
	old := unsteerableReasonFn
	unsteerableReasonFn = func(provider string) string {
		return m[strings.ToLower(strings.TrimSpace(provider))]
	}
	return func() { unsteerableReasonFn = old }
}
