# Jevons, Claudia and sidecar: architecture and token-burn audit

Assessment: 16 October 2026. Design-review record: **🎯T1073**, parked for owner discussion. This report recommends work; it does not authorize restarting the fleet or executing the remediation plan.

## Executive assessment

There is a coherent architecture worth preserving. The React component reset succeeded; durable delivery and seat observations are substantially better than in September; provider switching has become a much simpler Claudia operation. A rewrite would discard valuable progress.

However, the system still lacks a single dependable answer to **what authorizes the next model request, how its result is identified, and how its resource consumption is accounted for**. Several independently reasonable recovery and delivery mechanisms compose badly. This is the principal architectural problem behind the tinkering, and it has observable consequences rather than merely aesthetic symptoms.

The strongest incident evidence is the overseer spending model turns acknowledging notices that the daemon already knew required no action. A retained conversation sample contains 48 very short assistant responses, including **44 exact “No action needed.” replies**, reporting **29,213,798 processed tokens**. Of these, 27,411,518 were cache reads and 1,682,042 cache writes. These are provider-reported processing counters, **not newly generated tokens, an invoice, or a measurement of subscription quota depletion**. Nevertheless, they demonstrate repeated expensive processing for no useful decision.

The other consequential discovery is that sidecar usage exists inside snapshots but disappears from the normal Claudia usage events and Jevons accounting projection. The system's economic controls therefore cannot reliably observe its current default execution engine. This must be corrected before unattended operation can be evaluated honestly.

Recommended direction: stabilize the evidence and execution contracts, suppress deterministic no-op traffic before it reaches a model, give each state transition one owner, then delete the replaced mechanisms. Do not begin with package renaming, a new supervisor, another coaching agent, or another set of prompt instructions.

## Scope, snapshots and limitations

| Surface | Observed snapshot |
|---|---|
| Jevons checkout | Branch `steer-modes-stop-guards-seat-stops`, HEAD `2a179ceb781a221333063c66074db0094d3e5873`, 14 October 17:30 AEDT. At entry: 83 modified tracked paths, 3,039 insertions / 492 deletions, plus 116 untracked entries. |
| Claudia checkout | `master`, HEAD `11144a5205a378cc6e78f062836f1e50b4677287`, 14 October; v0.53.0 release commit `3e21967`. No tracked modifications; 18 untracked entries. |
| Claudia sidecar | `claudia/sidecar`, part of the Claudia repository/release. Bun/TypeScript with pinned `@oh-my-pi/pi-agent-core`, `pi-ai` and `pi-catalog` 18.2.11. It is a component, not a third repository. |
| Dependency resolution | Jevons pins Claudia v0.53.0; the parent `go.work` also overlays local Claudia and Vellum. Ordinary local tests and clean consumers can therefore use different code. |
| Development runtime | Read-only request to `127.0.0.1:13705/health` was refused. No daemon or fleet was started, restarted or resumed. Exact incident executable identity was not established. |
| History | Git ancestry and selected changed-path history, Bullseye state, Mnemo recent activity/search, stored event logs and sidecar snapshots. Mnemo results contain test sessions/tool-output contamination and are not a complete census of fleet turns. |

The local checkout is not local `master`. Its ancestry is 315 commits unique versus two unique to the locally recorded `origin/master`; squash integration makes those counts unsuitable as an “unshipped changes” estimate. No fetch or remote-state certification was performed. Committed code, pre-existing WIP and stored runtime evidence are distinguished throughout.

This was a broad architecture review with deep tracing of request delivery, recovery, usage, tool enforcement and verification. It was not an exhaustive line-by-line review, dependency vulnerability scan, penetration test, fresh full-suite certification or unattended soak. No paid model calls, keychain prompts or product agents were initiated. The only authored repository changes were this report and its design-review ledger record; Vitest also refreshed its ignored local results cache.

## What changed recently

HEAD ancestry contains 482 Jevons commits and 114 Claudia commits dated from 1 October through the inspected heads. Counts indicate change volume, not quality or effort.

