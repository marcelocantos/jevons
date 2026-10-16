# Cleanup verification map

This is the evidence map for 🎯T1074, grounded in the [October audit](../audits/entropy-audit-2026-10-16.md). Bullseye owns acceptance and target state. A proposed check below is not evidence that it has run, and a passing component check does not certify the integrated daily-driver goal.

## Baseline preservation

The initial committed Jevons baseline is `8617fb0f83973286e51be6d343046b164fe75384`; Claudia is `11144a5205a378cc6e78f062836f1e50b4677287`. The sibling `go.work` is not part of the accepted dependency contract. Checks use `GOWORK=off`.

Private preservation captures live at `/tmp/jevons-cleanup-baseline-20261016/{jevons,claudia}/`. Each contains the binary working-tree diff against HEAD, staged diff, raw Git status, untracked-file archive and a SHA-256 manifest. Archive contents were compared with both their manifest and the unchanged working files after capture.

| Repository | Preserved pre-existing work | Untracked archive SHA-256 |
|---|---|---|
| Jevons | 83 modified tracked files; 169 untracked files/symlinks | `0c0c1661ac60dcde0005671fe27c9eb84f1f9caf5055af22b67319e7b9fffcb3` |
| Claudia | No tracked edits; 29 untracked files | `fa16af6a10388e83d556f11d12d27d3642c1f0164680645a498122742b84cb0e` |

Four nested Jevons checkouts under `.jevons-worktrees-jevons/` are explicitly excluded and listed in the manifest; they are separate repositories, not inputs of the parent module. Ignored build outputs are not baseline source. These captures preserve work but do not attribute individual hunks to authors or certify it. Attribution and selective integration remain open. Do not discard WIP or include it wholesale in a cleanup commit.

## Oracle classes

| Class | What it can establish | What it cannot establish |
|---|---|---|
| Fast behavioral tests | State transitions and failure behavior through real production callers with controlled clocks, transports and stores | Real provider execution, authentication or owner experience |
| Cross-component contracts | Identity, usage and lifecycle facts survive actual serializers, adapters, queues and replay | Vendor capacity or untested provider-specific behavior |
| Static boundary rules | A forbidden dependency or retired entry point is absent/rejected | That the replacement is exercised or works |
| Documentation lint | Declared schema, command and documentation references agree | Product behavior or completion |
| Pristine build/full hermetic gate | A named committed revision and locked dependencies build and pass the declared runtime suites without sibling WIP | Live provider behavior or the final integrated mission |
| Bounded live journeys | A thin owner path through the actual daemon, backend and persistence reaches a machine-checked outcome | Every provider, every mission, or an unlimited unattended soak |

Missing credentials or socket/sandbox prerequisites are explicit outages. Skips, dirty runs and expected-negative fixtures are never reported as a product pass. Each changed provider path needs its bounded live evidence. The final goal consumes an exact Jevons/Claudia pair, not unrelated green results from each repository.

## Findings and decisive checks

The tests in the third column are required checks, not existing-test claims unless the evidence column explicitly names a run.

| Finding / target | Production authority | Decisive check and standing gate | Evidence / remaining work |
|---|---|---|---|
| OCT-01 / 🎯T952 | Jevons notice producer and durable delivery admission | Actual producer across at least 20 minutes, restart and changing age text: stable intentional hold submits zero model requests; actionable transition creates a bounded durable episode. Isolate journey verifies status visibility. | Incident occurrence IDs and October spool specimens are in the audit. Existing backoff WIP is preserved; new zero-submission behavior is unverified. |
| OCT-02 / 🎯T1074.2 | Sidecar request usage, Claudia normalization, Jevons attribution | Real producer → wire → broker/direct adapter → Event/Agent usage → fleet accounting; duplicate replay counts once, summaries/retries count, unavailable usage remains unknown. Contract suite plus bounded live comparison. | Not implemented or verified by this cleanup yet. Whole-snapshot sums are not the oracle. |
| OCT-03 / 🎯T1074.5 | Jevons episode allowance, Claudia admission, sidecar final request/tool checks | Exhaust count/token/rate/expiry test profiles via every automatic source; restart, tool heartbeat and no-op output cannot reset the allowance. Pause blocks new work across roles without losing in-flight obligations. Production-path contracts and isolate journeys. | No configured production budget is inferred. Existing limiter helpers alone do not close this finding. |
| OCT-04 / 🎯T766 | Jevons observation, generation claim and automatic decision authority | Concurrent ticks, stale facts and restart produce at most one action per generation; old automatic actuators are unreachable. Synchronous owner actions retain shared admission checks. Behavioral tests plus static boundary checks. | Existing achieved observation/report prerequisites remain; remaining authority/queue work is open. |
| OCT-05 / 🎯T1074.3 | Host logical RequestID, Claudia durable execution receipt, sidecar attempt/turn identity | Same text under distinct IDs, replay under one ID, A-active/B-steer/C-later, crash between acceptance and acknowledgment, reconnect and stale predecessor. Querying never executes or settles another request. Contract suite and changed-backend journeys. | Transcript substring evidence cannot close a request. Steering misbinding was an audit hypothesis; reproduce or disprove it. |
| OCT-06 / 🎯T1074.4 | Common sidecar execution policy | The same read-only, protected-root, symlink and shell corpus through ordinary tools and Cursor native callbacks, including after switching. Assert filesystem/process outcomes. | Existing normal-tool tests are insufficient for Cursor. A changed tool/backend contract requires bounded live coverage as well. |
| OCT-07 / 🎯T1074.1 | Build gate and pre-push attestation | Actual runner and hook refuse dirty starts, untracked inputs, changed end state, conflicting runs and obsolete profiles; committed planted Bun failure must fail the standing gate. Full committed gate includes sidecar behavior. | Claudia cleanup commit `d218789fb404731fb69ea14ad28ec5be6383d7d1`: focused clean gate `1151c9f3` and full clean gate `c160d997` passed. Integrated locally as `afbff6f`, with identical tree `18d2d339f7449a5b3ec26d46a3f4a49e2b25b7c2`. Baseline WIP attribution and the complete paired cleanup gate remain open. |
| OCT-08 / 🎯T1074.7 | Claudia canonical history, incremental diagnostics, explicit context policy | Freeze incident-derived corpus and measurements before implementation. Actual writer/reader replay preserves facts; ordinary per-turn bytes/cost are bounded independently of history size, except declared checkpoints. Separate context and consumed-token measurements. | No benchmark or cleanup claim yet. Compression alone does not prove bounded serialization. |
| OCT-09 / 🎯T1074.6, 🎯T1055 | Generic Claudia contracts; Jevons host adapter | Neutral consumer has no implicit Jevons endpoint/state access; explicit host wiring works; retired CLI surfaces fail with named unsupported errors and no reachable legacy launcher. Contract tests, pristine build and static dependency rules. | Preserve the owner's authorized Task withdrawal; a new Task engine is not a prerequisite. |
| OCT-10 / 🎯T1074.8 | Production behavior, architecture documentation and gate ownership | Mutations at real authority/adapter call sites fail behavioral checks; wording-only edits affect documentation lint rather than unrelated behavioral gates. Final architecture review names removed paths and evidence for each row. | This map separates evidence classes; it does not claim the existing gates have already been reorganized. |

