// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ctxcap

// Action is what the daemon may do with a [Decision]. Compact-as-remint
// is withdrawn (🎯T40.2): a large conversation is observed, never minted
// onto a new session.
type Action string

const (
	// ActionNone — under the ceiling or unmeasured; stay quiet.
	ActionNone Action = "none"
	// ActionObserve — the conversation is large or held. Log it. Do not
	// rotate, seed, or mint.
	ActionObserve Action = "observe"
)

// ActionFor maps a policy verdict onto the shipped governor act.
// VerdictCompact used to mean remint; it now means observe.
func ActionFor(d Decision) Action {
	switch d.Verdict {
	case VerdictCompact, VerdictHold:
		return ActionObserve
	default:
		return ActionNone
	}
}
