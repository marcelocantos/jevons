# Jevons: the React migration and the shared agent model

Assessment dated 5 September 2026. Reference code: `8dd6e1694bbfb9ca1ac335f2c2d6ca939ce30fab`. Development services were not changed.

**The architectural reset was the right move. The migration is substantially less complete at the interaction level than the existing parity machinery suggests.** There is a real shared React conversation component and a substantial common backend. Much of the missing work consists of connecting existing behavior to that architecture and repairing the remaining adapter differences. Recreating the old sidebar would work against the intended design.

The [searchable map](react-fidelity-2026-09-05/index.html), [CSV](react-fidelity-2026-09-05/map.csv), and [full evidence map](react-fidelity-2026-09-05/map.json) retain the individual requirements, source references, current interpretations, tests, and unresolved clauses. The [backend review](react-fidelity-2026-09-05/backend-unification.md) covers the additional agent-unification question.

## The reference is the intended product

The owner's explicit carveout governs this assessment: **the old sidebar transcript is not a parity oracle**. The new sidebar should use exactly the same widget and almost identical behavior as the main transcript, allowing deliberate differences such as density. Old sidebar-specific polling, rendering, Close/Refresh controls, and quirks are historical evidence, not features to recreate.

That principle extends to the backend. Overseer, product owners, workers, and asides should share the conversation and agent interaction model. Their responsibilities, privileges, hierarchy, and recovery priorities may differ. A different role does not justify inconsistent meanings for “queued,” “sent,” or “this agent's conversation.”

The reference order is therefore: current owner intent; explicit later product decisions; the old **main** interaction contract; and old implementation as evidence of what existed. Retired targets are not infallible or mutually consistent. Where two requirements conflict, the map records the conflict instead of silently choosing one.

Several examples matter:

- T241 replaced historical Alt+Enter “pop last” behavior with force-send. Restoring pop-last would be wrong.
- T381 makes provenance, rather than Markdown-looking text, decide how an owner message paints.
- T294 restored multi-component frontier graphs after the earlier single-primary decision. Executable legacy code implements that later decision despite stale comments.
- T555/T575, T588/T588.1, and T610 introduce the newer phase strip, local-time usage table, and server-authoritative budget bands. Those improvements should survive.
- T569 deliberately suppresses repeated identical assistant bodies. A two-question/two-“Yes.” fixture loses the second answer in React, but this follows that later target's broad rule. It needs a narrower product decision, not a disguised parity patch.

## What the audit covers

At the inventory freeze, all 789 targets were exported through Bullseye; subsequent audit follow-ups are recorded separately. The full acceptance of all 302 existing census entries was assessed. A separate reviewer screened all 487 other target names and read the plausible UI requirements and their retirement/supersession history. An independent pass through legacy modules, event handlers, persistence keys, and request paths supplied additional behaviors.

The resulting map includes those 302 entries plus 27 additional assessments. These overlap and vary greatly in size; **they are not units from which to calculate a completion percentage**. Some are backend or test obligations, some are deliberate changes, and some are still uncertain. The full original acceptance is retained even when the relevant frontend clause is narrower.

This is stronger than the existing census boundary. The census mostly selects tagged, achieved targets before 22 August, while real legacy behavior also exists in untagged, set-aside, still-open, and later targets. For example:

- T253 was set aside because the owner said selected-repository frontier behavior worked. That was not permission to remove it.
- T389's ledger-scoped target identity and T8.2's worker dashboard were absent from the census.
- T370 has working legacy keyboard-cycle code despite an open target and a separate browser-interception uncertainty.
- The old implementation continued changing after the cutoff, including Cursor monthly usage and the Grok model roster.

The [completeness review](react-fidelity-2026-09-05/completeness.md) records these cases and legitimate exclusions. Two further reviewers challenged source claims and scope decisions. Their corrections include removing old-sidebar-only requirements, distinguishing inherited shortcomings from migration losses, correcting the exact ledger boundary, and downgrading raw-protocol defects that the current indexed mux bypasses.

The map is complete against this declared inventory. It does **not** claim exhaustive runtime verification of every browser, provider, long-history geometry case, or latent legacy capability. Those uncertainties are named rather than counted as passing.

## What has succeeded

The shared frontend architecture is genuine. Main and sidebar mount `AgentInteraction`, `AgentTranscript`, the same bubble renderer, and `UserRequest`, with agent identity and density as inputs. They use `useConversation` and named transcript channels through one mux client. React owns reconciliation and uses a real virtualizer. Those are substantial improvements over a main widget and separately maintained inspector.

Other retained capabilities include basic owner/assistant rendering, image attachment chips and upload plumbing, working Home/End behavior, shared Tab/slash navigation, substantial folding and clipping logic, parent-based fleet ordering, many model-label rules, and rich target acceptance/dependency content. Fresh targeted probes agree on several ordinary stream joins, owner boundaries, fleet sorting, and model parsing. The map labels these as code-supported or bounded tested behavior, not blanket acceptance.

The backend also has a real shared foundation: named agents, common registry/provider abstractions, agent-addressed APIs, multiplexed conversations, and common delivery entry points. The new architecture is worth finishing.

