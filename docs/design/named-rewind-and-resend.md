# Named-agent recall and rewind

Status: implementation in progress under 🎯T562 / 🎯T627, within 🎯T540.
This document is a scout result and implementation contract, not evidence that
provider rewind works. Pigeon, iOS and deployment changes are outside its scope.

## Owner interaction

Alt/Option+Up recalls an earlier owner request in the selected conversation and
highlights that exact turn. Alt/Option+Down moves forward; moving past the newest
request restores the unfinished draft. Escape or selecting another agent cancels
the local history edit without overwriting the ordinary draft. Main and sidebar
use the same components. Agent reports and harness injections are not owner
history. Explicit wire provenance wins over legacy wrapper heuristics.

Primary submit in recall mode means **rewind and resend**, as required by T88.
An explicit “Send as new message” action appends instead. Failure to locate or
rewind the selected turn must never silently become an append. A failed operation
preserves the edited text. Rewinding chat does not undo files or external actions.

The first frontend slice implements recall within the loaded window, selected
turn highlighting, cancellation, an explicit append action, and an asynchronous
rewind callback with failure handling. **AgentInteraction does not yet bind that
callback to a provider operation.** It says that rewind is unavailable and refuses
primary submission in recall mode. Recall controls were integrated locally at
`6526637ba45a` and activated in development; this does not complete T562 or enable
provider rewind. The clean-tree gate `ac6f8813` passed the real Grok
send/reload/recall/cancel slice, without exercising rewind. A separate headless
Chromium observation of development `:13705` passed main and sidebar Alt-Up,
selected-turn highlighting, Escape draft restoration and Alt-Down draft
restoration (`36e4744e`, 2026-09-06), without sending a message. This observes
the running surface, not the dirty checkout recorded in that gate's metadata.
The preceding attempt timed out waiting for the sidebar selection (`ef58704c`);
the successful retry does not explain that intermittent failure. Native Firefox
key handling remains unverified. Paging beyond loaded owner history, queue
traversal, and durable recall/attachment recovery remain open.

## Why the old rewind handler cannot be connected

Read-only scout of committed `a9a20813570b740aab2c78fe0db63905986a697f`:

| Mechanism | Failure |
|---|---|
| `internal/server/chat.go`, `RewindOverseer` | Truncates the old JSONL before stopping the provider. `persistChatLine` stops updating that file once SQLite is active, so the truncation can target obsolete history. |
| `internal/server/statedb.go` and `internal/statedb/transcript.go` | SQLite is the canonical React journal; the old operation leaves it unchanged. |
| `internal/server/convomux.go`, `muxHub` / `muxWatch` | Cached events, absolute length, tool stamps and subscriber sent-ID sets survive. IDs use `e:<index>`; reuse after truncation requires reset of every watcher. A zero-length replacement does not currently clear a previous `journalN`. |
| `internal/statedb/transcript.go`, `ShouldImport` | An empty journal is treated as never imported. Rewinding to zero can resurrect the old JSONL on reopen. |
| `RewindOverseer` recap / acknowledgement goroutines | The seed and replacement request are separate asynchronous sends. They can race one another and queued delivery. |
| `internal/chatlog/chatlog.go`, `TruncateTurns` | Coalesces adjacent user lines. Provider-native turn counts include different boundaries, so a rendered owner-turn count is not a proven provider rewind count. |
| Named send, MCP delivery and notification/worker queue drains | No common per-agent exclusion protects the entire rewind operation. A lock on only a new HTTP handler would leave competing paths active. |
| Legacy `handleTranscriptRewind` MCP tool | Direct transcript truncation lacks coordinated process, SQLite and mux handling; it is not a reusable transaction. |

The frozen vanilla client at
`8dd6e1694bbfb9ca1ac335f2c2d6ca939ce30fab:web/index.html` computed a rewind
count from DOM bubbles and sent rewind and replacement separately. It also
silently appended if the selected bubble could not be found. Those are old
failure modes, not behavior to preserve.

## Provider strategy

T88 requires Grok/ACP redo. T52 explicitly accepts a fresh session reconstructed
from the surviving journal. “No native rewind” therefore does not mean that the
accepted Grok behavior may be dropped.