| Period | Evidence and interpretation |
|---|---|
| Early October | 🎯T766 was explicitly sharpened to reject renamed-but-still-active control mechanisms. The owner already identified the correct structural problem: one seat truth, one decision loop, typed facts and one queue meaning. Those acceptance criteria remain useful. |
| 10–12 October | Repeated work on pause/hold enforcement, receipt settlement, provider placement, finish classification and clean verification. `9c4e78a9` improves report-obligation discharge. `55ab847c` caches ledger parsing; `929633f8` avoids decoding unrelated seats' spool records. These address real mechanisms, but also show repair concentrated around control and evidence boundaries. |
| 13 October | Claudia 🎯T195 replaces provider migration/handover with in-place switching. `5b666a7` alone removes 3,829 lines while adding 976 across 61 files. Jevons follows through `2815211c`; `9bcac2b6` removes leftovers. This genuinely simplifies the design. |
| 14 October | Jevons removes more legacy TUI/CLI machinery and consolidates plan bands on Claudia's pressure classification (`0745eefd`, `81243137`, `a78431e7`). Both inspected heads were last updated that afternoon. Retained overseer history still demonstrates repeated no-action turns that day. |
| 15 October | Event logs continue through 18:26 AEDT; some sentinel work is deferred under a reported “189 live seats of 100” capacity condition. That is the governor's classified seat count, not proof that 189 models were simultaneously generating. No conclusion about paid activity follows from log activity alone. |

The latest work is therefore a mixture of real simplification and incomplete consolidation. It is inaccurate to characterize everything as uncontrolled growth, but equally inaccurate to call the cleanup finished.

## Measured structure and dimension vector

Using identical definitions on Git snapshots—non-test Go files, including comments—the two main Jevons coordination packages changed as follows:

| Package | September baseline `8dd6e169` | Current HEAD `2a179ceb` |
|---|---:|---:|
| `internal/mcpserver` | 26,880 lines / 94 files | 41,105 lines / 153 files |
| `internal/server` | 16,841 lines / 63 files | 20,399 lines / 74 files |

Together they grew from 43,721 to 61,504 lines, approximately 41%. Size is not the defect; the significant observation is that lifecycle, recovery and receipt decisions still accumulate in these transport-facing hubs. Splitting files has not made control authority correspondingly smaller.

A separate working-tree census of tracked Go/TypeScript files, excluding testdata, vendor and node_modules, found 174,772 non-test lines and 164,677 test lines in Jevons, and 51,924 / 59,618 in Claudia. “Non-test” includes command and verification tooling; these are not pure runtime-size estimates. There are 4,047 Go test functions in Jevons and 1,372 in Claudia. Verification investment is substantial; these counts do not demonstrate behavior or coverage.

| Dimension | State | Evidence / movement since September |
|---|---|---|
| Architecture topology | Concern | Clear outer boundaries and simpler switching; orchestration authority remains dispersed inside large hubs. |
| Redundancy / sources of truth | Critical | Usage disappears between layers; request fate is reconstructed from text; provider tool paths enforce different policies. Durable queue claims and seatstate improve the baseline. |
| Change amplification | Critical | Pause and lifecycle changes still require multiple actuator patches; 83 tracked WIP paths obscure the integration baseline. |
| Local code quality | Concern | Many clear pure helpers and explicit errors coexist with growing incident-specific prose classifiers. Findings concern their composition, not uniformly poor coding. |
| Correctness / verification | Critical | Sidecar unit suites are outside the standing gate; false-clean Claudia attestation is reproduced; no clean integrated baseline was certified. |
| Security / dependencies | Concern | Cursor's native executor misses common sandbox enforcement. Dependencies are pinned; comprehensive vulnerability/security coverage remains unknown. |
| Build / release / operations | Critical | React assets are now embedded and tracked, improving September. Metering and automatic-work admission still fail the unattended-operation bar. |
| Documentation / governance | Concern | Architecture and hygiene declarations lag the default sidecar runtime. Existing consolidation targets already describe much of the right destination. |

These labels are a dimension vector, not a scalar score. “Critical” refers to confidence in unattended daily use, not a claim that every operation currently fails.

## Observed architecture and recommended ownership

The main path is React → Jevons HTTP/WebSocket and fleet tools → Claudia SDK/registry/broker → Bun sidecar → provider. Sidecar tool calls either execute local coding tools or travel through host-tool routing. The broker owns credentials and process lifetime; the sidecar owns model-visible conversation, context maintenance and provider execution. Jevons owns the product and multi-agent work model.

That outline is sensible. The seams need to become authoritative:

