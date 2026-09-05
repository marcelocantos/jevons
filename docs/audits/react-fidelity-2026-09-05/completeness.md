# Completeness challenge: the census is a starting set, not the denominator

## Highest-priority user supersession

The owner explicitly excludes the **old sidebar transcript** from the parity baseline. The desired sidebar is the **same component with almost identical behavior as the main transcript**. React's reset to shared `AgentInteraction`/`AgentTranscript` is a success, not a failure to preserve the old sidebar's rendering, folding, close, refresh or scroll peculiarities. Any historical sidebar targets below are authority for shared/main behavior or independent fleet workflow only; they do not require reconstructing the old inspect implementation. Fleet identity selection, product ownership, ledger routing and removal of a disappeared selected identity remain independent workflow concerns. This user instruction outranks every old acceptance clause, duplicate/supersession note and source implementation cited here.

Examined the names of all 487 targets outside the 302-row census in the 789-target export. Read the full acceptance and retirement reasons for the plausible omitted UI requirements below, and checked the relevant frozen `web/` implementation at `8dd6e1694bbf`. This is a semantic challenge to the census, not another keyword-count claim. The full fleet/frontier/ticker map separately covers its 71 assigned census rows.

## The source of the blind spot

T540.1 explicitly requires **tagged**, **achieved**, **pre-22-August** UI targets. Its meta-test may meet that declaration while missing behavior the old product actually ships. Four cases demonstrate why this is not the required completeness boundary:

1. **An achieved behavior lacks qualifying UI tags.** T8.2 is the old working dashboard, and T389 is repo-scoped target identity. Both matter on screen but are omitted.
2. **A working behavior was set aside after owner acceptance.** T253's `set_aside_reason` says: “frontier selection now works in practice; drop residual reopen. Will refile if regression returns.” That is not permission to delete selected-repo routing. The old code still implements it.
3. **An open target has shipped code and an unresolved verification residue.** T370's keyboard cycle exists in the old product, while T436 questions whether the chord reaches a page through Chrome/Safari's real OS accelerator layer. Dropping both because neither is achieved loses the working behavior rather than preserving the honest browser residue.
4. **Legacy continued changing after the cutoff.** T549, T550 and T618 affect the old and React versions after August 22. T549 was explicitly appended to this census; T550 and T618 were not. The corpus must follow code changes as well as target-date filters.

An old source implementation is evidence of a capability to compare; its target history decides whether that capability was later superseded or rejected. Neither status alone nor raw source alone can decide all cases.

## Additions and authority