The inspected Claudia releases v0.29.0 and v0.30.0 expose native rewind for
Claude only. `Agent.Rewind` returns a successor; it does not install the successor
in the registry. Registry stop, session rewind, registration and launch are pieces
of the operation, not an atomic registry transaction.

| Provider strategy | Required evidence before enabling |
|---|---|
| Claude native rewind | Reliable mapping from the selected canonical owner row to a native provider boundary, including injected prompts and tool results; an affected-provider live check. |
| Grok fresh-session reconstruction | Canonical surviving context is seeded in defined order before the edited request. A real-provider check proves retained context survives and removed context is absent. |
| Other providers | Explicitly implemented and tested strategy. Do not edit private provider stores or claim support from a generic fallback branch. |

No native-provider path is changed by the frontend slice. The unknowns above stay
open; unit callback success is not evidence for any row in this table.

## Coordinated operation to implement

One agent-addressed request carries the selected canonical event ID, expected
conversation/session revision, replacement text and operation ID. Server-side
validation selects authoritative owner history. Changed session, stale selection
or ambiguous identity fails visibly without appending.

The operation must:

1. Exclude competing delivery and recovery for that agent, including queue drains
   and late events from its former process. Other agents continue operating.
   Do not hold `Server.mu` across provider calls.
2. Prepare the surviving prefix and recovery record before destructive changes.
   Preserve the seat's parent, purpose, model, goal and MCP configuration.
3. Apply the explicit provider strategy and deliver the replacement in defined
   order. No separate asynchronous “acknowledge the rewind” turn may overtake it.
4. Commit canonical history and the operation outcome with recoverable
   intermediate state. Provider success followed by database failure or daemon
   restart must have a defined reconciliation path.
5. Reset the selected agent's mux cache, absolute length, tool stamps and every
   subscriber's sent-ID set. Replay the surviving prefix before replacement
   events. Prevent empty-history JSONL reimport.
6. Return a correlated outcome. Queued is not receiver-confirmed delivery. An
   uncertain response must be reconcilable by operation ID without repeating the
   destructive work. Compose the T623 delivery contract rather than inventing
   a second set of send-success semantics.

Successor workers must use the existing launch/event-wiring path; the overseer
uses `AttachOverseer`. A successful process launch alone does not establish that
the new provider has received the surviving context or edited request.

## Verification map

Pinned UI checks: actual keyboard input in both shared densities; distinct IDs
for identical request text; explicit-owner wrapper classification; transcript
highlight and viewport position; Escape/Alt+Down draft restoration; no silent
append on failure; late operation results cannot modify a newly selected agent.

Standing frontend checks are `UserRequest.history.test.tsx` and the built-app
`scripts/react-ui-test/test.cjs`, both reached by the ordinary test targets. The
latter also runs its owner slice through J30 when attached to a real isolate.
Its recall/cancel result deliberately does not certify provider rewind.

Backend checks still to implement and execute:

- Populated SQLite plus stale JSONL; rewind-to-zero followed by restart.
- Two subscribed clients, reused indexes, identical prompt text and stale
  selection; owner versus agent-origin prompts and injected provider turns.
- Concurrent owner/MCP sends, queue drains and late former-process events.
- Relaunch, seed, persistence and resend failures; restart at each intermediate
  state; retry of the same operation ID cannot rewind twice.
- A canonical-UI real-provider journey with fresh request-specific evidence:
  retain an earlier fact, remove a later fact, resend an edited request, then
  verify provider context and the visible journal after reload/restart.

Still unresolved by the scout: native Claude boundary mapping, recoverable
coordination across provider state and SQLite, and reconstruction support beyond
Grok. These require implementation and focused live experiments before enabling
rewind. Neither this design nor green recall controls close those gaps.

## Canonical journal prerequisite

The storage slice keeps an initialized empty journal authoritative, including
after a database reopen and a mux `open`. Legacy imports now recheck initialization
inside the transaction that installs rows, the import receipt and a revision.
Every canonical write advances that revision; a snapshot reads revision and rows
from the same transaction. Legacy databases without revision metadata remain
readable and advance on their next write.

Owner-send durability follows the configured canonical store even when it is
empty or fails. A failed SQLite write cannot become a successful durability
claim through an obsolete JSONL append, including SQLite-only configurations.
The in-memory owner-health record retains the undurable state; durable
queue/outbox recovery remains part of T623.