| Concern | Jevons | Claudia public runtime / broker | Claudia sidecar |
|---|---|---|---|
| Work and roles | Owner intent, missions, hierarchy, targets, inter-agent obligations, acceptance, communication policy | Generic named seats and host-supplied policy; no PO/worker business semantics | Executes a seat; no fleet hierarchy or target interpretation |
| Request lifecycle | Durable inter-agent outbox, host-minted logical request IDs and mission correlation | Durable preservation of host request IDs, idempotent acceptance, queryable receipts, cancellation/pause generations | Preserves identity on queued inputs and provider attempts; emits execution facts |
| Provider choice | Fleet priorities, owner preferences, resource allocation | Shared provider availability/auth facts, eligibility/placement mechanism and in-place switching | Applies model changes and context maintenance |
| Continuation | Decides what work is still warranted and supplies bounded authority | Enforces per-seat admission/continuation authority for every consumer | Checks authorization before each actual provider request, including retries and summaries |
| Resource accounting | Attributes usage to missions; fleet budgets, scheduling and useful owner display | Durable normalized per-request usage and admission state | Produces actual usage facts, including cache dimensions, compaction and failed attempts where available |
| Tools and credentials | Supplies capabilities and Jevons MCP integration | Holds credentials and declarative grants; brokers host tools | One execution-policy implementation for all provider tool adapters |
| Conversation/history | Owner-facing journal and indexed display projection | Stable conversation/event API and replay identity | Canonical model conversation, context compaction and persistence |

The last row permits multiple stores with different purposes. A UI journal and a model context store are not inherently redundant. The problem is consumers independently inferring execution truth from those stores.

Keep the sidecar an implementation boundary inside Claudia. Do not create a third competing lifecycle API. Likewise, do not move fleet missions into Claudia merely because it owns generic continuation. A host can authorize a bounded continuation; Claudia should not infer from prose whether a Bullseye target deserves another worker.

## Findings

### OCT-01 — Known no-action notices still authorize expensive overseer turns

**P1. Observed runtime waste; source-supported explanation. Dimensions: operations, sources of truth, change amplification.**

The latest retained `jevons` turn-end snapshot in the 14 October spool is timestamped `2026-10-14T05:45:26.821Z`. Its 143 messages include the 48 short responses described above, timestamped from 13 October 23:33 to 14 October 15:56 AEDT. Repeated inputs for occurrence `1181ef220253` describe the same intentionally parked `jv-t1056-auto` backlog at ages 77h6m41s, 77h9m2s and 77h9m38s. Each explicitly says “No recovery action.” Each receives another model response. Occurrence `9edf22e9b2a6` shows the same pattern for another parked seat.

Committed [send_backlog.go](/Users/marcelo/work/github.com/marcelocantos/jevons/internal/mcpserver/send_backlog.go:463) offers intentional-park notices every sweep. [T810 coalescing](/Users/marcelo/work/github.com/marcelocantos/jevons/internal/mcpserver/t810_notice_coalesce.go:183) correctly normalizes ages into material bands, but deliberately reoffers unconfirmed notices for ten minutes. The lower [notify replay ledger](/Users/marcelo/work/github.com/marcelocantos/jevons/internal/mcpserver/notify_replay.go:229) hashes exact text, so changing age defeats its identical-batch suppression. [Overseer delivery](/Users/marcelo/work/github.com/marcelocantos/jevons/internal/mcpserver/deliver_by_name.go:684) classifies with `FlightUnknown`; a live-stream `SessionEvent` alone cannot establish the accepted outcome under that predicate. A model can answer while delivery remains “unconfirmed.”

**Counterevidence:** the material coalescer eventually suppresses repeats; its grace does not slide forever. Persisted records for both occurrences show unconfirmed state and more than 10,000 suppressed repeats. An untracked `t952_stall_notice.go` plus dirty call-site changes already mitigate this September-discovered pattern with backoff. That work is not in inspected HEAD, and compiled incident provenance is unknown. Even its proposed periodic reminders still spend turns on known intentional holds.

**Remediation and home:** Jevons records expected holds in status without waking a model. Only a meaningful actionable transition creates an inter-agent request. Receipt uncertainty queries the same Claudia request rather than creating another age-varied prompt. A cheap model classifier is unnecessary when the producer already knows the event is informational.

**Verification/ratchet:** drive the actual notice producer and delivery seam through changing clock values, unknown receipts and restarts. Stable intentional hold: zero model submissions; actionable transition: one durable request. A bounded isolate journey verifies owner visibility and receipt behavior. Reuse 🎯T952 and 🎯T1053/🎯T1054, but enforce the rule before prompting, not merely in the overseer's reply style.

### OCT-02 — The default execution engine drops usage at both consumer boundaries

**P1. Observed contract defect, independently supported by stored records. Dimensions: correctness, operations, sources of truth.**

