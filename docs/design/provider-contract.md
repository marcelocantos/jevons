# Provider Contract (design)

**Status:** draft for owner sign-off (🎯T27.1). The design below is the
class-3 human gate: whether this contract is *good* is a one-time
accept/reject. Everything downstream (config/persistence, registry,
MCP-client, feed ingestion, UI producer, mnemo-as-provider) is verified
by the conformance suite in §9, not by prose.

Companion to [../charter.md](../charter.md) (who Jevons is and what it
may decide) and [../vision-v2.md](../vision-v2.md) (what Jevons
becomes). This document specifies **how an external tool contributes to
Jevons** without jevonsd carrying any per-tool code.

## 1. Purpose & scope

Jevons is the ecosystem aggregation hub. External tools — mnemo, doit,
bullseye, ytt, … — register as **providers** and contribute up to three
capabilities over one contract:

- **feeds** — durable, replayable event streams (the fleet/evidence
  lane of the charter);
- **ui** — a declarative view surface in the ViewNode vocabulary,
  composed with other providers into one Jevons UI;
- **mcp** — the provider's own MCP tools, re-exported into jevonsd's
  `/mcp` surface.

A provider declares which of the three it offers; none is mandatory. A
feed-only provider (a sensor) and a tools-only provider (a pure
capability) are both valid.

**The load-bearing non-goal:** jevonsd carries **no per-provider code**.
Adding mnemo, or a tool that doesn't exist yet, is a registration — not
a jevonsd patch. This is the property the conformance suite (§9)
mechanically defends.

### Corrected lineage
The feed capability is the charter's event/evidence lane. An earlier
charter draft placed that lane "in mcpbridge"; that was a conflation of
mcpbridge's unrelated `_filter` query param with a durable event
substrate (see the charter §Architecture correction). **mcpbridge is not
part of this contract.** Feed durability is a provider obligation;
jevonsd aggregates.

## 2. Principles (inherited from the charter)

1. **Providers own durability; Jevons never remembers.** A feed's
   history lives in the owning service. jevonsd persists only per-feed
   **cursors** and a **last-known aggregated model**, both
   reconstructable by replaying feeds from 0.
2. **Loop-safety.** Content a provider *relays* (e.g. mnemo surfacing
   Jevons's own decision log) must never be re-ingested as new. Every
   feed event carries an `origin` provider id; jevonsd drops events whose
   `origin` is itself or already-seen (§5.4).
3. **Egress stays opt-in.** A provider that reaches external networks
   declares it in its manifest; jevonsd surfaces the capability and
   never grants it implicitly.
4. **doit gates actuation, not observation.** Feeds and UI are
   read/attention paths and are ungated. A provider MCP tool that
   *acts* is subject to doit like any other executed capability.
5. **Transport-agnostic.** A provider is either **launched** by jevonsd
   or **connected** to already-running; the contract is identical either
   way (§3).

## 3. Lifecycle & transport

A provider is defined by a registry entry (persisted per 🎯T27.2). Two
modes, one handshake:

- **launch** — jevonsd's generic supervisor (🎯T27.3; *not* claudia,
  which is Claude-hardwired) starts the provider from an argv, owns its
  stdio/lifecycle, restarts per policy.
- **connect** — the provider is already running (its own daemon);
  jevonsd dials its endpoint. Registration may be pushed by the provider
  (`POST /api/providers`) or configured out-of-band.

Either way the first exchange is **describe** (§4). Transport for the
control/feed channel is the existing JSON WebSocket fabric jevonsd
already speaks (`internal/server`), extended with the frames below —
no new socket type. MCP tools use their own endpoint (§7).

Reconciliation: on startup and on registry change, jevonsd reconciles
the desired provider set against live connections (start missing
launch-mode, dial missing connect-mode, drop removed) — 🎯T27.3.

## 4. Manifest — `describe`

The provider answers `describe` with:

```jsonc
{
  "id": "mnemo",                 // stable, unique; namespaces everything
  "version": "0.34.0",           // provider's own version
  "contract": "1",               // provider-contract major this speaks
  "capabilities": {
    "feeds": [
      { "name": "health",  "schema": "mnemo.health.v1",  "replay": true },
      { "name": "activity","schema": "mnemo.activity.v1", "replay": true }
    ],
    "ui":   [
      { "surface": "mnemo.status", "title": "mnemo", "feeds": ["health"] }
    ],
    "mcp":  { "endpoint": "http://127.0.0.1:7702/mcp", "tool_prefix": "mnemo_" }
  },
  "egress": false                // declares external-network reach
}
```

`contract` is the contract major version the provider speaks; jevonsd
refuses a major it doesn't implement (forward-compat is not assumed).
All ids, tool names, feed names, and UI surfaces are namespaced by
`id` so two providers never collide.

## 5. Capability: feeds (the event/evidence lane)

### 5.1 Shape
A feed is an **append-only, monotonically sequence-numbered** stream of
JSON events. Each event:

```jsonc
{ "feed": "health", "seq": 4213, "ts": "2026-07-04T…Z",
  "origin": "mnemo", "kind": "degraded", "data": { … } }
```

`seq` is per-feed, gap-free, assigned by the provider. `origin` is the
provider that *authored* the event (for loop-safety; a relayed event
keeps its original author).

### 5.2 Cursors & replay
jevonsd subscribes with a cursor: `{"subscribe":"health","from":<seq>}`.
`from: 0` requests full replay; `from: <last+1>` resumes. A provider
advertising `"replay": true` MUST serve history from any `seq` it has
retained; a non-replay feed (a pure live sensor) serves only new events
and jevonsd treats gaps as expected. jevonsd persists the last acked
`seq` per feed so a restart resumes without loss and without
re-ingesting.