Cache refresh, live folds and tool stamps share a per-agent journal lock through
persistence. Without this coordination a reload could install an older database
read over a newly arrived message, then reuse its index. Startup import is called
before serving; future runtime imports and rewind replacements must also take
this lock. It is not the provider-operation exclusion required above.

The standing Go regressions exercise both named roles through mux `open`,
database reopen, concurrent refresh and incoming requests, import failure rollback
and consistent snapshots during writes. Restoring the old mux implementation
caused the empty-history regression to fail and the concurrent refresh regression
to retain only 40 of 100 requests in the observed negative control. This is a
storage/replay exception to a new provider journey: those races are decided by
real SQLite and production journal/replay entry points; adding a model response
does not decide their interleaving. The existing isolated J30 remains the
provider-backed send/reload smoke check, not rewind evidence.

This prerequisite does not expose a revision token on the wire, perform a
compare-and-swap rewind, reset existing subscribers or enable provider rewind.
A future token must also bind the provider session and daemon generation so an
old daemon that predates revision tracking cannot validate a newer selection.

Restart recovery now treats an existing canonical database as authoritative,
including an empty history after reopen. An unreadable database yields an
explicit `unreadable_chatlog` residual and logs its cause; only an absent database
permits legacy JSONL recovery. A read-only open never creates or repairs the
database. A tail snapshot reads revision, user-turn boundary and subsequent
events within one transaction, preserving later disposition evidence and
staleness checks. The ordinary restart notification still reaches the overseer
when no recoverable intent exists.

The regression reaches `NotifyDaemonRestarted` and records what its destination
receives, alongside real SQLite empty/reopen/corruption and concurrent snapshot
tests. This is a journey exception for the recovery-source decision: the changed
mechanism decides whether to compose a mandatory resume before a provider sees
anything. No provider process contract changes, and no rewind journey is claimed.

The original reader failed all five initial canonical-history regressions
(`377ab942`). Broader race checking also exposed an independent existing test
collector race (`slogCapture` in `TestT426ALaunchInFlightIsNotADarkStream`),
reproduced on clean pre-change `15a33be1f965` by `f9df4bec`. It remains a test-net
gap relevant to T604; a focused recovery race pass must not be reported as a
whole-package race pass.

The existing owner-health resend path still does not repair missing canonical
journal rows or provide durable duplicate prevention. A missing database alone
cannot distinguish a pre-SQLite state directory from one whose entire database
was deleted; the future durable rewind operation must preserve its own recovery
receipt. These remain within T623/T627; this slice does not make provider rewind
safe or prove that all recovery paths honor empty history.

## Durable fleet-queue prerequisite

The queue drain now persists an attempt before calling the provider. The full
payload, acceptance time and stable entry ID remain present during submission.
A matching attempt token is required to release the entry; stale completions
cannot consume a later retry or recreate an explicitly discarded entry. Known
pre-write refusals return the same entry to pending. Unknown errors and outcomes
without the required evidence remain held, including across daemon death, and
are never automatically replayed. A failed claim prevents submission; a failed
resolution leaves the attempt held.

Automatic missing-agent and finished-agent cleanup uses the same claim/resolve
mechanism, so it cannot erase an in-progress delivery or clear concurrently
appended messages with a whole-queue delete. Cleanup claims the entry IDs from
its observed backlog, not a count that a newer message can silently fill.
Rerouting retains explicit unconfirmed results and failed successor enqueues.
Explicit overseer discard retains
its authority and records entry IDs and prior delivery states; it does not label
an uncertain attempt as definitely undelivered. Recovered attempts appear in
agent-list pin status and recovery notices, with reconciliation advice rather
than an instruction to start the agent to force a retry. An unusable configured
queue directory rejects acceptance instead of falling back to memory.
Current-process ownership keeps a healthy active attempt out of orphaned-attempt
pin status; that ownership is deliberately absent after a store is reopened.

A terminal received while submission is being witnessed must still wake the
next drain after resolution. A per-agent terminal generation prevents a late
confirmation from overwriting that observed end. Ordinary busy refusals remain
waiting and do not accumulate failed-delivery pins.

