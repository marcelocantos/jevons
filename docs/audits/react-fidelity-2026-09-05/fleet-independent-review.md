# Independent challenge of the fleet/frontier review

Reviewed `fleet-frontier.json` and `coverage-challenge.md` against the frozen executable source at `8dd6e1694bbf`. This was a bounded independent source challenge, not a second full 71-target audit or a development-instance test. No product code, target ledger or running daemon was changed.

## Outcome

The consequential graph and selected-ledger findings are supported. Two corrections were applied directly to `fleet-frontier.json`, `fleet-frontier.md` and `coverage-challenge.md`: the legacy Play/Stop error behavior was overstated, and the lost-ledger mechanism named the wrong field/boundary. The broader missing feature claims sampled below have concrete missing production wiring. Existing `code_supported` labels should continue to be read narrowly, not as product acceptance.

## Graph payload: independently confirmed

The claim is stronger than a guess from absent component code:

1. `internal/server/frontier.go:622–637` defines the actual API response with `available`, `diagrams[]`, `pack`, `mermaid`, counts and error metadata. `handleFrontierGraph` at line1234 JSON-encodes that response directly.
2. `frontier.go:1009–1020` builds top-level `mermaid` by joining multiple complete diagram definitions with pack markers. Its explicit contract says this is pin/copy source, not valid as one Mermaid render. The independently inspected JS exporter at `web/scripts/frontier_table.js:588` has the same format.
3. `web/scripts/frontier_table.js:716–755` selects `mode:'pack'` whenever there is more than one diagram unless `preferPrimary===true`. The stale `index.html` comments describing single-primary do not control execution. `index.html:4216` passes `preferPack`, so it does not defeat this final default; lines4250–4260 send each component to the pack renderer.
4. `ui/src/components/MermaidVizPanel.tsx:18–40` fetches the API, extracts only `j.mermaid || j.source`, wraps that result in one Mermaid fence and drops `diagrams`, `pack`, `available` and `error`. The renderer at `ui/src/conversation/mermaidPaint.ts:87–102` attempts that code block as a single Mermaid diagram, retaining the source block on failure.

A fresh independent check used the installed Mermaid package, two valid flowcharts, the legacy join/normalization/open-plan helpers and a payload shaped like the real response. Both individual definitions parsed. The joined pack returned `false` from `mermaid.parse(...,{suppressErrors:true})`. The final old open plan returned `pack` even when the call supplied `preferPack:false`, confirming the stale-comment trap.

`GATE parity-fleet-independent-graph exit=0 GREEN id=e5296991`

Evidence: `transcript-probes/fleet-challenge.test.ts`, `fleet-challenge-results.json`, `fleet-challenge-output.txt`. This green proves the parser incompatibility and final old selection rule. It is not a screenshot, dimension/legibility check, or successful React graph-render verdict. The parent browser run may supply those separate properties. The one-diagram case is not condemned by this experiment: pack comment lines around a single diagram can still be valid Mermaid. The defect is the multi-definition payload and omitted component dispatch.

The target chronology also agrees: T276/T277 were parked under T280's primary-only decision, but T280's later attestation and T294's acceptance/context explicitly record why that result was false-fixed and why component packing returned. `coverage-challenge.md` correctly follows this later decision instead of the older set-aside reason.

## Correction 1: Play/Stop errors are inherited, not lost fidelity

**Before:** EXTRA-fleet-13 was `missing` and treated owner-facing kickoff/Stop failure messages as a legacy capability. T182's only listed gap was the React failure handler returning silently to idle; T198 also listed absent Stop notices as a parity gap.

**Independent source:** `web/index.html:10949–10992` catches failed kickoff, calls `jLog`, clears submitted state and restores the button. `web/index.html:11049–11059` catches Stop failure, calls `jLog`, then reenables the button. Neither paints an error to the owner. `web/scripts/jlog.js:58–80` explicitly forwards to the browser console and `/api/log`; it is not an owner-visible notice mechanism. T182's full acceptance requires tight columns and a real play send, not an error toast/banner. Its historical 409 “no silent drop” note concerns the backend response, not a demonstrated UI error surface.

