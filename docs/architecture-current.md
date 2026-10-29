# Jevons — Current Architecture

This is the description of the implemented system (🎯T42), checked against
Jevons source and its published Claudia `v0.64.0` dependency on 2026-10-24.
It describes the source tree, not an assertion that the running development
service has loaded it. [charter.md](charter.md) describes governance;
[../STABILITY.md](../STABILITY.md) describes surface contracts. The
[cleanup verification map](design/cleanup-oracles-2026-10.md) records
revision-scoped evidence and remaining work under 🎯T1074. Documentation
corrections do not close that target or its open dependencies.

## Components and process ownership

Jevons coordinates coding agents under one CEO agent, the overseer
`jevons`. The human owner talks to that agent through the cockpit.
`jevonsd` is the Go infrastructure process: it serves the UI and tools,
records owner intent and delivery obligations, and applies mission policy.
The Claudia service hosts agent execution independently of the UI daemon.

```text
browser / iOS WKWebView
        │ HTTP + /ws/mux (iOS transport via pigeon)
        ▼
jevonsd (Go)
  ├─ React bundle, conversation projections, owner delivery
  ├─ MCP HTTP handlers, fleet/mission policy, admission and accounting
  ├─ Jevons-owned control-replica.db
  │       ⇅ sqlpipe changesets over Claudia broker control socket
  │  Claudia broker (separate Go process)
  │    ├─ Claudia-owned claudia-control.db
  │    └─ Bun sidecar: multiple named seats
  │         └─ bare pi-agent-core Agent + Claudia History/Maintenance
  │              ├─ provider requests and tool execution
  │              └─ Claudia SeatStore conversation journal
  └─ connected per-seat MCP path (currently inside jevonsd)
       └─ seatpairs/<seat>/{mcp.db,jevons.db}, in-process peer exchange
```

- **`cmd/jevonsd`** wires the server, registry, fleet adapter, butler,
  admission, cost guard and background controllers. `internal/server`
  owns HTTP/WebSocket handlers and backend chat-wire normalization.
- **`ui/`** is the canonical Vite/React cockpit. `ui/bundle.zip` is tracked
  and embedded; `make ui-check-bundle` checks freshness without repairing it.
  Main and sidebar conversations share `AgentInteraction` and
  `ui/src/conversation/`. `make ui-dev` is optional HMR. Development
  `:13705` serves the embedded bundle under `supervisor/jevonsd.ini`.
  There is no vanilla fallback or standing `:13706` service. Historical
  vanilla reference: `8dd6e1694bbfb9ca1ac335f2c2d6ca939ce30fab`; fidelity
  obligations remain in 🎯T540.3/T540.7.