The standing Go tests exercise the production drain, terminate a separate test
process inside its provider submission, reopen the real queue files, and check
that the exact acceptance survives without another send. Additional cases cover
disk failures at claim and resolution, opaque errors versus known pre-write
refusals, concurrent drains and arrivals, late attempt tokens, authorized
discard, cleanup bypasses, and terminal events arriving before confirmation.
The boundary regression fails on the preceding implementation: the queue is
empty during provider submission (`9586f158`, expected negative control).

This slice deliberately preserves the existing **nil-error** successful-send
classifier. Its generic live-session event can still be local acceptance or
unrelated activity; it is not correlated receipt evidence. An errored send may
release the obligation only on payload-specific evidence. This is a bounded
durability improvement, not proof of exactly-once delivery and not completion of
T623. Provider-correlated receipts, reconciliation of uncertain attempts,
serialization with direct sends, durable overseer notifications, and recorded
terminal-disposition guarantees remain open. Finished-seat forwarding also
inherits the direct-send success contract rather than acquiring a new receipt.

Journey exception: the crash-window and filesystem-failure assertions are
decided by the production queue/drain with actual disk and process death; a
model response cannot decide those interleavings. No provider process contract
changes in this slice. A live-provider smoke run is additional compatibility
evidence, not a replacement for those tests or proof of provider rewind. The
then-existing J17 queue-bounce journey only checked that recovery was reported,
so its result could not establish receipt-correlated delivery.

Verification at `df59db58969a`: `queue-attempt-clean` (`5c3e2e16`) ran from a
fresh checkout and passed 3,191 Go tests (four skipped), the focused queue and
lifecycle race checks, and the daemon build. Local integration `f1379e2e` has
the same committed tree; unrelated shared-checkout work was preserved.

The subsequent live experiment did **not** pass: `queue-attempt-grok-smoke`
(`4adefcb4`) failed J17 after its pre-bounce reply said `queued (0 pending)`.
Although the isolate selected Grok, J17 omitted the thread's provider and the
effective worker was Claude. The journey never established a message in the
daemon-owned queue; no post-bounce recovery/re-offer evidence appeared. J5
isolation passed and the throwaway daemon was stopped. This is recorded under
T625: the journey must enforce or report its effective provider, identify a
fresh request, establish its durable queue entry before bouncing, and verify
the corresponding recovery outcome. The failed experiment is neither a live
recovery pass nor evidence that this queue fix regressed a working journey.
The development daemon has not been activated with this slice.

## Request-specific queue recovery journey

J17 now starts a worker explicitly on the selected provider. The agent runs a
bounded shell wait and creates its own ready marker. The harness then submits
one fresh follow-up through the same named-agent send API and reads the durable
queue without modifying it. Exactly one matching pending entry must exist before
restart. A queued reply, including `queued (0 pending)`, cannot substitute for
that acceptance.

The result file must be absent both before shutdown and after the old daemon
exits. After restart, the worker must perform the queued request's shell action
and write the fresh token. The named runtime launch must identify the selected
provider on both sides of the restart, and the queue must settle. This proves
the bounded post-restart action; returning at that point does not rule out a
later duplicate or establish general exactly-once semantics. The independent
review explicitly retains that limit.

The first fixture revision failed (`ec264650`): it parsed daemon text logs as
JSON and used thread-direct before a first named-agent send, whose fleet brief
changes the queued envelope. Both were corrected. The provider parser is now
tested against actual `slog.TextHandler` output and a captured-format launch;
negative controls reject another worker, wrong/mixed providers, missing queue
entries, duplicate acceptances, stale payloads, uncertain attempts, and any
pre-restart result.

In the corrected real Grok experiment, entry `20c27e5913fd` was held before the
bounce, then its agent-created result appeared after restart and its queue
settled. J17 and J5 isolation succeeded, and teardown stopped the isolate.
The combined gate (`0ecde71f`) remains **RED** because the separate existing J2
owner-chat round trip timed out. It is not a green journey-suite result. T625
remains open for J2, tools-attached/directed-work, bounce/resume, and the remaining
provider coverage; no React or rewind completion follows from this J17 slice.

The committed repair then passed `queue-journey-clean-grok` (`c71e4cd6`,
`clean@2806f863bc3d`): journey helper tests, a fresh daemon build, and J17 with a
new real Grok acceptance `65e1fff3bbe0`. The worker produced the corresponding
shell result after restart and the queue settled; teardown completed. The full
Go net also passed 3,210 tests with four skips (`c6a8b2ed`), run on the same code
before its commit; that gate truthfully records a dirty tree and does not claim
to measure the preceding HEAD alone. The earlier combined J2/J17/J5 gate stays
red; a focused green does not erase the owner-chat timeout.