**After:** EXTRA-fleet-13 is `not_applicable` to the parity deficit, with an explicit inherited/product-hardening disposition. The failed callback remains a real new-product concern; it is not concealed or marked fixed. T182 is `code_supported` for the implemented send/control path; its inherited failure concern is separated. T198 remains `partial` because the independent ledger-scope loss is real, with Stop notices moved out of its parity gaps.

## Correction 2: preserve the precise ledger boundary

**Before:** Parts of the review said “App discards/strips ledger_key/cwd.” That wording suggests the agent's ledger metadata is dropped and names a nonexistent agent JSON field.

**Independent source:** `internal/server/chat.go:854–859` declares `Ledger string` as JSON `ledger`; `chat.go:980` resolves it from workdir. `ui/src/App.tsx:145–151` preserves `a.ledger` in the mapped agent. `ui/src/frontier/play.ts:91–106` can filter engagement using this field when given a desired `ledgerKey`.

The real omission remains: `App.tsx:162–171` fixes the frontier query key and `/api/frontier` URL regardless of selected workdir; `toFrontierRows` returns rows and drops the response-level ledger/cwd metadata; `App.tsx:389` does not supply `FrontierTable.ledgerKey`. The graph query is similarly unscoped. Thus the engagement helper receives no desired ledger and aggregates equal target IDs across all returned agents. Stop sends no cwd and the server defaults to primary, so the correct consequence is mismatched display/action scope, not a global cross-repository kill.

The review now names this exact boundary in the JSON, narrative and completeness challenge. This is a wording/mechanism correction, not a dismissal of the high-priority repository-selection defect.

## Other spotchecks and confidence limits

- **Fleet push refresh (T82): agree.** App uses 5s agent and 8s frontier polling with no matching push subscription/query invalidation. The mux does not invalidate those queries. This is production wiring evidence, not merely an absent helper-name search.
- **Portfolio grouping (T200): agree.** AgentTree can render a preexisting purpose=portfolio node, but App never obtains the declared portfolio membership feed or creates those folder nodes. A capable row renderer does not wire grouping.
- **Model menu (T285.2/T506): agree.** `AgentTree.tsx:60–96` renders a button labelled “Select provider and model” with no action handler; the row click therefore selects the agent. Current model parsing is a separate surviving capability. This is functional omission, not pixel taste.
- **Rich-card cache (EXTRA4/T485): agree.** `frontier/table.ts:299–313` fingerprints neither value/cost nor extra fields, while the card renderer at lines234–278 displays them. The observable staleness is a valid parity concern even if cache architecture is excluded.
- **Ghost-session dot (T412): agree.** The server can intentionally emit `running:true` with `status:dead_unmaterialized` (`chat.go:955–967`), and React's `agentDotState` returns running before considering that status (`rowModel.ts:178–184`). This is not a hypothetical impossible fixture.
- **Natural target sort (T199): narrow agreement.** The shared server sorts numerically; React preserves API order within the later free/engaged partitions. Do not interpret this as a freshly verified full-graph ordering property.
- **T72 source-of-truth criterion needs narrower labeling.** The review already identifies invented fallback `jevons`/`jevons-po` rows in EXTRA8. Its per-criterion `code_supported` label for “single source of truth” should explicitly inherit that changed/residual disposition. The normal nonempty feed mapping is supported; the empty/unavailable state is not solely registry truth.
- **T266 context truth remains partial in practice.** The helper is wired and can derive a repo from explicit text/agents, but the author notes missing ambient frontier scope. The broad “owner never guesses the ledger” acceptance should not be read as established by its simple helper fixture. Link the already-confirmed selected-ledger omission rather than inflating helper success into a product guarantee.
- **Sidebar carveout: agree with the revised reference.** Shared `AgentInteraction`/`AgentTranscript` is the desired architecture. Independent agent selection, repository scope and disappearance handling remain workflow concerns; old inspector polling, separate rendering and scroll mechanics are not requirements to resurrect.

I did not independently repeat every badge-color, row-geometry, hover-retention or billing-label test, or verify all487 excluded target names. Those remain the original review's scoped evidence and the parent's rendered-product checks. No prior author's passing claim was used as the sole support for the concrete confirmations above.