[Sidecar seat](/Users/marcelo/work/github.com/marcelocantos/claudia/sidecar/seat.ts:374) emits a full snapshot at turn end. [Go event pumping](/Users/marcelo/work/github.com/marcelocantos/claudia/omp_agent.go:533) does not populate `Event.Usage`; [Agent.Usage](/Users/marcelo/work/github.com/marcelocantos/claudia/agent.go:1944) only accumulates that field. Jevons's alternate [spool projection](/Users/marcelo/work/github.com/marcelocantos/jevons/internal/spool/jsonl.go:29) discards snapshot usage and only fabricates zero-token usage when a top-level `CostUSD` exists. Claudia's [spool schema](/Users/marcelo/work/github.com/marcelocantos/claudia/sidecar/spool.ts:8) emits no such field. The inspected 13–14 October files contained zero top-level `costUSD` records.

**Counterevidence:** usage is retained in nested assistant messages, so forensic reconstruction is possible. Existing cost readers and fixtures exist; they do not establish that the real producer feeds them. Context size, per-request consumption, provider plan remaining and API-equivalent cost are distinct facts. Fixing one does not measure all four.

**Remediation and home:** sidecar emits per-provider-request usage, including summary/retry calls; Claudia durably normalizes and deduplicates it; Jevons consumes those facts and attributes mission/fleet cost. Unknown usage must remain unknown. Do not sum whole snapshots, and do not preserve provider-transcript parsing as the main accounting path. Extend existing Claudia 🎯T2.4 rather than inventing a second accounting service.

**Verification/ratchet:** producer-to-`Agent.Usage`-to-Jevons accounting contract test, replayed across reconnect without double counting; separate compaction usage; failed/interrupted-request unknowns; one bounded real-provider check of the reported dimensions. A token-budget control cannot be accepted while this contract is absent.

### OCT-03 — Recovery rate limits do not bound authorized automatic work

**P1. Observed containment gap; not established as the dominant incident trigger. Dimensions: operations, architecture.**

Production installs `ImpatienceEngine` in [main.go](/Users/marcelo/work/github.com/marcelocantos/jevons/cmd/jevonsd/main.go:1329). [idle_nudge.go](/Users/marcelo/work/github.com/marcelocantos/jevons/internal/mcpserver/idle_nudge.go:1377) then bypasses the fallback path that enforces durable `MaxNudges`. The installed [engine](/Users/marcelo/work/github.com/marcelocantos/jevons/internal/mcpserver/impatience.go:130) records nudge counts but does not admit based on them; its in-memory ladder repeats on timed rungs and resets on process restart. Claudia's [goal continuation](/Users/marcelo/work/github.com/marcelocantos/claudia/goal.go:183) bounds consecutive tool-free idle turns, but any tool activity resets that count. This is not a durable mission-progress or token bound.

Observed configuration has `daily_token_budget: 0` (unset); subscription dollar accounting is intentionally observational. **Counterevidence:** capacity deferral, intent holds, idle suppression, bounded auth replay and compaction retry limits all exist. Recent logs show only four impatience decisions across the selected 12–14 October days; blaming that ladder for the measured no-action burst would be unsupported.

**Remediation and home:** Jevons grants a durable recovery/continuation allowance for a mission episode; Claudia enforces it at request admission, with sidecar checks covering internal provider requests. Allowance includes request count, token/rate constraints and expiry, and survives restart. Tool activity is not automatically progress. Rate limiting should defer new automatic requests, preserving accepted work, consistent with 🎯T777. Explicit owner pause must stop new requests across every path (🎯T966); interruption policy remains separate.

**Verification/ratchet:** drive all automatic sources, including goal continuation and compaction, through an exhausted allowance and restarts. No unauthorized new request; exactly one useful escalation; retained pending work. Use bounded production-path tests rather than a helper-only `MaxNudges` assertion.

### OCT-04 — Reconcile orders actuators but does not yet own every transition

**P1. Observed dispersed authority; overlapping-pass failure remains a risk, not a reproduced race. Dimensions: topology, change amplification, local quality.**

[Reconcile](/Users/marcelo/work/github.com/marcelocantos/jevons/internal/mcpserver/reconcile.go:16) groups operations but leaves guards in individual actuators. Cockpit convergence and [sentinel repair](/Users/marcelo/work/github.com/marcelocantos/jevons/internal/mcpserver/sentinel.go:370) can both invoke it; no pass-wide serialization or shared action claim protects the whole sequence. Recent pause changes touch direct send, queue drain, interruption, restart recovery and reattachment separately. Finish/reap decisions still depend partly on English completion/wait classifiers, with new phrases required after real premature-reaping incidents.

**Counterevidence:** `seatstate` is a real observation authority with freshness and Unknown semantics; queue claims and individual mutexes prevent some duplication. Typed envelopes already exist. The defect is not absence of all structure, but multiple ways to decide and enact the same semantic transition.