### 5.3 Aggregated model
jevonsd folds feed events into a **last-known aggregated model** (🎯T27.5)
broadcast to clients. The model is derived state — never a source of
truth. It is fully reconstructable by replaying all `replay:true` feeds
from 0, which is the operational meaning of *Jevons never remembers*.

### 5.4 Loop-safety (decidable)
jevonsd drops any inbound event where `origin` equals the id of the
provider jevonsd itself relays *to*, or an `(origin, feed, seq)` already
folded. This makes "mnemo surfaces Jevons's decision log, Jevons folds
mnemo's feed" cycle-free by construction. The conformance suite asserts
a relayed event is not re-emitted.

## 6. Capability: ui (declarative surface)

A provider contributes one or more **surfaces** rendered natively by the
existing client renderer (`ios/Jevon/Models/ViewNode.swift`,
`ServerView.swift`, `LuaRuntime.swift` — 🎯T9/T11). **Not web, not
served HTML.**

- A surface is a `ViewNode` tree (`{type, id, props, children}` +
  `ViewProps`), produced either as a static tree or by a Lua view
  function the provider ships, evaluated against that provider's feed
  state.
- jevonsd's **server-side UI producer** (🎯T27.6; the piece deleted mid
  T9-pivot, rebuilt with multi-provider composition) composes each
  provider's surface into the aggregate Jevons UI. Composition is by
  surface, not by pixel: providers own their panel; Jevons owns layout.
- User interactions flow back as `ActionMessage` routed to the owning
  provider (by surface namespace); `ViewMessage`/`DismissMessage` as
  today.
- Data flow is one-directional and reactive: **feed event → aggregated
  model → surface re-render**. A provider never pushes a view
  imperatively; it pushes feed state and the surface derives.

## 7. Capability: mcp (tool re-export)

The provider declares an MCP `endpoint`. jevonsd runs an **MCP client**
(🎯T27.4; jevonsd is MCP-*producer*-only today) that connects, lists the
provider's tools, and re-exports them into jevonsd's own `/mcp` surface
under `tool_prefix`. Calls are proxied through; results pass back
unaltered. The provider's tools thereby become available to the Jevons
overseer agent and any MCP client of jevonsd, with no per-tool code.

doit gates any re-exported tool that actuates, exactly as it gates a
first-party tool (charter §2.4).

## 8. Versioning & trust

- **Contract version** is a single major integer (`contract`). Minor
  evolution is additive (new optional manifest fields, new feed kinds);
  a breaking change bumps the major and jevonsd may run multiple majors
  side by side during migration.
- **Trust.** A launched provider is code jevonsd executes; a connected
  provider is a peer daemon. Both are *trusted to describe themselves
  honestly* — the contract is cooperative, not adversarial. The
  adversarial boundary is doit (actuation) and egress opt-in, not the
  feed/UI paths.

## 9. Conformance (the oracle — 🎯T27.1 items 2–3)

The contract ships with an **executable spec**, not just this prose:

- **Reference mock provider** (`cmd/mockprovider` or `internal/provider/mock`)
  that declares **exactly one** replay feed, **one** ViewNode surface,
  and **one** MCP tool, all with known fixed content.
- **`go test` conformance suite** that registers the mock against a test
  jevonsd and asserts, through jevonsd's public surfaces:
  1. the feed's events appear in the aggregated model, in order, and
     resume from a cursor across a simulated restart with no gap and no
     re-ingest;
  2. the surface renders into the composed UI and an `ActionMessage`
     round-trips to the mock;
  3. the MCP tool is listed and callable through jevonsd's `/mcp` under
     its prefix.
- **No-per-provider-code check:** a test asserts (grep/build) that
  jevonsd contains no identifier naming a concrete provider (`mnemo`,
  etc.) in its provider-handling path.
- **Loop-safety & egress** each map to an assertion (§5.4; egress-off
  provider makes no external connection under test) or a recorded
  accepted risk.

Every downstream target and every real provider (mnemo 🎯T27.8 / mnemo
🎯T109) is verified by **passing this suite**, never by description.

## 10. What jevonsd must gain to host this

The contract is greenfield against today's jevonsd (MCP-producer-only,
JSON-WS hub, claudia agent registry, no config/persistence/feeds). It
fans into:

| Need | Target |
|------|--------|
| structured config + persistence (provider registry) | 🎯T27.2 |
| generic supervisor (launch) + connect + reconcile | 🎯T27.3 |
| MCP **client** aggregation into `/mcp` | 🎯T27.4 |
| feed ingestion → aggregated live model | 🎯T27.5 |
| server-side UI producer, multi-provider compose | 🎯T27.6 |
| desktop menu-bar/tray head over the aggregate | 🎯T27.7 |

## 11. Open questions for sign-off

1. **Feed transport framing** — reuse `/ws/remote`-style JSON frames, or
   a dedicated `/ws/provider` channel? (Leaning dedicated: keeps client
   and fleet lanes cleanly separated per the charter.)
2. **Feed durability floor** — is "provider retains from 0" required for
   `replay:true`, or is a bounded window (e.g. last N / T) acceptable
   with jevonsd snapshotting the fold? Bounded windows weaken the
   "reconstruct from 0" guarantee; decide the trade.
3. **UI composition authority** — does Jevons (layout owner) get to
   reorder/hide provider surfaces by policy, or is placement
   provider-declared? Affects whether the CEO can triage its own panel.
4. **MCP re-export auth** — does jevonsd pass the caller's identity to
   the provider, or call as itself? Bears on doit attribution.
5. **`_filter` envelope** — pursue as an independent mcpbridge feature,
   or drop? Orthogonal to this contract; listed only to close the
   conflation.
