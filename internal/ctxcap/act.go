// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ctxcap

// Action is what the daemon may do with a [Decision]. Compact-as-remint
// is withdrawn (🎯T40.2): a large conversation is never minted onto a
// new session. When it stays above the ceiling, 🎯T417 reports the seat
// unworkable instead of churning remints.
type Action string

const (
	// ActionNone — under the ceiling or unmeasured; stay quiet.
	ActionNone Action = "none"
	// ActionUnworkable — the conversation is large or held above the
	// ceiling. Do not rotate, seed, or mint. Report the agent as
	// unworkable to its parent and the overseer (🎯T417).
	ActionUnworkable Action = "unworkable"
)

// ActionFor maps a policy verdict onto the shipped governor act.
// VerdictCompact used to mean remint; it now means unworkable, same as
// VerdictHold — living above the ceiling after compaction cannot help
// is the signal, not another remint.
func ActionFor(d Decision) Action {
	switch d.Verdict {
	case VerdictCompact, VerdictHold:
		return ActionUnworkable
	default:
		return ActionNone
	}
}
