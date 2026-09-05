# React migration completion contract

Owner-aligned revision, 2026-09-05. The current definitions and dependencies of
🎯T540 and its related targets in Bullseye are authoritative. This document
explains their scope and interpretation; it is not a second progress ledger.

The desired state is: **the React cockpit is Jevons's sole supported UI and
fulfils the accepted interaction contract across agent roles.** Retiring the
vanilla runtime is a useful, independently achievable milestone. It does not
establish that the accepted behavior has been implemented.

## Reference and precedence

The contract follows the [reviewed fidelity audit](../audits/react-fidelity-2026-09-05.md),
its [evidence map](../audits/react-fidelity-2026-09-05/map.json), and its
[backend assessment](../audits/react-fidelity-2026-09-05/backend-unification.md).
The baseline is fixed:

- Legacy/source reference: `8dd6e1694bbfb9ca1ac335f2c2d6ca939ce30fab`.
- Reviewed audit commit: `bfd890f706780352e5dd0da8b6c67f320d222641`.
- Evidence-map Git blob: `af2118dd7163f92c34638e1e99f8faa28c327593`.
- Inventory: 329 assessments and 917 clause assessments, including discoveries
  outside the old 302-row census.

Those counts define an accounting boundary, not equally weighted features or
a completion percentage. The map contains overlap, requirements outside the
cockpit, deliberate changes, inherited shortcomings and uncertainty. Its
original requirements and clause assessments must remain traceable; the
implementer cannot make the gate easier by silently shrinking its inventory.
Deliberate reviewed revisions may extend or correct the baseline.

Interpret evidence in this order: current owner intent; explicit later product
decisions; the old main-interaction contract and legacy implementation evidence.
A retired target is useful historical evidence, not proof that its acceptance
criteria were implemented correctly or still describe current intent.

Two owner instructions govern the migration:

1. **The old sidebar is not a parity oracle.** Main and sidebar must share the
   interaction, transcript and composer components and almost identical
   main-derived behavior. Agent identity, density and deliberate presentation
   differences are parameters. Do not restore the old sidebar's forked code,
   polling or divergent Close/Refresh behavior. Recover useful side-conversation
   lifecycle capabilities through the shared components.
2. **Agent roles share conversation semantics.** Overseer, PO, worker and aside
   are comparable named agents. History, provenance, delivery outcomes and
   recovery should have consistent meanings across APIs. Hierarchy, authority,
   scheduling, persistent versus disposable seats and provider capabilities may
   legitimately differ. This does not call for a wholesale backend rewrite.

## Retirement and completion are different targets

| Target | Desired result | Dependency boundary |
|---|---|---|
| 🎯T540.2 | React is independently packaged and served; the vanilla runtime and standing comparison service are retired. | Only 🎯T624 and 🎯T557.1. No broad fidelity or backend-repair prerequisite. |
| 🎯T557.1 | Standing UI test entry points validate React, with every removed legacy suite's assertions preserved as coverage, explicit open obligations or a valid scope decision. | Enables retirement without declaring missing behavior passed. |
| 🎯T540.3 | The composed React product fulfils the accepted behavior from the reviewed audit. | Composes the existing shared-component targets, 🎯T562, 🎯T627 and the 🎯T540.7 evidence gate. |
| 🎯T540.7 | Standing checks enforce evidence and dispositions for the full frozen inventory. | Composes 🎯T540.7.1, 🎯T624 and 🎯T625; it does not require a running vanilla UI. |
| 🎯T540 | The sole supported React UI fulfils the accepted interaction contract across roles. | Requires both retirement and behavior completion, plus 🎯T623–🎯T625 and 🎯T627. |

Retirement requires a pristine canonical build and package that serves React
root, assets and deep links when launched outside the checkout. A pre-existing
`ui/dist` or Vite-only build is insufficient. Preserve the clean-checkout Go
embed guarantee, move still-used shared protocol logic deliberately, and remove
vanilla dependencies from supported runtime, installation, restart and test
paths. Remove the standing `:13706` service from tracked and installed setup
without disrupting the development daemon or fleet. A small frozen regression
fixture may remain; a second runnable cockpit may not.

**Owner clarification, 2026-09-05:** this is the browser cutover. Migrating
the native iOS wrapper or Pigeon transport is separate work, not a condition
of retiring the vanilla browser UI. The earlier iOS clause in 🎯T540.2
overextended that scope and has been removed. 🎯T628 and its transport
findings remain separately recorded; they do not block 🎯T540.2. The same
separation applies to the independently discovered PO recovery gap, 🎯T629.

The retained and replacement assertions and package/runtime checks must pass
with recorded GREEN gates. A thin real-agent journey through the packaged
React UI establishes that retirement leaves a usable product. Observe the
development surface after activation. Missing feature obligations may remain
open under 🎯T540.3/🎯T540.7; deleting an obsolete test cannot erase them.
Historical two-UI staging attestations under 🎯T540.1/🎯T540.4 remain history and
do not require keeping the comparison service alive.

