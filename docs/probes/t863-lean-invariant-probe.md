# 🎯T863 probe — Lean on one Jevons invariant

## What we tried

Invariant: **🎯T862.17 writ policy fail-closed** (`internal/writconf`).

- Go oracles: `TestT862_17MalformedPolicyRefusesLoadAndExecution`, `ClassifyAccess` nil-manifest deny (`internal/writconf/check.go`).
- Lean: abstract finite model in `lean/Invariants/T862_17_FailClosed.lean` (policy sample tags mirror the Go JSON fixtures).
- Agent skill: `skills/jevons-lean-invariant/SKILL.md`.
- Thin oracle: `scripts/lean-oracle.sh` (`build` / `sorries` / `check`).
- Hermetic pin: `internal/writconf/t863_lean_oracle_test.go` + `make test-lean-invariants`.

## Probe result (recommendation)

**Thin Lean oracle is warranted; a deep Jevons↔Lean bridge is not (yet).**

Evidence from this slice:

1. An agent with only the skill + `lean-oracle.sh` can close sorry holes on a
   *finite abstraction* of a real invariant in one sitting (~130 lines Lean).
2. The abstraction is honest but partial: it proves the reliability property on
   named policy shapes, not JSON parsing correctness. The Go tests remain the
   ground truth for bytes on the wire.
3. Skill-only (markdown nudging “use Lean”) would not have produced checkable
   glue; the sorry counter + `lake build` feedback loop mattered.

**Not recommended now:** extracting Go into Lean automatically, Bullseye ledger
formalization, or daemon lifecycle modeling in Lean — cost dominates without a
clear extraction path from production code.

## Non-goals (unchanged)

- Rewriting Jevons in Lean.
- Translating Bullseye targets into Lean propositions at scale.