However, sharing a component does not automatically make its behavior identical. A fresh same-payload test supplied an explicitly owner-marked `<user_query>` message with literal asterisks. Main kept it literal; compact/sidebar promoted it to Markdown because of a body-shape override. The sidebar also omits the main conversation's phase strip; the backend currently supplies root phase metadata to named channels, so this needs agent-specific state before simply exposing the same strip. These are gaps **within the desired shared model**, not arguments for restoring the old sidebar.

## The consequential gaps

| Area | Finding | Practical consequence |
|---|---|---|
| Sending and editing | Queue, force-send, history, dictation, and layout helpers exist but are largely disconnected from `UserRequest`. Escape has no general interrupt path. | Ordinary follow-ups do not have the old visible queue/edit/recovery experience. An internal mux buffer or server queue is not an owner-manageable queue. |
| Aside workflows | Prefix routing, target/idea capture, creation lifecycle, and historical browsing are incomplete or absent. | `aside:` goes verbatim to the root conversation instead of opening the intended side conversation. This concerns routing and lifecycle, not old sidebar rendering. |
| Fleet control | Model badges have no migration menu; portfolios and pushed refresh are absent; some phase/identity chrome is wrong. | The owner cannot perform several real fleet-management actions through controls that appear to exist. A `dead_unmaterialized` conversation can receive a green dot when `running=true`. |
| Repository context | Frontier and graph queries stay on the primary ledger; the table receives no ledger context for engagement matching. | Selecting another product does not rebind the displayed work. Identical target numbers in different repositories can share an incorrect engagement overlay. The Stop backend defaults to the primary ledger; this is not evidence of a global cross-repository kill. |
| Graphs and rich content | React feeds the server's joined multi-graph source to one Mermaid render and ignores `diagrams[]`. Highlighting, image lightbox, design-choice cards, envelope headers, and some graph actions are absent. | Some important content is unusable, and several copied visual styles describe controls that are never mounted. Individual graph components render successfully; their joined source fails. |
| Transcript continuity | Soft reconnect resets displayed frames; indexed multi-block fixtures expose extra/duplicated prose; ISO timestamps disappear. | A superficially working transcript still has continuity and fidelity gaps. The raw reused-stream-ID issue is separately marked compatibility-only/uncertain, because current indexed mux bypasses that reducer path. |
| Observability | Coach is a placeholder; the worker dashboard is permanently empty; browser decision telemetry and owner-interaction degraded chrome are missing. | Existing backend data and recovery decisions are less visible to the owner. Missing a warning mechanism is not evidence that its underlying failure is currently occurring. |
| Distribution | The binary embeds the old web UI; React relies on an on-disk `ui/dist`. The release build packages binaries, not that React tree. | Deleting `web/` before providing an equivalent standalone React distribution would remove the existing embedded fallback. Standard-port startup already requires the dist. |

Rich target cards are not wholly missing: acceptance and dependency content survive. The specific losses are metadata and incomplete cache invalidation. Similarly, action-error notices for frontier Play/Stop were already inadequate in legacy. They are useful product hardening, not lost parity. Optional dollar chrome can be explicitly retired under its existing requirements; it need not be restored merely for visual similarity.

## What fresh execution establishes

The browser comparison used unchanged builds from the frozen commit, identical synthetic data, and real Chromium rendering. Transport and APIs were mocked; these are **not live-agent journeys**. Legacy CDN libraries were supplied from the installed build dependencies; highlighting and terminal libraries were not exercised in these browser cases.

| Input or action | Old main/product behavior | React behavior |
|---|---|---|
| Five lines in the composer | Grows to 121px | Stays at 42px with 123px scroll content |
| Alt+Up after an owner turn | Recalls that request | No recall |
| Enter while working | Shows follow-up with Send now / Cancel; no immediate submit | Sends on mux; visible queue remains empty |
| Open Coach with one filed judgment | Fetches and displays judgment, evidence, reason and target | “Coach judgments port next.”; no dispositions request |
| Open Closed | Opens archive panel | No action |
| Submit `aside: review this separately.` | Calls aside-creation endpoint | Sends the prefix verbatim to `transcript:jevons` |
| Owner-interaction degraded sample | Visible standing warning | No banner |

The aside fixture proves **routing**, not successful agent creation/delivery: its deliberately minimal response does not attest delivery. React also correctly displayed a supplied classified provider failure. The old fixture altered some characters in that text, so that case is not claimed as exact old/new parity or a live provider failure test.

Browser evidence: `GATE parity-browser-v2 exit=0 GREEN id=3e83c0cf`, clean `8dd6e1694bbf`. The earlier five-case comparison was `744e90f5`. The paired screenshots visibly show two conversation bubbles, the old queue strip and populated Coach list, versus React's empty queue and Coach placeholder. The large empty transcript area is expected for this deliberately two-turn fixture; it says nothing about a full workday's history packing.

Other fresh observations:

- `e2e94019`: ten fleet/helper/SSR comparisons, including incorrect dead-conversation dot, omitted labels, stale card cache, and missing ledger scope.
- `e5296991`: the actual Mermaid renderer accepts individual graph components and rejects the joined source React supplies.
- `4b339add`: transcript differential fixtures, including indexed multi-block behavior. The fixture results are separated from runtime applicability.
- `70e3a105`: identical main/sidebar owner provenance differs; identical Mermaid source renders again after rematerialization; ISO time strings are lost.
- `2a493c32`: isolated backend seams described below.

These gates often assert the presence of a difference. **Their green result confirms the finding, not React parity.** Existing focused frontend suites also ran: 113 passed/1 skipped for transcript, 37/12 for composer/aside, and 106/15 for fleet/frontier. The individual assertions and their limits are recorded in the map. No number of those passing helpers substitutes for a mounted interaction or a live-provider journey.

## The backend: shared names, remaining differences in meaning

The owner's impression is substantially right: this is not two wholly separate agent systems. But unification is incomplete precisely where user-visible semantics cross an adapter.

Two isolated tests reproduced consequential differences at committed HEAD:

1. A provider-originated incoming **worker user event** is appended to the worker JSONL journal, but an early return skips the SQLite/mux update. Once that cache exists, refreshing the React conversation still omits the incoming turn. Its assistant reply appears, and an HTTP-originated owner-turn control appears. This is a conversation-history consistency failure, not an intended worker role distinction.
2. The named HTTP/mux send path branches for the overseer before reaching the common delivery hook. It can return `sent` while the owner's payload is still queued. A worker control returns `queued`; the common MCP overseer entry can return the more honest `delivered_unconfirmed` when it has no receiver evidence. The API family is shared, but these outcomes do not mean the same thing.

The backend review also identifies code-supported recovery and concurrency risks, distinct from those reproduced seams. Current uncommitted T621 changes add provider-history reconstruction to the legacy inspect path; they do not repair the mux journal gap and can further separate what legacy inspect and React read. T622's use of Claudia's migration operation is a meaningful unification improvement, but it is not included in the frozen-HEAD test claim.

The right next boundary is a common conversation service: named agent in; canonical history, subscription, send result and recovery state out. Owner provenance must be carried explicitly. Role policy can wrap that service; it should not silently change delivery evidence or which incoming messages exist in a conversation. The reproduced shared-semantics gaps are recorded as 🎯T627; durable outstanding-delivery work remains 🎯T623. The detailed [backend assessment](react-fidelity-2026-09-05/backend-unification.md) traces these paths and test limitations.

## Why the earlier parity effort could look finished

The repository contains a valuable amount of ported behavior, but several gates measure an intermediate asset:

- A helper has a correct queue decision, while the composer never calls it.
- A test verifies that Bedrock belongs to Anthropic's model family, while the actual selector omits the Bedrock mark.
- A target is assigned to a journey stub, while no meaningful journey assertion runs for it.
- An “implementation-only” exception discards a still-visible outcome, such as a current hover card or stable long-history layout.
- A snapshot fixture applies special row offsets. It can be useful for a fixture, but it cannot prove that ordinary product history has correct geometry.

This is why another pass counting target IDs, helper ports, or green tests would not have answered the owner's question. The useful unit is **a requirement expressed as an interaction on the actual composed product**, with an expected result independent of the implementation being tested.

## How to retire the old UX without abandoning its useful contract

I would keep React as the only development direction and preserve its shared component/backend design. I would not resurrect the old sidebar or undertake another wholesale rewrite.

The practical route is six coherent pieces of work:

1. **Unify conversation semantics.** Fix worker journal→mux consistency and delivery-result meaning; make identical input/provenance behave identically across main/sidebar. Protect those with one parameterized contract across named agent roles.
2. **Finish the shared composer.** Connect queue/history/force-send/interrupt, growth, dictation and pending/draft recovery. Exercise actual controls in both densities, including reload and failed submission.
3. **Finish fleet context and control.** Restore selected-ledger binding, scoped engagement, model migration, portfolio grouping, truthful state and actionable failure handling. These matter more to orchestration than cosmetic matching.
4. **Complete side-conversation lifecycle.** Route prefixes and target/idea capture through named agents; show the opening turn and phase; support dismissal/history. Keep the new shared transcript widget.
5. **Restore remaining content and observability.** Graph packs, highlighting, media viewer, design choices, Coach, relevant diagnostics, and remaining geometry/performance obligations. Decide explicitly which older dashboard or decorative features should be retired.
6. **Close the release and verification boundary.** Package the React assets for a standalone install, replace legacy-dependent test entry points, and run representative live-provider journeys through the canonical UI. Keep helper tests as supporting evidence.

**Two running user interfaces are not necessary to retain a reference.** Once this map and the explicit changes are accepted as the contract, the old implementation can live at a pinned revision and in differential test fixtures. The old sidecar need not remain a competing daily UI. Removing its service and deleting embedded/source dependencies are separate operations: the latter still needs the distribution/test work above.

That gives a concrete quick measure: freeze the old reference and stop discovering requirements by repeatedly browsing it. Drive the React work from this map, with named uncertainties and explicit retirement decisions. Full fidelity should no longer mean “whatever an agent remembers about the other UI.” No product code, running service, push, or release was changed by this assessment.
