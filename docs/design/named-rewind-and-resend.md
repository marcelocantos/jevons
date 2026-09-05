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
primary submission in recall mode. This is preparatory work, not a T562 completion
or an activation of the fix in development. Paging beyond loaded owner history,
queue traversal, and durable recall/attachment recovery remain open.

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

Another recovery reader remains to fix before enabling rewind:
`internal/mcpserver/open_intent.go:loadOpenIntentDialogueStateDB` currently falls
back to JSONL on an empty or unreadable database. The mux fix above does not
prevent that reader from recovering stale intent after restart. The existing
owner-health resend path also does not repair missing canonical journal rows or
provide durable duplicate prevention. These remain within T623/T627; the storage
slice is not a claim that all recovery paths now honor empty history.