## Exact owner-chat round trip

J2 and J4 use the canonical `/ws/mux` connection and `transcript:jevons`
open/send envelopes used by React. Each sends a fresh UUID request and requires
an exact, completed assistant reply after its matching owner echo, with the
selected provider observed in the named runtime launch. The reader checks full
coalesced event snapshots; it never concatenates snapshots or mixes a text
fragment with another row's terminal. Both row-ID-to-index and index-to-row-ID
identity stay stable. A row that preceded the owner request, a completed row
rewritten later, an agent-origin echo, and a truncated requested reply cannot
pass. Unrelated completed rows cannot substitute for the requested result.

J4 requires its seed exchange in the actual bounded replay, another fresh reply
through the replacement connection, and the seed exchange in the read-only
canonical SQLite store. The replay boundary is complete tail-window metadata
(`n/lo/hi/following`), not an arbitrary quiet period or an interleaved live
status update. A later live answer cannot repair a missing replayed exchange.
Each real exchange gets its own turn budget. Recorded-store checks start at the
fresh seed owner boundary, so older failures do not taint that exchange.

The typed-owner regression exposed the original false negative: the product
emits typed text blocks, but the old journey only recognized strings. Its
baseline-compatible control failed on old code (`3c2f318d`). Subsequent strict
legacy `/ws/chat` experiments passed exact replies but failed replay on both
Grok (`5ade09d8`) and Cursor (`43f7f569`), because that endpoint replays obsolete
JSONL. This migration repairs the test's product-path selection; it does not
restore the obsolete transport. The first canonical Grok J2/J4/J5 run passed
(`33a9ecfe`) before the final metadata and reverse-identity guards; it is not
completion evidence for the final reader.

These are real-provider wire journeys, not React-rendering or native-browser
key evidence. React's hydration currently accepts any meta, including partial
live status updates; the independent wire reader's complete-window boundary
does not certify that behavior. J3 remains a generic legacy cancellation smoke,
not an exact-reply or cancellation-ordering guarantee. T625 also remains open
for tools-attached/directed-work, bounce/resume, and remaining provider coverage.

## Budget-notice recursion found by the canonical journey

The clean mux candidate `13ab69ff831e` passed helper race tests and a fresh
daemon build (`e8d3237a`), then Grok J2/J4/J5 (`1a32fb33`). Cursor J2 passed,
but J4's next seed echoed at index 3 and received no reply in 90 seconds
(`8211056b`, red). Its store retained the request; its log had a preceding
budget warning and no second notification-queue enqueue. A later identical
Cursor run passed (`fcd76d52`, only ledger dirty), so retry success did not
explain or resolve the failure.

Source tracing and a deterministic regression establish a recursive lock:
`Enforcer.Act` owns the budget mutex while delivering its warning; the agent
notice reaches `SendToOverseer`, whose unconditional activity hook calls
`Enforcer.Heartbeat` and reacquires that same mutex. Subsequent owner sends
block after their journal echo and before queueing. Both the recursion and
incorrect system-as-owner activity fail on the preceding code (`add318f0`).
Attribution of the original Cursor timeout remains inferred: no goroutine dump
was captured from that isolate.

The T623.1 correction invokes the activity hook only for owner-marked turns.
It preserves normal owner contact while excluding budget, worker, and system
notices. Focused owner/notification race regressions pass (`981a5aca`). An
initial corrected-code run failed a fixture expectation because the real
enforcer prefixes the notice with `budget: `; that expectation was corrected,
without changing product formatting. Automatic owner retries still count as
owner-marked contact, and budget delivery remains synchronous; this correction
does not claim to solve either broader concern.

Journey exception for the exact warning trigger: the deterministic
cross-component test uses the real enforcer and the production notification
and heartbeat methods to decide the recursive-lock property without relying
on incidental provider spend to trigger a warning. Fresh real-provider
canonical send/reconnect journeys verify integration separately; a passing run
without a warning is not presented as proof it exercised that trigger.