**Remediation and home:** finish 🎯T766 according to its existing semantic acceptance. Jevons has one decision path per seat/mission generation; timers and inbound events supply observations, while effectful actuators accept a claimed decision. Claudia is the sole executor of generic seat commands. Mission disposition—continuing, blocked, needs owner, report ready, superseded—is typed; model prose remains explanation. Keep overseer judgment for acceptance and quality, not for transport facts.

**Verification/ratchet:** simultaneous ticks, stale observations and pause/restart cannot create two actions for the same generation. Old actuators must be unreachable except through the authority, not merely renamed. Existing real wait/finish specimens become tests of typed protocol adaptation. Do not weaken the 🎯T766 acceptance to a file/string census.

### OCT-05 — Delivery still guesses request identity from transcript content

**P1. Observed ambiguous receipt mechanism; an additional steering issue is an inference. Dimensions: correctness, boundaries.**

[Held-backlog settlement](/Users/marcelo/work/github.com/marcelocantos/jevons/internal/mcpserver/send_backlog.go:213) scans evidence from byte zero with a text needle. Its own comments acknowledge that an earlier identical payload may satisfy a later uncertain send. Jevons also retains provider-specific transcript receipt knowledge. Meanwhile Claudia queues logical RequestIDs FIFO, but [the OMP prompt wire](/Users/marcelo/work/github.com/marcelocantos/claudia/omp_agent.go:491) does not carry that logical identity, and sidecar steering ignores its metadata. A steering request binding to a later unrelated turn is a credible inference, **not a reproduced defect in this audit**.

**Counterevidence:** durable queue claims, definitely-not-sent versus uncertain outcomes, and recent parent-report discharge are meaningful improvements. They should be retained.

**Remediation and home:** Jevons/the host mints logical request IDs; Claudia durably preserves them and provides accepted/started/completed/cancelled receipts and execution-attempt identity, propagated through sidecar queues. Jevons retains inter-agent obligations and routing, but stops interpreting provider histories to settle transport fate. Distinguish “request executed” from “mission accepted.”

**Verification/ratchet:** identical text under two IDs; duplicate replay under one ID; crash between acceptance and receipt; steering during an active turn; reconnect; stale predecessor events. Querying a receipt must neither execute the request again nor settle a different request. Extend 🎯T766.4/🎯T766.5 and Claudia's 🎯T184/🎯T188 request propagation/reconnect contracts, alongside 🎯T179 turn identity and 🎯T185 enforcement.

### OCT-06 — Cursor's second coding-tool path bypasses the common sandbox

**P1. Observed static enforcement gap; no exploitation attempted. Dimensions: security, redundancy.**

[seat.ts](/Users/marcelo/work/github.com/marcelocantos/claudia/sidecar/seat.ts:141) constructs Cursor native executors with cwd/output bounds, while normal coding tools receive `SandboxPolicy`. [cursorexec.ts](/Users/marcelo/work/github.com/marcelocantos/claudia/sidecar/cursorexec.ts:96) writes directly and runs `/bin/bash` without the shared write-root/seatbelt policy. [coding.ts](/Users/marcelo/work/github.com/marcelocantos/claudia/sidecar/coding.ts:99) separately implements read-only tool removal and filesystem/shell enforcement. Provider callbacks are not governed merely by the ordinary tool list. The two paths landed alongside sandbox work on 13 October.

**Counterevidence:** Cursor has path handling, output bounds and branch-checkout checks; normal tools have meaningful sandbox tests. Those controls do not establish provider-independent grant enforcement.

**Remediation and home:** one sidecar execution-policy gate and common primitives; provider adapters translate arguments/results. Claudia supplies declarative grants. This should precede unrestricted multi-provider resumption.

**Verification/ratchet:** run both entry paths against the same read-only, protected-root, symlink and shell corpus, including after model switches. Assert actual filesystem/process outcomes, not only the presence of a policy parameter.

### OCT-07 — The standing verification contract no longer matches the shipped runtime

**P1. Observed gate omission and reproduced attestation defect. Dimensions: correctness, build, governance.**

Claudia's [Makefile gate](/Users/marcelo/work/github.com/marcelocantos/claudia/Makefile:13) and inspected CI workflows do not run the sidecar's 126 Bun tests. Some Go tests launch Bun processes; none invokes those unit suites. The omitted suite tests the new default runtime's context, switching, auth replay and tool policies.

