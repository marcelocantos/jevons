// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "strings"

// FleetStandingBrief is prepended to the first jevons_agent_send of each
// fleet child so PO/workers inherit product delivery + spawn doctrine
// without relying on the parent to remember (🎯T78 / 🎯T104 / 🎯T111.4 / 🎯T125 under fan-out).
const FleetStandingBrief = `[Jevons fleet standing brief — apply for this whole assignment]

## Delivery: local by default (🎯T104)
- Done = local commits + oracle evidence + notify overseer.
- Do NOT open GitHub PRs, run PR-creation flows, or treat "opened a PR" as done
  unless the owner (or overseer on the owner's clear instruction) asked to ship via PR.
- "master" / "merge to master" means local branch master, not origin/master.
- "locally" means no git push, no GitHub PR, no CI merge.

## Fleet spawn (🎯T78)
- Create child work via jevons_agent_start / jevons_thread_spawn, not Grok spawn_subagent.

## Multi-slice fan-out (🎯T111.4)
- If this mission has multiple independent slices, spawn jevons_agent_start
  children (with parent/actor lineage) early — do not stay in unbounded solo
  read/plan loops. Single-agent tasks are fine; multi-slice briefs require fan-out.
- Zero children after planning on a multi-slice mission is a failure mode the
  overseer can see (agent_list fan-out check).

## PO never implements (🎯T125)
- If you are a Stratum-1 product owner: spawn-only for Build work — never
  implement yourself (including small patches, oracles, docs commits).
  Stay interruptible for overseer/owner directs; workers/bosses execute.
- Residual: instructional doctrine, not a hard spawn-gate in the daemon.

## Report
- When finished: report commit SHA(s) + test evidence to the overseer.
- Do not ambient-autopilot /push or gh pr create.

---
`

// EnsureFleetBrief prefixes text with FleetStandingBrief when this is the
// first send to name in this process. already maps agent name → briefed.
func EnsureFleetBrief(already map[string]bool, name, text string) (out string, injected bool) {
	if already == nil {
		return text, false
	}
	if name == "" || already[name] {
		return text, false
	}
	// Skip if caller already included the standing brief (idempotent).
	if strings.Contains(text, "Jevons fleet standing brief") {
		already[name] = true
		return text, false
	}
	already[name] = true
	return FleetStandingBrief + text, true
}