| Target / behavior | Why it belongs | Actual legacy surface | Current React consequence |
|---|---|---|---|
| T8.2: worker observability | Achieved; dashboard task/status/output/token/outcome clauses are explicitly owner visible | `web/index.html:6906` fetches `/api/workers`, listens to `/api/workers/events`, paints up to 40 workers and active/done count | `App.tsx:393` permanently renders `Workers NONE YET` and an empty div; no query/SSE. Separate from durable fleet tree. |
| T253: selected-repo frontier/graph | Set aside because owner said the behavior now worked, not because behavior was unwanted | `web/index.html:11064`, `:4160`; `frontier_table.js:1610` resolves selected workdir and sends `?cwd=` | Fixed primary-repo queries; selected-PO kickoff can send the displayed primary target to another PO. |
| T389: ledger-scoped target identity | Achieved; explicit acceptance names RHS overlay alongside daemon guards | `web/index.html:10514`, `:10998`, server engagement scope | The agent API emits ledger and App preserves it. The frontier row mapper discards the response ledger/cwd metadata; App uses an unscoped query and passes no table ledgerKey. Same numbered target in another repo falsely shows engagement. Stop backend defaults to primary, so evidence is mismatched UI scope, not a global-kill claim. |
| T370 plus T436 residue: fleet keyboard cycle | T370 code shipped; current identified state is not proof of absence | `web/index.html:9690`, `web/scripts/fleet_cycle.js` | No React fleet cycling handler. Preserve T436 uncertainty about real browser OS interception; synthetic keystrokes cannot settle it. |
| T390.1: grouped boxed windows, time triangles, provider inclusion | Converging umbrella with substantive shipped implementation; early T390 wording is older | `web/scripts/plan_usage.js:367`, `:796`, `web/index.html:7010` | Helpers mostly present; compare actual grouping/triangle/unknown behavior. Do not insist on the obsolete text-chip form of T390. |
| T390.1.1: underused/locked weekly colors and explanations | Identified target but old class/color/waste calculations exist | `web/scripts/plan_usage.js:187`, `:367`; old hover formatting | React has classes and shared server band; exact quantity/explanation shown on hover needs semantic comparison. |
| T550: Cursor monthly window | Achieved after cutoff and changed legacy code | `web/scripts/plan_usage.js`, provider-duration handling | React has monthly rank/abbreviation/published duration; keep it in the executable corpus, not an accidental uncensused success. |
| T618/T619: current Grok model roster and observed model truth | Achieved September; source/provider display evolved after cutoff | `internal/server/migrate_options.go`, `grok_model.go`, `web/scripts/provider_menu.js` | Label parser handles grok-4.6; menu is absent. Shared API observation improves current model truth and should not regress to a pinned model. |
| T54: startup/provider-unavailable error in chat | Achieved; acceptance specifically says legible actionable UI error frame, not only logs | Boot / owner chat error path | Needs explicit inclusion in the transport/chrome review. Code compilation cannot prove the absent-provider UI message. |
| T137: subscription-aware dollar labels or absence | Achieved and implemented as optional ticker/worker metadata | `web/index.html:6974`, `cost_display.js` | Removing dollars is an explicitly permitted alternative. List as a conscious retirement, not an unexplained parity gap or a requirement to restore misleading spend. |
| T144: reload storm prevention | Achieved; old browser reload is an actual interaction | `/ws/reload` + debounce server | If static React intentionally does not auto-reload, name that changed development behavior. Do not blindly port legacy file-watcher mechanics. |

The first eight additions are included or cross-referenced in `fleet-frontier.json` EXTRAs. T54/T144 need the root/transcript reviewers' lifecycle context. T8.2 and T389 are the most consequential omissions beyond the original 71-row assignment.

## Legitimate exclusions and supersessions

Set-aside reasons materially change the correct answer; these should be explicit edges in the review, not rediscovered later.

| Target | Correct disposition |
|---|---|
| T158 | Duplicate of T157. Under the current user instruction, compare shared sidebar markdown to main behavior; old inspect-specific treatment is not binding. |
| T206 | Widened into T205. Under the user supersession, use main follow/free-scroll semantics in the shared component; do not revive old inspect poll/repaint mechanics. |
| T220 | Duplicate of T221. Main/shared provenance-aware rendering is the current reference; old sidebar body-shape heuristics need not survive. |
| T233.1 | **Explicit owner rejection:** collapse should be size-only, not kind-based. Do not reintroduce bootstrap-as-nugget merely because old source contains it. T480/T559 are later authority. |
| T276/T277 | Earlier set-aside reason defers multipack in favor of T280 single-primary. **Later T294 changes the default again**: final executable `resolveFrontierGraphOpenPlan` at `frontier_table.js:716` returns the full pack for multiple components. Stale `index.html` comments still say primary-only. Preserve all components and final aspect/legibility intent. |
| T290 | Duplicate of T289 smooth UI. Keep latency/jank experience under T289; do not duplicate architecture requirements. |
| T297 | Duplicate of T296 Grok logo/compact badge. |
| T303 | Duplicate of T302; owner explicitly restored styled O/S/H letters. T299's no-O interpretation is superseded. |
| T310/T373 | Duplicates of T309/T372 unified conversation widget/contract. The latest user explicitly reaffirms this direction: same component and almost identical main/sidebar behavior. React's shared architecture is successful even when older sidebar details differ. |
| T390.1.2 | **Unfinished ambition, not a React regression:** neither frozen old nor React implements the proposed HSV lerp; both paint discrete classes. Keep separate from retirement requirements. |
| T436 | Open uncertainty about browser-level interception, not a proven old cross-browser feature. Preserve or explicitly accept its residue; do not certify it with Playwright's synthetic keys. |
| T16.1 | “Dashboard” is misleading by name: acceptance is an MCP/tool table of sessions/repos/PRs. No separate old browser surface discovered; exclude from frontend parity with this reason. |
| T9/T11/T17/T29 and other voice/mobile/generative UI aspirations | Different retired or parked surfaces; they do not become React obligations merely because the names say UI. |