Independently, the unchanged [gate attestation script](/Users/marcelo/work/github.com/marcelocantos/claudia/scripts/gate-attest.sh:26) was exercised in a disposable Git fixture. Starting with tracked dirt and restoring it before completion writes `dirty=0`, despite the warning. Untracked Go source is also excluded from the dirty count. The pre-push check trusts that record. This proves a false-clean attestation condition, not that a released version used one.

**Counterevidence:** substantial Go, mutation, live-test census and TLA+ infrastructure exists. Jevons's gate correctly reported DIRTY/RED in this audit. Preserve these strengths.

**Remediation and home:** Claudia's gate includes its entire shipped engine. Gate provenance names immutable source and dependency revisions, rejects untracked source inputs and dirty-start runs, and separates concurrent runs. Establish a clean Jevons/Claudia pair with `GOWORK=off` before broad remediation. Generic test provenance belongs in build tooling, not fleet mission policy.

**Verification/ratchet:** the demonstrated false-clean fixtures must be refused; a deliberately failing sidecar test must fail the top-level gate. Do not call environmental sandbox failures green or infer full product failure from them.

### OCT-08 — Full-context diagnostic snapshots amplify storage and obscure the event contract

**P2. Observed storage shape; CPU contribution not profiled. Dimensions: operations, boundaries.**

Every sidecar turn-end includes the full agent state; [spool.ts](/Users/marcelo/work/github.com/marcelocantos/claudia/sidecar/spool.ts:97) serializes it into a global dated log. The 10 October log is 2,462,462,738 bytes; the spool directory is approximately 11 GB. A 14 October overseer snapshot contained 2,477 messages and assistant requests reporting about 840,000 context/cache tokens for seven output tokens. Later compaction reduced retained history, so context maintenance is operating.

**Counterevidence:** canonical conversation persistence and diagnostic history serve different purposes; recent per-seat indexing reduces some read amplification. Keeping a forensic checkpoint can be useful. This does not prove the spool caused a specific stall.

**Remediation and home:** sidecar/Claudia retain canonical conversation persistence and expose incremental events with occasional referenced checkpoints. Usage and receipts become first-class events, not properties extracted from repeated snapshots. Retention and context economics are explicit policies. Context-window compliance alone is not a useful economical-context target.

**Verification/ratchet:** at representative history sizes, replay reconstructs the same facts and per-turn event bytes/latency remain bounded independently of total history, apart from deliberate checkpoints. Benchmark the actual reader/writer path; compression alone does not fix repeated serialization or missing semantics.

### OCT-09 — Generic Claudia still carries Jevons defaults and a second execution architecture

**P2. Observed coupling and incomplete retirement. Dimensions: topology, redundancy, documentation.**

Claudia defaults unknown `jevons_*` tools to `localhost:13705`, spools under `~/.jevons`, and seeds/exempts Jevons MCP state. Generic routing already exists, making these removable defaults rather than fundamental requirements. Session aliases use the sidecar, while [Task dispatch](/Users/marcelo/work/github.com/marcelocantos/claudia/task.go:409) still selects native CLI implementations, preserving another set of process/auth/tool semantics.

**Counterevidence:** compatibility can justify temporary duplication. Here, Claudia 🎯T189 already records the owner's authorization to remove both seat and Task CLI transports and accept missing equivalents; 🎯T192 specifies clear failure for withdrawn Task dispatch. Do not reopen that decision or make a new Task engine an unsolicited prerequisite.

**Remediation and home:** finish 🎯T160 and the authorized 🎯T177/🎯T189/🎯T192 retirement. Host integration supplies tool registry, storage roots and explicit import locations. Unknown tools fail explicitly. Remove Jevons-specific fallback from Claudia; keep Jevons adapter code in Jevons.

**Verification/ratchet:** neutral Claudia consumer cannot contact Jevons endpoints or write Jevons state; explicit Jevons integration still works. Retired public surfaces fail clearly and no legacy launcher remains reachable.

### OCT-10 — Verification and doctrine are too tightly coupled to individual incidents

**P2. Observed maintenance cost, not a claim that all incident tests are weak. Dimensions: change amplification, local quality, documentation.**

45 of 46 tracked Jevons docratchet files contain string/regex checks; the suite also includes expensive clean-build checks. Many behavioral repairs add target-numbered prose classifiers and companion instruction ratchets. A documentation phrase can become a compatibility obligation without proving that the product follows it. The declared “current architecture” still describes Grok ACP and legacy conversation sources even as the default runtime changes to the sidecar. It does correctly document the new in-place switch; documentation drift is selective.

**Counterevidence:** real incident specimens, production call-site tests, mutation checks and schema ratchets are valuable. Untracked `guardreach` work explicitly addresses helper-only tests; it is promising WIP, not an already-landed guarantee.