The broader server race run remains red (`40f130e4`) for a separate existing
`handleRemote` disconnect-log map race. Baseline `8ef4b57e` reproduces it in
unchanged server source. It is tracked separately, not repaired in this slice.

Clean fixed revision `88658538446c` passed server, cost and journey tests,
focused owner/notification race tests and a fresh daemon build (`9eb268d3`).
Real selected/effective Grok (`38541088`) and Cursor (`e79ec4ea`) then both
passed J2, J4 and J5: exact reply, actual seed replay, a fresh reply through the
replacement socket, canonical seed persistence and completed isolate teardown.
The broader remote-provider race remains T604.1 in the shared target ledger.
The code was integrated into local master `41a51cfd` and activated through
the existing supervisor. A real development React send returned the exact
terminal reply, rendered it, acknowledged the composer and retained the reply
after reload (`c8fd9cdf`). This observes the running surface; its gate's dirty
checkout metadata does not claim to be a clean-source build test. The preceding
probe (`7b47ca77`) had a false-negative selector: assistant groups do not retain
individual event IDs in the DOM. The corrected check retains exact wire
correlation and verifies the single rendered assistant reply by its fresh text.

Visual verdict after hard reload: earlier owner and assistant turns populate
the pane, the diagnostic exchanges sit at the bottom, spacing is modest, the
composer is empty and Latest is absent. Yes, this looks like a normal chat
transcript after a hard reload. This completes T623.1's bounded defect; T623,
T625 and T540 remain open.

## Directed-worker journey fidelity (T625)

J9 previously accepted any nonempty reply and omitted the worker provider.
It now sends a fresh request to a fresh named worker, requires the exact direct
response, explicitly selects the provider and checks that worker's runtime
launch. Removal must clear both the thread list and the agent registry.
Adversarial tests exercise the whole journey against a controlled MCP peer;
they reject generic, stale, truncated and merely mentioned answers, wrong or
missing provider evidence, reused identities and surviving registry entries.
These tests are oracle regressions, not live product journeys.

The historical implementation fails the first negative controls (`e4c119cc`).
The revised journey package passes with race detection (`741a49e9`). An initial
real Grok J9 run passed (`5e7525f2`) before the final registry-removal assertion;
fresh verification of the final committed slice remains required. J6c's
independent tool effect, post-bounce action and full React owner-to-worker
coverage remain open. This change does not enable rewind or close T625.

The stricter clean J9 on `7f2aaeeb` exposed two product failures. Grok
`7c98c4b0` returned its exact UUID, but the `500` substring inside it was
classified as a backend outage. The log records the exact successful response
as the failure's raw text; this was a local classification defect, not evidence
that Grok was unavailable. Cursor `e3ae0ae1` returned its UUID split by an
inserted newline. Its saved provider transcript contains two correct chunks
without that newline and a separate empty terminal event. The fleet assembler
treated Cursor append chunks as paragraph blocks. That run also failed J5
because the J9-only fixture had made no owner turn; subsequent subsets include
J2 to establish the owner-store precondition.

T625.3 narrows numeric failure markers to whole status tokens, preserving
identifier punctuation instead of matching numeric substrings. Real status
messages retain their classes. T625.4 concatenates Cursor append chunks
verbatim, including provider-authored newlines, while retaining existing
Claude/Codex block behavior. Captured failures reproduce before the fixes
(`38e3fb2b`, `aae656f7`); affected classifier, fleet, MCP and journey suites
pass afterward (`7be65319`). An earlier Cursor regression attempt failed to
compile because its new test lacked an import (`111c3a17`); it was corrected
before obtaining the actual red reproduction. The strict J9 now deliberately
includes numeric status-shaped fragments before its fresh UUID on every run.
Clean code `6b16ef21f636` passed the full Go suite, focused classifier/fleet/
journey race tests and a fresh daemon build (`ce555b43`). Grok J9/J5 passed
(`4b83c5c6`); that invocation's J2 filter was misspelled and did not run, so it
is not owner-turn evidence. Cursor J2/J9/J5 passed (`4e445986`), including the
owner-store precondition. Both provider runs required their exact fresh reply
with embedded numeric status fragments and verified thread/registry cleanup.
Development activation and observation remain outstanding for these product
fixes; T625.3 and T625.4 are not yet claimed achieved.