T549 is **already present** in the 302-row census provided to this audit. A keyword screen overlooking it is a screening weakness, but it is not a census omission here. Its context notes an evaporated T547, so matching numbers without checking content/history would also miss the acceptance lineage.

## False or incomplete exception boundaries inside the census

- **T485 “implementation-only” is too broad.** Cache strategy is implementation detail; a card continuing to show old value/cost/extra fields after fresh data is a visible failure. The paired probe `e2e94019` reproduced precisely that: old fingerprint invalidates; React's excludes those fields. Preserve the observable requirement while allowing a different cache mechanism.
- **Virtualization exceptions need an experience clause.** T56/T119.2/T119.3/T482/T484/T486 can retire exact vanilla node/layout mechanisms, but bounded browser work, scroll stability and no hangs must remain covered by their visible parents. “React owns reconciliation” is not proof of bounded rendering.
- **A backend exception is legitimate only if its UI consequence survives the adapter.** T412 illustrates the problem: the same daemon correctly supplies `status=dead_unmaterialized`, but React gives precedence to `running=true` and paints green. Shared backend does not prove adapter fidelity.
- **Behavioral skips are not taste residue.** T285.2 is skipped as pixel-identical chrome although it includes real model-migrate requests and provider eligibility. T508's test checks only Bedrock→Anthropic classification, missing the explicit provider mark. These are decidable DOM/action properties.

## Preserve improvements instead of enforcing obsolete parity

Post-cutoff targets T555/T555.1–5 and T575 replace the old status boolean/placement with a closed phase word and its correct slot. T588/T588.1/T611 give the owner a tabular usage comparison and local, minute-resolution rollover. T591/T595 refine early-window alarms; T610 makes the server's band authoritative. These have specific accepted/current targets and should remain even where the old version differs.

A source comment calls the pressure-model work T596, but the current exported T596 is a different localhost-placement target. **T610's acceptance explicitly describes the pressure model and authoritative band**, so cite that verified semantic authority rather than trusting a reused or stale number in a comment. This is another reason not to construct the gap map by joining raw ID mentions.

## Recommended completeness rule

Use the union of: (a) all final old entry points and data paths, (b) retired acceptance clauses including their supersession chain, and (c) post-cutoff owner-approved changes. Resolve each to a behavior with one current disposition: preserved, intentionally changed, genuinely absent, or unverified. Keep test/methodology/implementation claims separate. Inventory all old controls even when they have no target; inventory all retired outcome clauses even when no control names them. A suite of IDs satisfying T540.1's original narrower declaration cannot close this stronger owner request by itself.

No production changes, target mutations or development-instance operations were performed for this challenge.

## Independent source challenge (2026-09-05)

The graph supersession and payload claim were independently confirmed against the server response, the final executable old open-plan helper, the actual React loader, and the Mermaid parser (gate e5296991). The claimed selected-ledger loss remains, with a corrected mechanism: agent.ledger is preserved; frontier response scope and the table ledgerKey prop are lost. EXTRA-fleet-13 owner-facing Play/Stop error notices are not a lost old capability: both versions lack them, and the legacy logger writes only console/server logs. That row is retained as a separate product-hardening opportunity and excluded from parity deficits.