**Remediation and home:** group tests around stable lifecycle, request, usage and tool-policy properties while retaining specimens. Separate documentation lint, static dependency rules, expensive pristine-build probes and live journeys. Thin MCP/HTTP handlers should call domain authorities rather than accumulate control policy. Update architecture and hygiene documents after decisions are embodied; do not create another narrative authority in parallel.

**Verification/ratchet:** mutations of the real production seams fail behavioral tests; wording-only changes do not break unrelated product gates. Dependency rules prevent transport packages from recreating lifecycle authorities. File count or a lower substring census is not acceptance.

## Healthy structure to preserve

- **Shared React interaction:** main and sidebar both mount `AgentInteraction` in [App.tsx](/Users/marcelo/work/github.com/marcelocantos/jevons/ui/src/App.tsx:359), sharing transcript and composer. Do not restore parity with the intentionally superseded old sidebar. Tracked embedded assets also remove September's external-UI artifact ambiguity.
- **Seat observations:** `seatstate` explicitly represents stale and unknown facts, drops predecessor-session facts and separates feeds from the pure fold. Build the single decision owner on it rather than introducing another status model.
- **Durable delivery:** queue claims precede submission; uncertain outcomes are distinguished from definite failure. Keep this while replacing text-derived receipts.
- **In-place switching:** Claudia owns the generic operation and preserves the conversation; Jevons supplies placement policy. Do not resurrect handover summarizer agents. Source-model compaction before switching to a smaller window is intentional and tested; rescue when that source has no capacity is a separate explicit policy limitation.
- **Context and auth guards:** sidecar persistence, compaction progress checks, bounded tool output, safe auth replay and shared plan-token recovery exist. The problem is not “no guards”; it is incomplete end-to-end accounting and admission.
- **Plan-band consolidation:** the October change removes competing classification from Jevons UI. This is a good example of resolving ownership by deleting duplicate decisions.

## Verification performed and hygiene posture

No full clean build, browser render, paid-provider gate or product journey was executed. Re-enabling a parked system to run a readiness test would have defeated the investigation's containment. These omissions are explicit residue, not green evidence.

| Check | Actual outcome |
|---|---|
| Selected Jevons envelope, sendq, seatstate and dependency-pin Go tests | Command exit 0, but **DIRTY**, gate `13f273fd`; also exit 0 with `GOWORK=off`, **DIRTY** `0cb2a6ef`. These do not attest HEAD. |
| Claudia live-test census and Go source-walk packages | Command exit 0, **DIRTY** `e68140c1`; source-walk package has no tests. No live providers were invoked. |
| Selected React tests | 17 passed, seven skipped, two failed; **RED** `a601bc12`. Failures are in an untracked WIP transcript-queue test expecting absent streaming/sealed markers. This is not proof committed HEAD is red. |
| Sidecar Bun suite with mock providers | 120 passed / six failed across 126 tests; **RED** `198b233b`. Two failures explicitly report sandbox enforcement EPERM; four store-restart tests time out. A disposable sidecar start separately confirms Unix socket listen EPERM. These six are environment-sensitive; they are neither passes nor established product defects. |
| Claudia attestation script in disposable Git fixture | Both false-clean cases reproduced using the unchanged script. No push or release attempted. |
| Static production-path review and stored-spool reconstruction | Establish the usage omission, competing tool-policy paths and actual repeated no-action responses. Do not establish complete incident expenditure or runtime executable provenance. |

The hygiene skill was applied. Jevons still has no `hygiene.yaml`; no held tiers were invented. Claudia's local declared floors resolve: correctness, security, quality, dependencies, release, build, docs, VCS and agent dimensions at tier 2; performance at tier 0. These are **pointer-validation results**, not a contradiction of the defects above. Remote governance is unknown because the validator could not contact GitHub. Its declaration still describes a tag-only Go library without uploaded binaries, despite broker/sidecar distribution, and describes the previous pre-push behavior. A posture can validate while the standing gate omits the runtime's own behavioral suite.

The next enforcement seams should be actual request/usage/tool-policy contracts, full-runtime gate coverage and immutable provenance. Further governance automation is not the first repair.

## Recommended sequence and exit criteria

