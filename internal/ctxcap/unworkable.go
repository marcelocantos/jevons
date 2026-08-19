// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ctxcap

import (
	"fmt"
	"strings"
	"time"
)

// FormatUnworkableNotice is the parent- and overseer-facing text when an
// agent lives above the context ceiling and compaction will not be looped
// (🎯T417). Pure so the wording is hermetically pinned.
//
// It names context size, ceiling, and compaction cadence — the three
// numbers a supervisor needs to decide whether to raise the ceiling,
// migrate, park, or accept the stall — and it says the agent is
// unworkable rather than implying another remint is coming.
func FormatUnworkableNotice(d Decision, minInterval time.Duration, sinceLast time.Duration) string {
	agent := strings.TrimSpace(d.Agent)
	if agent == "" {
		agent = "(unnamed)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "⚠️ Agent %s is unworkable: context %d exceeds ceiling %d.\n",
		agent, d.Context, d.Ceiling)
	b.WriteString("Compaction is not being re-run on a loop")
	if minInterval > 0 {
		fmt.Fprintf(&b, " (min compaction cadence %s)", minInterval.Round(time.Second))
	}
	b.WriteString(".\n")
	switch {
	case sinceLast > 0:
		fmt.Fprintf(&b, "Since last compaction/rotation: %s.\n", sinceLast.Round(time.Second))
	case d.Verdict == VerdictHold:
		b.WriteString("Since last compaction/rotation: within the hold window (recent).\n")
	default:
		b.WriteString("Since last compaction/rotation: none recorded (or unknown).\n")
	}
	if r := strings.TrimSpace(d.Reason); r != "" {
		fmt.Fprintf(&b, "Governor reason: %s\n", r)
	}
	b.WriteString("\nAct: raise the ceiling, migrate/remint with a thinner handover, ")
	b.WriteString("park the agent, or accept that this seat cannot progress until ")
	b.WriteString("context shrinks. Do not expect another automatic remint to fix it.\n")
	return b.String()
}

// Unworkable reports whether a decision means the agent lives above the
// ceiling and must be surfaced rather than reminted again (🎯T417).
func Unworkable(d Decision) bool {
	return ActionFor(d) == ActionUnworkable
}
