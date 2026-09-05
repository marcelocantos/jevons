# Independent challenge of the primary and transcript maps

Reviewed against frozen product source `8dd6e1694bbfb9ca1ac335f2c2d6ca939ce30fab`, the full exported acceptances, and the owner's 2026-09-05 clarification. This is a challenge of the audit's claims, not a product completion certificate. Changes below were applied to `primary.json` with the lead reviewer's authorization. Transcript corrections were sent to the lead reviewer for integration.

The owner explicitly excludes the old sidebar transcript as an acceptance baseline. The current contract is the same main conversation widget with almost identical behavior, allowing deliberate density differences. Independent sidebar Close/Refresh controls, old sidebar-only Markdown heuristics, and forcing sidebar Send focus back to main are therefore not requirements to restore. Aside identity, routing, lifecycle, and isolation from main remain applicable.

## Fresh observations

`GATE parity-independent-challenge exit=0 GREEN id=70e3a105`, clean checkout `8dd6e1694bbf`: three actual-source assertions passed. These assertions establish observed defects; they are not a green parity gate. Source: `composer-experiments/challenge.test.tsx`. Output: `evidence/gates/70e3a105.log`.

- An identical frame with explicit `turn_origin: owner` and `<user_query>**owner wrote this**</user_query>` becomes literal owner text on main but an agent-origin `<strong>` element on compact. `ui/src/conversation/display.ts:228` overrides explicit owner provenance from the body wrapper when `inspect` is true. This is an actual disagreement inside the shared component, rather than a reason to reproduce the old sidebar.
- Materializing the same Mermaid source twice invokes `api.render` twice. `ui/src/conversation/mermaidPaint.ts:18` caches only loading the bundle; line 89 renders every recreated fence. Vanilla preserves successful SVG by source at `web/index.html:4689`. The React “classic” look is configured, but the claimed SVG cache did not exist.
- Numeric timestamps survive `displayRows`; ISO timestamp strings are discarded. This independently confirms the transcript review's T91 finding and narrows the previously broad T308 timestamp claim.

No real browser layout, provider send, restart, or idle performance outcome is certified by these jsdom assertions.

## Corrections applied to the primary map

| Target | Correction and reason |
|---|---|
| T114 | **Partial**, not wholly excluded as doctrine. The fourth acceptance explicitly covers RHS aside appearance and absence of main-flow pollution. React shares the registry/name-addressed send component, but prefix lifecycle and main attention-envelope filtering are missing. Shared daemon clauses remain separately outside this frontend certification. T136/T250 authorize removal of the old chip wall. |
| T324 | **Partial**, separating session binding from client freshness. `App.tsx:161` polls every five seconds; `App.tsx:221` only changes the connection flag on mux reconnect. There is no `agents_changed` subscriber or reconnect invalidation. Event-driven model-badge refresh cannot be excluded as a server-only concern. |
| T484 | **Partial**: missing successful SVG reuse, classic look source-supported, settled Firefox performance uncertain. Removed the unsupported statement that React has the required Mermaid cache. |
| T142 | **Uncertain** owner outcome, with coalesced replay architecture source-supported. Mux window opening and `convomux.go:797/924` are new transport paths, so retained old replay code cannot waive first-paint/reconnect performance. The specific old helper is replaceable; a cheap replay remains required. |
| T222 | **Partial**, with shared duplicate-filing policy separated from the actual Frontier handler. `FrontierTable.tsx:143` invokes the kickoff guard and engaged rows become Stop. Set-aside/achieved rows still offer Play, but a blocked click invokes optional `onNotice`; `App.tsx:389` supplies no handler. The required clear owner error is therefore absent. |
| T56 | **Partial**. Bounded virtual item architecture does not prove preserved replay/coalescing/scroll position, and measurable smoothness improvement is explicitly uncertain. |
| T119.2 | Retained source support for bounded measurement; marked the named thousands-of-rows/page-up execution clause **uncertain**. |
| T119.3 | **Partial**. The removed vanilla `children` ReferenceError is not applicable; current bounded DOM and historical collapse/anchor behavior need their own runtime checks. |
| T308 | **Partial**, preserving the real shared-constructor success. Numeric times and common time/title markup exist, ISO timestamps are lost, and there is no 30-second clock subscription/timer—labels update only on incidental renders. Added the explicit-owner same-payload main/sidebar discrepancy under the new owner contract. |
| T309.2 | Kept common name-addressed API architecture source-supported, but split unexecuted Go coverage and imperfect event normalization from that claim. The lead reviewer's separately requested backend investigation may further revise it. |

T105.2, T105.4, and T493 remain verification-method rows rather than independent runtime features, but now explicitly retain their retirement-oracle obligations. “Not a pixel” is not permission to delete the prefix, collapse, or visible-transcript tests when retiring the old reference.

T53's standalone React distribution gap is correctly retained. T140 correctly distinguishes connected/phase chrome from absent browser lifecycle logging. T354 now has direct paired-browser evidence of the Coach placeholder; T355 correctly preserves client health/recovery duties rather than treating its entire contract as shared daemon behavior. I found no basis to weaken those findings. T128.2's logging-only empty-reason scope exception is reasonable.

## Transcript-map challenges sent for integration

- **T223:** T504 authorizes a real owner turn to become a chronological barrier. That changes the old one-body-across-owner-insert interpretation. It does not revoke clear stream identity or correct current mux ordering. Mark the superseded clause changed, retain identity/normalization obligations, and do not infer from the stale title that all ordinary Enter queuing was revoked; T113 separately still applies.
- **T242 / T479 and other equal-text losses:** T569 explicitly introduces global exact-body suppression, including later repeated acknowledgments. This is an authorized semantic change with side effects, not an accidental port omission. A row whose only failing comparison is that rule should be changed or separately annotated, rather than adding it to the missing-parity count. T479's distinct-stream fixture also fails the old reference; retain that acceptance/reference conflict visibly.
- **T381:** The direct owner-marked payload result above is the relevant provenance failure. Missing syntax highlighting or link decoration belongs to those specific formatting requirements; it should not be used as the principal explanation of T381 without its acceptance demanding those features. Wrap/overflow still needs browser evidence.
- **EXTRA-transcript-3:** The repeated sealed stream ID observation is a real raw-reducer difference, but current indexed mux frames follow `applyWindowBody` (`reduce.ts:221`) instead of that raw join path. Mark current product applicability uncertain/compatibility-only until an indexed counterpart is reproduced. A synthetic fixture against a fallback path must not become the headline daily-path claim.

These challenges preserve the useful progress: the React conversation component really is shared, and many common transformations exist. The central risk is integration and evidence—disconnected helpers, omitted adapters, and green tests on different paths—not the fact that the old sidebar looks different.