1. **Establish an inspectable baseline.** Preserve/attribute existing WIP, including 🎯T952, rather than blindly discard or commit it. Select exact Jevons and Claudia revisions and a dependency mode. Fix attestation and wire sidecar tests into Claudia's gate. Use isolated implementation worktrees and one explicit integration branch. Exit: the chosen committed pair has reproducible local hermetic evidence; environmental outages are named.
2. **Close the economic failure before broad resumption.** Known intentional holds do not wake a model. Implement end-to-end usage events and durable automatic-work allowances. Fix the shared sandbox enforcement gap. Exit: stable parked fleets create zero new automatic model requests, usage reaches the product exactly once, and provider choice cannot weaken a seat's tool grant.
3. **Make the Claudia seam factual.** Durable request IDs, per-attempt facts and queryable receipts across steer, interrupt, reconnect and switch. Jevons settles obligations from these facts. Exit: crash/replay and identical-payload tests decide every receipt independently; no transcript substring determines delivery success.
4. **Finish the existing Jevons consolidation.** Follow 🎯T766's ordering and semantic criteria, reusing seatstate/sendq/envelopes. One decision owner claims actions; typed dispositions replace lifecycle prose. Retire the obsolete actuators in the same slices that move consumers. Exit: simultaneous triggers and stale facts cannot duplicate work or override an owner hold, and the old paths cannot act.
5. **Delete residual coupling and reduce data amplification.** Complete authorized CLI retirement and neutral Claudia configuration. Replace routine full-snapshot event emission with incremental facts/checkpoints; separate gate tiers; reconcile docs and hygiene. Exit: neutral consumers remain neutral and representative context/history sizes have bounded operational cost.
6. **Run a bounded daily-driver pilot.** One overseer, one PO and a small worker allocation on an isolated profile first. Exercise owner request → delegation → actual backend work → result → accepted completion, plus pause/restart, auth unavailable, provider switch and receipt uncertainty. Capture request causes and usage. Then expand intentionally. Exit criteria are useful completed work, no orphan obligations, no repeated no-action turns, and adherence to a declared resource allowance—not merely “it stayed up.”

Steps can share narrow contract work, but a large feature fanout would reproduce the integration problem. Assign ownership by seam, with one integration owner and immutable tested revisions. Do not ask the running fleet to supervise its own unrestricted cleanup while the spending boundary is under repair.

The owner decisions are limited to the amount/rate of unattended resource use and the desired balance of continuity versus economical context size. They do not block the diagnosed deterministic repairs. My recommendation is to preserve accepted work, defer additional automatic requests at the allowance boundary, and surface one concise actionable reason. Source-exhausted compaction policy should be decided explicitly rather than hidden behind another fallback loop.

**Readiness judgment:** the current evidence does not support resuming broad unattended daily-driver operation. It does support a bounded consolidation effort rather than a rewrite. A successful pilot after steps 1–4 is a meaningful return-to-use milestone; finishing every historical feature or target is not required.

## Evidence notes and comparison with September

The token reconstruction read only the 13–14 October spool files, deduplicating nested assistant usage by seat/provider/response ID, with timestamp/model/usage fallback, and retaining message dates from 10 October. Snapshots preserve older history, so this is a **retained sample**, not complete fleet accounting. It contains 1,280 overseer provider responses reporting 510,632,439 total tokens, including 495,240,552 cache reads. The 48-response no-action sample is a smaller directly inspectable subset from the final retained snapshot. Neither quantity measures newly generated tokens or actual charges. Per-request records are the required fix; forensic snapshot summation is not a production accounting design.

Local scratch evidence is under `/tmp/jevons-audit-20261016/`; gate logs under `/tmp/jevons-audit-gates/`. Raw transcripts were not copied into Git. The enduring evidence is the named source revisions, paths, reconstruction method and summarized observations here; scratch paths are not treated as permanent artifacts.

Compared with the [5 September audit](/Users/marcelo/work/github.com/marcelocantos/jevons/docs/audits/entropy-audit-2026-09-05.md):

- **Improved:** durable queue claims; React artifact/component convergence; seat observation authority; provider switching dramatically simplified; plan-band classification consolidated.
- **Persistent mechanism:** several partial authorities still infer work/lifecycle/delivery from observations and prose. WIP and verification provenance still obscure what exact product has passed.
- **Newly demonstrated at the current boundary:** snapshot usage lost from normal accounting, repeated known-no-action turns, Cursor sandbox-path divergence, sidecar gate omission and false-clean Claudia attestation.
- **Not re-certified:** September's full-suite and journey failures were not rerun and are not asserted unchanged. Historical memory-gate bypass is not repeated as current; the inspected capacity configuration has it off. Old migration/handover findings are superseded by Claudia 🎯T195.

The clean destination is already visible in the best existing code and 🎯T766. The remaining work is to make those authorities exclusive and measurable, then remove the surrounding repair machinery.