## Workstreams established by the audit

| Workstream | Target ownership and scope |
|---|---|
| Shared agent conversation and delivery | 🎯T627 with 🎯T623: populated history, explicit origin, correlated delivery outcomes, durable pending obligations, selected-agent metadata, recovery policy and migration receipts. |
| Shared composer | 🎯T562: visible busy queue, force-send, interruption, highlighted history recall and redo/append, growth and caret behavior, dictation, images and per-conversation draft recovery. |
| Fleet context and control | 🎯T540.3: selected ledger queries and target identity, scoped engagement/actions/graphs, model migration, portfolio grouping, pushed refresh, actual card metadata and invalidation. |
| Side-conversation lifecycle | 🎯T540.3: aside/target/idea routing, opening requests, agent phase, dismissal and historical browsing through the shared widget. |
| Transcript, rich content and observability | 🎯T540.3 and 🎯T540.7.1: indexed history, turn order, reconnect, multi-block rendering, follow/scroll behavior, graph packs, accepted rich content and real usage/phase/attention/Coach data and actions. |
| Packaging and trustworthy verification | 🎯T540.2/🎯T557.1, 🎯T624 canonical type-check/build, 🎯T625 journey correlation, and 🎯T540.7 standing completion checks. |

These workstreams preserve important details that a generic parity checklist
would lose:

- 🎯T241 force-send supersedes Alt/Option+Enter pop-last. A seed-only composer
  with a queue must force-send the queued item. Help text must match.
- 🎯T88 history means highlighted recall and primary rewind-and-resend through
  the provider path, with an explicit append escape and cancellation. Merely
  copying old text into the composer does not fulfil it.
- 🎯T294 multi-component graph packs and newer phase, usage, budget and
  provenance decisions supersede older presentation. Preserve working
  acceptance/dependency card content while fixing metadata and invalidation.
- 🎯T569 repeated-identical-answer suppression needs an explicit product
  decision. Optional dollar chrome and the old worker dashboard need not
  return if the owner retires them. The old Play/Stop error-notice shortcoming
  is inherited hardening, not a lost React capability.
- Root phase projected onto a selected agent must be corrected before simply
  showing the main phase strip in compact mode. Reusing a component does not
  prove its participant-specific data is correct.
- B5 recovery-policy divergence and B6 mux locking are source-supported risks,
  not reproduced production incidents. Test the actual paths before claiming
  failure or resolution. Preserve the accepted synchronous `thread_direct`
  operation and legitimate role policy.
- Migration receipts need honest receiver evidence. A stored handover draft
  or an invocation of Claudia `Agent.Migrate` does not establish what reached
  the successor. Verify actual continuity, including 🎯T622 integration,
  without presuming the existing seed is wrong.
- 🎯T540.7.1 protects owner-turn boundaries on the canonical indexed live/replay
  path. A reused-stream raw reducer fixture is compatibility evidence; it does
  not by itself establish a current indexed-product defect or prescribe a fix.

## What counts as completion

Every assessment and clause needs a traceable current interpretation and
disposition: implemented with suitable evidence; an existing explicit
supersession or owner decision; demonstrated non-applicability; or still open.
Newly dropping accepted behavior requires an explicit owner decision. Missing,
partial, code-supported but unexecuted, skipped, assigned and uncertain states
are not passing completion states. Identify inherited hardening separately.

Standing checks must exercise mounted React controls with realistic data
through production mappers and APIs. Helper tests, source greps, coverage-list
mentions and richer-than-product fixtures remain supporting evidence. They
cannot establish that the composed cockpit works. An absent feature skipped by
the test is an open obligation, not a pass.

The final evidence includes a real-provider owner → overseer → worker → result
journey through the canonical React UI and representative side-conversation,
busy-send, failure/recovery, reload/reconnect and long-history slices. Record
the effective provider and correlate a fresh request-specific outcome using
the actual wire shape. Missing providers are outages, not skip-and-green.
Changes to provider process contracts require their affected live gates or
explicit unresolved/accepted residue. Geometry changes require a real-render
prose verdict; owner-visible claims require observation of the development
surface.

The checks must themselves be challenged: removing an interaction binding or
required inventory row, skipping an assertion, breaking a mapper, introducing
a type error or accepting an unrelated reply must fail the relevant standing
gate. Keep evidence tied to a clean committed revision and recorded gate
results. The audit's green defect-reproduction tests are evidence of defects,
not evidence of migration success.

This revision changes the definition of completion. It does not claim that
these behaviors now work, achieve any migration target, start repair agents,
retire the running service or authorize publication. Local implementation and
its evidence can complete the targets without a push or release. Unrelated
voice, mobile onboarding, provider expansion and the rest of daily-driver
readiness remain outside this migration contract.