- **[Claudia](https://github.com/marcelocantos/claudia)** supplies the Go
  registry/client, broker and Bun sidecar. Jevons pins `v0.64.0` in
  [go.mod](../go.mod). The sidecar pins `@oh-my-pi/pi-agent-core` `18.2.11`
  and `pi-ai`; Claudia owns its `History`, `Maintenance` and `SeatStore`
  adapters. The rejected `pi-coding-agent` SDK is not the production seat
  engine (🎯T1074.12). Grok ACP, tmux and native provider CLI seat transports
  are not the current execution architecture. Provider identifiers still
  select models, credentials and permissions; they do not select a second
  turn loop. Legacy transcript readers are observation/import adapters.
- **`ios/Jevon`** is a thin WKWebView client over a paired
  [pigeon](https://github.com/marcelocantos/pigeon) QUIC relay. It does not
  own orchestration or an alternative conversation implementation.

The normal broker path can keep seats alive while `jevonsd` is absent;
reattachment finds their existing identities. This does not establish that
an in-flight Jevons MCP call survives a daemon restart: its current HTTP
handler and pair caller still live inside `jevonsd`.

## Agent identity, policy and execution

A **seat** is a named registry identity with provider/model configuration,
workdir and session/generation identity. A **thread** (`internal/thread`)
records a durable conversation/workdir association; `internal/butler`
provides adoption, takeover, launch, direct and removal policy through its
`Fleet` interface. The production implementation is `fleet.Claudia` over
Claudia's registry. `jwork` is the bounded one-shot work primitive, rather
than another durable lifecycle authority.

The overseer is an addressable seat with CEO policy. Its persona comes
from structured config (`~/.jevons/config.yaml` and
`internal/config/persona.md`), supplemented by the fleet standing brief.
Roles and lineage distinguish product owners, workers and auditors.
Jevons owns that mission hierarchy and target policy; Claudia owns runtime
hosting, provider interaction, tool execution and canonical agent history.
`internal/seatstate` supplies the daemon's shared seat-state authority to
fleet, butler and server consumers. It is not a second agent engine.

Model changes use `fleet.Claudia.SwitchModel` and Claudia's registry switch
on the same seat/session. The owner surface is
`POST /api/agents/switch-model`; MCP exposes `jevons_agent_switch_model`.
Jevons applies placement policy; the runtime performs the switch and
context-fit handling. A running turn must be refused or explicitly
interrupted, rather than silently replaced with a new seat.

## Conversation and delivery

Conversation is addressed by agent name: transcript reads use
`GET /api/agents/{name}/transcript`, and sends use
`POST /api/agents/{name}/send` or MCP `jevons_agent_send`.
The React client uses indexed, windowed `transcript:{name}` channels on
`/ws/mux`, with opaque event identity for deduplication and dense indexes
for paging. `/ws/chat` and `GET /api/history` remain compatibility surfaces;
they are not another frontend runtime.

The owner chatlog is Jevons' durable display/delivery journal. Fleet
transcript readers and runtime events supply agent conversation
projections. These records must not be confused with the Claudia seat
journal that restores the model's actual conversation, including
compaction. Older provider-specific logs can still be read by discovery
and transcript adapters; their existence does not make them the current
seat execution store.

Asynchronous delivery converges on `internal/mcpserver/deliver_by_name.go`
and its `deliverByNameAs` path. Fleet recipients resolve through the
registry; the overseer resolves through the server owner-chat seam, which
journals and queues notifications. Synchronous thread direct is separate:
it subscribes before sending and gathers a reply (`internal/fleet/reply.go`).
Request identities and mission bindings are established before capable
runtime handoffs; durable send queues and receipt queries reconcile held
requests. A receipt confirms the request's runtime state, not the truth of
an agent's completion claim.

Typed fleet envelopes (🎯T509) carry lifecycle declarations; schema and
enums live in `internal/envelope`. A `finish-report` is interpreted through
its typed slots and gate evidence. Reap, stop and idle decisions must not
be inferred from completion words in English. Prose classifiers retained
for report-time diagnostics are distinct from those lifecycle decisions.

## Shared control plane: implemented transport, incomplete lifecycle wiring

The shared sqlpipe pair is separate from the per-seat tool pairs.
[`internal/cost/replica.go`](../internal/cost/replica.go) opens only Jevons'
`control-replica.db` and links to the broker control socket. Claudia opens
its own `claudia-control.db`. Reconnect performs the sqlpipe handshake;
changes committed while disconnected can then converge. The old
shared-file/in-process shared-control handshake is not this path.

Each replicated table has one originating writer; the other peer receives
its changesets:

| Tables | Originating writer | Meaning |
|---|---|---|
| `jevons_desired_seat`, `jevons_grant`, `jevons_control_intent` | Jevons | Desired configuration, grants, individual controls |
| `claudia_seat_status`, `claudia_control_outcome` | Claudia | Runtime status and control results |
| `claudia_usage_fact`, `claudia_request_receipt` | Claudia | Attributed physical usage and logical request receipts |

The published Claudia `controlplane` package implements ownership checks,
replication and reconciliation. Jevons has `AcceptDesiredSeat`,
`AcceptGrant` and `AcceptControlIntent` wrappers, but at this checkpoint
only tests call them. Production launch, send, stop and switch still use
the registry/runtime API. A working usage/receipt link therefore does not
mean all lifecycle operations use durable desired state. 🎯T1074.9 remains
open for that integration and its process/restart evidence.

`startControlServices` (`cmd/jevonsd/control_services.go`) opens the shared
replica and binds the server/MCP receipt readers before optional accounting.
The daemon owns its lifetime and closes it on shutdown. Disabling accounting
or failing to open `usage.db` therefore does not remove that link or its
receipt readers. This startup wiring publishes no lifecycle intents.
Per-seat startup catch-up still runs inside `startCostGuard`
(`cmd/jevonsd/cost.go`) under its existing accounting predicate: it executes
tool effects and has deliberately not been promoted into the independent,
read-only shared-control service. The broader per-seat recovery contract
remains open.

## Per-seat tool pairs: connected caller and recovery limits

[`internal/mcpserver/seatpair_call.go`](../internal/mcpserver/seatpair_call.go)
routes a known seat's daemon-dependent registered tool call through
`answerThroughSeatPair`. It commits an operation ID and generation,
executes catch-up, and waits for that operation's stored result. Calls
without a known seat identity/state directory remain on the direct handler.
Seat identity comes from the MCP request context; this is not a separate
standalone MCP service.

[`internal/seatpair/pair.go`](../internal/seatpair/pair.go) stores
`mcp_tool_intent` in the MCP side and `jevons_tool_outcome` in the Jevons
side. The code opens both files and exchanges sqlpipe peer frames inside
one process. Generation checks reject stale intents, and a stored outcome
prevents ordinary replay from executing the handler again.

There is a material crash window: `catchUpSeat` calls the effect before
`writeOutcome`. Arbitrary registered handlers are not transactionally
coupled to that outcome. A crash after an external effect can leave an
unresolved intent; the current implementation does not prove exactly-once
execution or reconcile that uncertainty. The idempotent internal ping
probe does not establish that property for real tools.

🎯T1074.10 remains open for independently hosted MCP/daemon endpoints,
one file owner per side, live invocation continuity while Jevons is absent,
and transactional/idempotent or explicit uncertain-effect recovery. The
required topology is N durable seat pairs (2N databases) in addition to
the one shared control pair. The connected implementation is partial
progress toward it, not evidence that the full topology is deployed.

## Usage, admission and enforcement

Claudia records physical usage and receipts from its seat execution path.
Jevons' `Collector.CatchUpReplicas` ingests replicated facts into `usage.db`
through the canonical usage contract, with durable deduplication and
rejection reporting. Request-to-mission attribution is frozen separately
by `RecordRequestMission` before handoff; mission policy does not become a
Claudia usage-table authority. Spool/history recovery and observational
subscription usage readers have distinct purposes.

Jevons has existing policy owners: `missionbound` reserves worker starts,
`fleetintent` persists fleet/seat intent, `capacity` admits ambient work,
and the cost monitor/enforcer applies spend policy through fleet actions.
These are not proof of the still-open shared mission/episode and
per-provider-request admission contract (🎯T1074.5). An observational
usage callback cannot enforce a before-request reservation.

`budget.json` distinguishes list-price accounting, subscription estimates
and disabled accounting. Subscription API-equivalent USD must not pause
or kill work as though it were billed spend. Cost enforcement includes
warn/throttle/pause/kill policy and protected-overseer behavior; usage
reporting, agent authentication and billing-reader health are separate
observations. `internal/harnessusage` is an observational cross-harness
reporter, not an alternative enforcement plane.

## Persistence and authorities

Paths below use the configured Jevons state directory (normally
`~/.jevons`) or the independently configured Claudia service roots.

| Store | Writer / authority | Purpose |
|---|---|---|
| Jevons `agents.json`, `threads.json`, role/intent/policy stores | Jevons registry client and policy components | Fleet definitions, durable threads and orchestration policy |
| Claudia history root `<seat>/<session>.jsonl` | Claudia `History` / `SeatStore` | Canonical engine entries, compaction, request/usage records needed to restore a seat |
| Broker state `claudia-control.db` | Claudia for its owned tables; replicated Jevons rows | Service facts and received desired state; not a conversation journal |
| Jevons `control-replica.db` | Jevons for its owned tables; replicated Claudia rows | Shared control-plane peer and receipt/usage projection |
| Jevons `seatpairs/<seat>/mcp.db`, `jevons.db`, `generation` | Current in-process pair implementation | Tool intent/outcome storage; independent ownership remains open |
| Jevons `usage.db`, `budget.json` | Cost store / owner policy | Deduplicated accounting, request attribution and clamp policy |
| Jevons `chatlog/<overseer>.jsonl` | Server owner-chat path | Owner conversation display and delivery journal |
| Jevons `credential.json` | Pigeon provisioning | Paired device credentials |
| WebSocket fan-out, live streams and voice FSM | In-memory | Rebuilt or reattached after restart |

Claudia's store validates seat/session identity and fails a corrupt restore;
a torn final append has a narrowly defined repair. Service databases and
UI projections do not replace this journal. Parked legacy conversations
are not silently imported or rewritten to select a new execution engine.

## Security and dormant voice

Default HTTP binding is **loopback-only** (`127.0.0.1`) unless explicitly
widened. Remote devices use a **self-hosted** pigeon relay with URL and
optional bearer token (`PIGEON_TOKEN` on pigeon; `--relay-token` /
`TERN_TOKEN` on Jevons, 🎯T156). There is no implied public free tier.
mTLS CA/QR provisioning is available but is not enabled by default.
WebSocket origin and mutating-route CSRF checks protect the owner surface.

Treat the deployment as a single trusted operator on one machine. Agent
execution is powerful; roles, lineage checks and tool policy do not make
it a hostile multi-tenant sandbox. MCP-origin attribution and execution-time
checks must be evaluated on their actual caller path, including direct
handler exceptions. Stranger-ready device onboarding remains unfinished.

Voice is de-emphasized. 🎯T37 rejected xAI Voice Agent Builder as the primary
stack; dictation uses Wispr Flow. Dormant `/ws/voice`, server voice bridge
and iOS voice code are not evidence of a completed full-duplex product.

## Verification and terms

`make test` combines Go, React and mocked-WebSocket UI checks with isolated
live journeys. `make test-journey` starts throwaway state/port and interacts
with an agent; development attachment is intentional-only. Clean bundle
and build checks validate artifacts, docratchet checks enforce declared
invariants, and paid journeys prove selected real paths. None alone
certifies independent process ownership or crash recovery. The cleanup map
holds the finding-to-evidence record; open 🎯T1074.9/.10 and the final bounded
coding pilot remain obligations, not implied successes.

| Term | Meaning |
|---|---|
| Overseer / CEO | The owner's named agent `jevons`, distinct from `jevonsd` |
| Seat | A durable named runtime identity and its configuration |
| Session / generation | The conversation identity used to bind runtime requests and reject stale control |
| Thread | Jevons' durable conversation/workdir association |
| Canonical journal | Claudia's persisted engine history, read by its restore path |
| Projection | A derived owner-facing or service view, not another execution authority |
| Shared control pair | The two process-owned databases for Jevons–Claudia state/facts |
| Per-seat pair | Seat-specific Jevons tool intent/outcome databases; standalone endpoints are still open work |
| Receipt | Runtime acceptance/terminal state of a logical request, not a completion oracle |
| Usage fact | Uniquely identified physical provider usage, separately attributed to a Jevons mission |

## Reactive subscription usage (🎯T1224)

Claudia's scheduled pollers write `claudia_plan_usage` and
`claudia_plan_usage_history` in its owned control database. The current row
separates the latest attempt and retry deadline from the last successful
observation. A failed request preserves that observation and its timestamp;
429 backoff survives a broker restart. Raw authenticated payloads are not
persisted in these tables.

The existing shared sqlpipe pair replicates those tables into Jevons' control
replica. One sqldeep subscription, `internal/planusage/projection.sql`, produces
the complete JSON for the usage bars, histories, overrides and spend notices.
Jevons retains that opaque result for initial delivery and forwards changes
through `/ws/mux`; `/api/plan-usage` reads the same bytes. Policy consumers decode
the same result and reject expired observations at decision time.

There is no Jevons vendor poller, shared JSON reading cache, raw Cursor payload
augmentation, or browser-triggered collection in this path. Opening, reconnecting
or focusing a view cannot trigger provider requests. React renders stored
last-success values neutrally during a failure and advances their displayed age
using a presentation clock. Its ordinary WebSocket reconnect restores the latest
subscribed result. Explicit authentication recovery remains a separate action.

Old history and band overrides are imported once into local tables in the
replica; the old files remain untouched for rollback. Subsequent overrides write
the replica and invalidate the same subscription. Only the named control-plane
tables participate in sqlpipe synchronization; these local presentation tables
do not acquire a second writer.

Verification is split at real boundaries: Claudia tests run scheduled recovery
and durable pacing; connected control-plane tests exercise real sqlpipe socket
reconnect and persisted replicas; the browser isolate runs the actual sqldeep
query, HTTP/WebSocket handlers and embedded React bundle. The browser isolate
uses synthetic observations and is not evidence of live provider availability.
