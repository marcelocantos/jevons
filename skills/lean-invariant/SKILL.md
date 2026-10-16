---
name: lean-invariant
description: Close a small Jevons reliability invariant expressed in Lean 4 (stdlib only) using lake + scripts/lean-oracle. Probe skill for 🎯T863 — not a product Lean bridge.
---

# Lean invariant probe (thin skill)

## When to use
An owner or brief asks whether a **named Jevons reliability invariant** can be
expressed and closed in Lean without a deep platform bridge.

## Non-goals
- Do **not** rewrite Jevons in Lean.
- Do **not** translate Bullseye into Lean.
- Do **not** add Mathlib unless a later target explicitly asks.
- Do **not** claim the Go production path is proven — the Lean model is a
  *mirror* of the pure policy shape; Go tests remain the product oracle.

## Workflow
1. **Pick one invariant** already on the frontier (prefer 🎯T862.12 retry
   exhaustion classification, 🎯T862.17 deny/policy fail-closed, or
   seat/remint lifecycle).
2. **Stub** under `probes/<id>-lean/` with `lean-toolchain`, `lakefile.lean`,
   and a module that states definitions + `theorem … := by sorry` goals.
3. **Run the thin oracle** from repo root:
   ```bash
   scripts/lean-oracle list-sorries probes/<id>-lean
   scripts/lean-oracle build probes/<id>-lean
   scripts/lean-oracle status probes/<id>-lean
   ```
   Structured lines: `oracle:build ok|fail`, `oracle:sorries count=N`,
   `oracle:sorry <file:line:…>`, `oracle:status GREEN|RED`.
4. **Close sorries** with ordinary Lean tactics (`simp`, `cases`, `rfl`,
   `decide`, `intro`, …). Prefer proofs that follow the definitions, not
   axiomatic `sorry` replacements (`axiom` is not a close).
5. **Stop when** `scripts/lean-oracle status` prints `oracle:status GREEN`
   (build ok **and** zero `sorry`).
6. **Write a short recommendation** next to the probe:
   - `skill-only` — prose skill was enough; oracle optional convenience
   - `thin-oracle` — structured check/build/list-sorries feedback was load-bearing
   - `deeper-bridge` — need Jevons-native Lean product (IPC, continuous CI bridge, …)
   Back every claim with what the probe actually showed.

## Proof habits that work on these stubs
- Make predicates computable (`Bool`) when the model is finite/decidable; then
  `cases` / `simp` on the match.
- Keep `Option`/`inductive` mirrors of Go nil vs value; prove fail-closed by
  exhausting constructors.
- One theorem per acceptance bullet; name them `I1`… in module docs.

## Evidence for finish reports
- `scripts/lean-oracle status …` → `GREEN`
- Commit SHA that lands stub+proofs+skill+oracle+recommendation
- Explicit recommendation verdict