## Gate slice evidence and limits

Claudia's `d218789` gate change was independently reviewed. Review found and the change fixed an inherited `MAKEFILES` false-green and publication before cleanup; both are accounted for in the final implementation. The clean focused gate ran `go test -race -count=1 ./internal/ownergate ./internal/prepush` and reported `GREEN id=1151c9f3`. It exercises the production runner and pre-push script, not only a modeled attestation.

The sidecar prerequisite probe correctly refused the restricted execution environment with `OUTAGE` for unavailable local sockets. An unrestricted diagnostic run passed 126 sidecar tests, including macOS sandbox and sidecar restart behavior. That diagnostic run preceded the commit and is not the committed full-gate evidence.

The subsequent committed `make gate` passed with `GREEN id=c160d997`, in 16m43.5s, on clean `d218789fb404`. It ran the locked sidecar suite (126 tests), Go race suite, vet, stability surface checks and mutation evidence, including the prove-teeth pass. Twelve command/helper packages declared no tests; this result does not invent coverage for those packages. The attested profile is `full-runtime-v1`. The local integration commit `afbff6f` has the exact same tree as the tested commit. Unrelated untracked Claudia files remain preserved; no attestation was copied into that non-clean checkout.

This slice changes build verification, not agent execution behavior; provider-spending journeys are not its correctness oracle. The actual Go runner, Make recipes, Bun suite, filesystem locks and pre-push script are its production path. The rest of the cleanup still requires the live journeys in the table and final goal. No development fleet restart or release is authorized by these gate results.

## Intentional-hold slice and journey exception

The first 🎯T952 slice suppresses model notifications for intentional parked/owner-blocked/provider-blocked backlogs before the periodic sweep's delivery and uncertain-attempt branches. The existing durable queue and fleet intent remain the authorities; no new backoff store is added. Reaped-seat routing and first recovery alerts remain available when fleet intent permits them. A fleet-wide park defers those too until lifted.

`TestT952IntentionalHoldIsStatusAcrossRestart` uses the October `1181ef220253` incident's 77h6m41s parked-backlog shape. It drives the real sweep across 20 simulated minutes with a new server and reopened stores halfway through, checks zero overseer/worker submissions, preserves queue identity and uncertainty, and reads the obligation through the reconciliation tool handler. It also verifies the first actionable alert after lifting a hold. The new regression failed on unchanged production code (`RED 02dedb7f`); that is expected-negative evidence, not a passing gate. `ReportRecoveredBacklog` has only test callers in this revision: its filtering test protects a dormant helper and is not claimed as shipped startup coverage.

Journey exception for this narrow slice: no provider wire changes, and the property is **absence of dispatch** under durable intent. Production sweep calls with a recording delivery boundary, real filesystem stores and a controlled clock directly decide it, including restart and the first permitted transition. Spending a model turn cannot strengthen a zero-dispatch assertion. This exception does not replace the final goal's bounded integrated pilot or close 🎯T952: durable finite admission for actionable episodes and receipt reconciliation remain open, as does broader UI projection coverage.
