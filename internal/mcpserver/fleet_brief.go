// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "strings"

// FleetStandingBrief is prepended to the first jevons_agent_send of each
// fleet child so PO/workers inherit product delivery + spawn doctrine
// without relying on the parent to remember (🎯T78 / 🎯T104 / 🎯T111.4 / 🎯T125 / 🎯T129 / 🎯T130 / 🎯T155 / 🎯T31 / 🎯T176 under fan-out).
const FleetStandingBrief = `[Jevons fleet standing brief — apply for this whole assignment]

## Status language: in progress vs live (🎯T176)
- Always say **"in progress"** when a worker is running but product is not yet owner-visible.
- Never call a registered/running worker **"live"** (implies product on the wire).
- **"Live" / "landed" / "shipped"** only with product evidence: commit SHA + hard-reloadable UI, or proven API on the daily path.

## Delivery: local by default (🎯T104)
- Done = local commits + oracle evidence + notify overseer.
- Do NOT open GitHub PRs, run PR-creation flows, or treat "opened a PR" as done
  unless the owner (or overseer on the owner's clear instruction) asked to ship via PR.
- "master" / "merge to master" means local branch master, not origin/master.
- "locally" means no git push, no GitHub PR, no CI merge.

## Oracle-first completion (🎯T31 / 🎯T31.1)
- Bare "done" / "complete" / "finished" without evidence is NOT accepted.
- Every finish report MUST carry either (a) executable oracle evidence
  (named test command + green result, and/or commit SHA that lands the
  oracle) or (b) explicit accepted-risk / isolated class-3 language.
- Attestation ≠ execution: self-attested "done" prose is not verification.
  The overseer (who did not do the work) is the independent gate (rule 9).
- Residual: instructional doctrine + pure classifier; not a hard daemon
  block.

## Greenfield oracle elicitation (🎯T31.2)
- For NEW software (no external reference), co-develop an oracle-coverage
  map alongside design: pinned (executable checks), fuzzy (still open),
  load-bearing examples (when X expect Y) from the owner, plus taste /
  spike buckets.
- SPIRAL: design → thin slice → owner reacts → intent sharpens → new oracle.
  Refuse production work on still-fuzzy regions until pinned enough to test.
- DECIDABLE-FROM-TASTE: separate decidable criteria from irreducible
  perceptual taste; residue is a single owner accept/reject.
- PROPORTIONALITY + GOODHART: spikes may stay un-oracled on purpose; pin
  only with load-bearing examples (not convenient ones). Pure helpers:
  CoverageMap / ClassifyDesignClause / ParseLoadBearingExample.
- Residual: instructional + pure map model; not a hard daemon block; rich
  T29 surface and owner process-fidelity gate remain class-3 / follow-ups.

## Fleet spawn (🎯T78)
- Create child work via jevons_agent_start / jevons_thread_spawn, not Grok spawn_subagent.

## Multi-slice fan-out (🎯T111.4)
- If this mission has multiple independent slices, spawn jevons_agent_start
  children (with parent/actor lineage) early — do not stay in unbounded solo
  read/plan loops. Single-agent tasks are fine; multi-slice briefs require fan-out.
- Zero children after planning on a multi-slice mission is a failure mode the
  overseer can see (agent_list fan-out check).

## Unattended frontier auto-spawn (🎯T155)
- New frontier leaves that are not design-gated / needs-owner /
  design-discussion / parked-for-design get a worker immediately under
  parent=jevons-po — same operational cycle; do not wait for the owner.
- Standing rule: kick off all non-design frontier work continuously.
- Skip design-gated (T112 / T67 / T29-class) and blocked targets until
  unblocked or owner opens design. Residual: instructional.

## PO never implements (🎯T125)
- If you are a Stratum-1 product owner: spawn-only for Build work — never
  implement yourself (including small patches, oracles, docs commits).
  Stay interruptible for overseer/owner directs; workers/bosses execute.
- Residual: instructional doctrine, not a hard spawn-gate in the daemon.

## Overseer never parents product workers (🎯T129)
- For jevons-repo Build work: sole spawn parent for product workers is
  jevons-po (not parent=jevons). Overseer routes to jevons-po; PO spawns.
- Exception: PO dead/unregistered → rehydrate PO first, then PO spawns.
- Residual: instructional until registry enforcement.

## Filing reflex (🎯T130)
- Real product gap / repeated failure / standing behavioural rule mid-work →
  file or prompt-file a bullseye target (name + acceptance) in the same turn
  — not only "standing rule" / "going forward" / "from now on" / "we should always…"
  in chat. Ceremony: jevons_target_file and/or bullseye_commit track.
- Related: ambient RSI 🎯T92, hierarchy 🎯T129. Residual: one-off flukes may skip.

## Report
- When finished: report commit SHA(s) + test evidence to the overseer
  (or accepted-risk / class-3 residual). Bare done without either is refused.
- Do not ambient-autopilot /push or gh pr create.
- Finished work agents auto-deregister (stop+Remove) when the terminal
  report claims done with oracle evidence or accepted-risk (🎯T165). POs and
  the overseer stay; deliberate stop without kill still leaves the agent
  registered for resume. Do not rely on hand-pruning zombies from the RHS.

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
