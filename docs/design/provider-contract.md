# Provider Contract (design)

**Status:** specified for 🎯T27.1 (decidable oracles green). Class-3
human sign-off of the design itself remains residual and does not gate
the executable suite.

Companion to [../charter.md](../charter.md) (who Jevons is and what it
may decide) and [../architecture-current.md](../architecture-current.md)
(what is built today). This document specifies **how an external tool
contributes to Jevons** without jevonsd carrying any per-tool code.

Executable reference: Go package `internal/provider` (mock +
conformance suite). Every downstream 🎯T27.* node and every real
provider (mnemo 🎯T27.8 / mnemo 🎯T109) is verified by **passing that
suite**, never by prose alone.

## 1. Purpose & scope

Jevons is the ecosystem aggregation hub. External tools — mnemo, doit,
bullseye, ytt, … — register as **providers** and contribute up to three
capabilities over one contract:

| Capability | What it contributes | Hub role |
|------------|---------------------|----------|
| **feeds** | Durable, replayable event streams (charter event/evidence lane) | Cursor + aggregate live model |
| **ui** | Declarative ViewNode surface (native T9/T11 renderer) | Compose surfaces into one UI |
| **mcp** | MCP tools at a declared endpoint | Client + re-export into jevonsd `/mcp` (🎯T27.4) |

A provider declares which it offers; none is mandatory alone. Feed-only
(sensor), ui-only, mcp-only, and any combination are valid. The reference
mock declares **exactly one of each** so the conformance suite can assert
all three paths.

**The load-bearing non-goal:** jevonsd carries **no per-provider code**.
Adding mnemo, or a tool that does not exist yet, is a registration — not
a jevonsd patch. The conformance suite mechanically defends this.

### Corrected lineage (mcpbridge)

The feed capability is the charter's event/evidence lane. An earlier
charter draft placed that lane "in mcpbridge"; that conflated
mcpbridge's unrelated `_filter` query param with a durable event
substrate. **mcpbridge is not part of this contract.** Feed durability
is a provider obligation; jevonsd aggregates.

### Internal MCP server is not this contract

jevonsd remains an MCP **server** for fleet control
(`jevons_agent_*`, `jwork`, …) via generated agent config. That is
internal agent-control plumbing. Providers never implement those tools;
they expose **their own** MCP endpoint for the hub to aggregate.

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
   read/attention paths and are ungated. When a UI surface triggers a
   provider action (§6), the provider actuates under its own doit
   policy — jevonsd relays intent, never executes.
5. **Transport-agnostic.** A provider is either **launched** by jevonsd
   or **connected** to already-running; the contract is identical either
   way (§3).

## 3. Lifecycle & transport

A provider is defined by a registry entry (persisted per 🎯T27.2). Two
modes, one handshake:

| Mode | Behaviour |
|------|-----------|
| **launch** | jevonsd's generic supervisor (🎯T27.3; *not* claudia, which is agent-hardwired) starts the provider from an argv, owns its stdio/lifecycle, restarts per policy. |
| **connect** | The provider is already running (its own daemon); jevonsd dials its endpoint. Registration may be pushed by the provider (`POST /api/providers`) or configured out-of-band. |

Either way the first exchange is **describe** (§4). Control/feed
transport is the existing JSON WebSocket fabric jevonsd already speaks
(`internal/server`), extended with the frames below — no new socket
family. Default channel: dedicated `/ws/provider` (keeps client and
fleet lanes separate per the charter). MCP tools use the provider's own
MCP endpoint (§7), not the feed WebSocket.

Reconciliation: on startup and on registry change, jevonsd reconciles
the desired provider set against live connections (start missing
launch-mode, dial missing connect-mode, drop removed) — 🎯T27.3.

```text
  config / POST /api/providers
            │
            ▼
     ┌──────────────┐     launch argv / connect URL
     │   registry   │────────────────────────────► provider process
     └──────┬───────┘
            │ describe
            ▼
     capabilities: feeds | ui | mcp
            │
     ┌──────┼──────────────────────┐
     ▼      ▼                      ▼
  subscribe  compose ViewNode   MCP client → /mcp re-export
  feeds      surfaces (T27.6)     (T27.4)
```

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
    "ui": [
      {
        "surface": "mnemo.status",
        "title": "mnemo",
        "feeds": ["health"],
        "root": { /* ViewNode tree — static, or omitted if Lua-derived */ }
      }
    ],
    "mcp": {
      "transport": "http",       // "http" | "stdio"
      "endpoint": "http://127.0.0.1:8741/mcp"
    }
  },
  "egress": false                // declares external-network reach
}
```

Rules:

- `contract` is the contract major version; jevonsd refuses a major it
  does not implement (forward-compat is not assumed).
- All ids, tool names, feed names, and UI surfaces are namespaced by
  `id` so two providers never collide.
- `capabilities.mcp` is optional. When present, jevonsd's MCP client
  (🎯T27.4) dials `endpoint` (http) or attaches stdio (launch-mode) and
  re-exports `tools/list` into the hub `/mcp` surface, namespaced as
  `{provider_id}__{tool_name}`.
- Empty `feeds` / `ui` arrays (or omitted keys) mean that capability is
  not offered.

### 4.1 Wire frames (JSON over provider WS)

| Direction | Frame | Notes |
|-----------|--------|--------|
| hub → provider | `{"op":"describe"}` | First exchange after connect |
| provider → hub | `{"op":"describe_ok","manifest":{…}}` | §4 body |
| hub → provider | `{"op":"subscribe","feed":"health","from":0}` | Cursor; `from:0` = full replay |
| provider → hub | `{"op":"event","event":{…}}` | §5.1 feed event |
| hub → provider | `{"op":"action","surface":"…","action":"…","value":"…"}` | UI ActionMessage relay |
| provider → hub | `{"op":"ack","seq":N}` optional | Explicit cursor ack |

MCP traffic does **not** ride this socket; it uses the declared MCP
endpoint (Streamable HTTP or stdio per MCP transport norms).

## 5. Capability: feeds (the event/evidence lane)

### 5.1 Shape

A feed is an **append-only, monotonically sequence-numbered** stream of
JSON events. Each event:

```jsonc
{
  "feed": "health",
  "seq": 4213,
  "ts": "2026-07-04T12:00:00Z",
  "origin": "mnemo",
  "kind": "degraded",
  "data": { }
}
```

- `seq` is per-feed, gap-free, assigned by the provider.
- `origin` is the provider that *authored* the event (loop-safety; a
  relayed event keeps its original author).

### 5.2 Cursors & replay

jevonsd subscribes with a cursor: `{"op":"subscribe","feed":"health","from":<seq>}`.
`from: 0` requests full replay; `from: <last+1>` resumes. A provider
advertising `"replay": true` MUST serve history from any `seq` it has
retained; a non-replay feed (pure live sensor) serves only new events
and jevonsd treats gaps as expected. jevonsd persists the last acked
`seq` per feed so a restart resumes without loss and without
re-ingesting.

**Durability floor (decided):** `replay:true` means the provider retains
a **bounded window** sufficient for operational recovery (default
expectation: last 24h or last 10_000 events, whichever is smaller). Full
`from:0` forever is not required. jevonsd may snapshot the folded model
so a cursor behind the window degrades to "snapshot + live" rather than
silent gaps. Documented residual: reconstruct-from-0 is best-effort for
long-lived feeds.

### 5.3 Aggregated model

jevonsd folds feed events into a **last-known aggregated model** (🎯T27.5)
broadcast to clients. The model is derived state — never a source of
truth. It is fully reconstructable by replaying retained history of all
`replay:true` feeds (plus any hub snapshot), which is the operational
meaning of *Jevons never remembers*.

### 5.4 Loop-safety (decidable)

jevonsd drops any inbound event where:

1. `origin` equals the hub's own provider id (if any), or
2. `(origin, feed, seq)` was already folded.

This makes "mnemo surfaces Jevons's decision log, Jevons folds mnemo's
feed" cycle-free by construction. The conformance suite asserts a
relayed self-origin event is not re-emitted into the aggregated model.

## 6. Capability: ui (declarative surface)

A provider contributes one or more **surfaces** rendered natively by the
existing client renderer:

- `ios/Jevon/Models/ViewNode.swift` — `ViewNode` / `ViewProps`
- `ios/Jevon/Views/ServerView.swift` — recursive SwiftUI map
- `ios/Jevon/Models/LuaRuntime.swift` — optional Lua screen functions

**Not web, not served HTML.** (The desktop web UI at `web/index.html` is
the chat shell; provider surfaces target the native ViewNode path.)

### 6.1 ViewNode vocabulary (pinned)

Wire shape matches Go `internal/provider.ViewNode` and Swift
`ViewNode` / `ViewProps`:

```jsonc
{
  "type": "vstack",           // node kind — see table below
  "id": "mock.root",          // optional; path ids assigned if missing
  "props": {                  // ViewProps — snake_case JSON keys
    "text": "hello",
    "spacing": 8,
    "padding": [8, 8, 8, 8],
    "action": "refresh",
    "sf_symbol": "heart",
    "bg_color": "secondarySystemBackground",
    "a11y_label": "status"
  },
  "children": [ /* ViewNode… */ ]
}
```

**Node `type` values** the T9/T11 renderer understands (non-exhaustive
product set; providers SHOULD stick to this list unless a later contract
major adds types):

| type | Role |
|------|------|
| `text` | Label / body |
| `button` | Tappable; `props.action` fires ActionMessage |
| `hstack` / `vstack` / `zstack` | Layout stacks |
| `spacer` | Flexible space |
| `scroll` | Scroll container |
| `list` / `row` | List chrome |
| `image` | `image_asset` / `image_url` / `sf_symbol` |
| `textfield` | Input; value returns via ActionMessage |
| `nav` / `screen` | Navigation chrome / full screen root |
| `divider` | Visual separator |

**ViewProps** (snake_case on the wire; Swift `CodingKeys` map):

| Field | Purpose |
|-------|---------|
| `text`, `placeholder` | Content |
| `sf_symbol`, `image_asset`, `image_url` | Imagery |
| `font`, `weight`, `color`, `bg_color`, `opacity`, `corner_radius` | Style |
| `spacing`, `padding`, `alignment`, `min_length`, `max_lines`, `truncate` | Layout |
| `title`, `title_display_mode` | Navigation chrome |
| `disabled`, `action`, `style` | Interaction |
| `keyboard`, `autocorrect`, `autocapitalize`, `submit_label` | Input |
| `scroll_anchor`, `scroll_dismiss_keyboard`, `keyboard_avoidance` | Scroll |
| `frame_width`, `frame_height`, `frame_max_width`, `frame_max_height` | Frame (`"infinity"` allowed for max) |
| `foreground_style`, `content_mode` | Visual |
| `a11y_label` | Accessibility |

### 6.2 Composition & data flow

- A surface is a `ViewNode` tree, produced either as a static `root` in
  the manifest / surface push, or by a Lua view function the provider
  ships, evaluated against that provider's feed state.
- jevonsd's **server-side UI producer** (🎯T27.6) composes each
  provider's surface into the aggregate Jevons UI. Composition is by
  surface, not by pixel: providers own their panel; Jevons owns layout.
- **Layout authority (decided):** Jevons may reorder, hide, or collapse
  provider surfaces by owner/CEO policy. Providers declare preferred
  `title` and `surface` id only — not absolute placement.
- User interactions flow back as `ActionMessage` routed to the owning
  provider (by surface namespace); `ViewMessage` / `DismissMessage` as
  today.
- Data flow is one-directional and reactive: **feed event → aggregated
  model → surface re-render**. A provider never pushes a view
  imperatively; it pushes feed state and the surface derives (static
  roots are the degenerate case of a constant function of empty state).

Wire messages (client lane, unchanged from T9):

```jsonc
// server → client
{ "type": "view", "slot": "main", "root": { /* ViewNode */ } }
{ "type": "dismiss", "slot": "sheet" }
// client → server
{ "type": "action", "action": "refresh", "value": "" }
```

## 7. Capability: mcp (tool endpoint)

A provider that offers tools declares `capabilities.mcp`:

```jsonc
{
  "transport": "http",
  "endpoint": "http://127.0.0.1:8741/mcp"
}
```

| Field | Meaning |
|-------|---------|
| `transport` | `http` (Streamable HTTP / MCP HTTP transport) or `stdio` (launch-mode child only) |
| `endpoint` | URL for `http`; ignored for `stdio` (hub attaches to the launched process pipes) |

Hub obligations (🎯T27.4):

1. Connect an MCP **client** to each enabled provider's endpoint.
2. Re-export tools into jevonsd's `/mcp` surface, namespaced
   `{id}__{tool}` and attributed to the provider.
3. Tools appear/disappear with provider enable/disable and config reload.

Providers that are also used **directly** by agents (fleet already
MCP-native) may keep a direct config entry; hub aggregation is additive
so owner chat / automation can call provider tools without per-agent
MCP config edits.

UI-triggered actuation still routes as `ActionMessage` → provider; the
provider may implement the action by calling its own tools under its
doit policy. jevonsd never executes provider tools as a side effect of
rendering.

## 8. Versioning & trust

- **Contract version** is a single major integer (`contract`). Minor
  evolution is additive (new optional manifest fields, new feed kinds,
  new ViewNode types); a breaking change bumps the major and jevonsd may
  run multiple majors side by side during migration.
- **Trust.** A launched provider is code jevonsd executes; a connected
  provider is a peer daemon. Both are *trusted to describe themselves
  honestly* — the contract is cooperative, not adversarial. The
  adversarial boundary is doit (actuation) and egress opt-in, not the
  feed/UI paths.

## 9. Conformance (the oracle — 🎯T27.1)

The contract ships with an **executable spec**:

| Asset | Path |
|-------|------|
| Contract types + registry hub surface | `internal/provider` |
| Reference mock (exactly one feed, one ViewNode surface, one MCP tool) | `internal/provider/mock.go` |
| Conformance suite | `internal/provider/*_test.go` (`go test ./internal/provider`) |

### 9.1 Mock fixed content

| Cap | Fixed value |
|-----|-------------|
| id | `mock` |
| feed | `pulse` / schema `mock.pulse.v1` / `replay: true` |
| UI surface | `mock.status` — golden ViewNode tree (vstack + text + button) |
| MCP tool | `mock_ping` (returns `{ "pong": true }`) |

### 9.2 Suite assertions

Through the in-process hub surface (`provider.Registry` — the same API
jevonsd will own after 🎯T27.2/T27.3/T27.4/T27.5/T27.6):

1. **Registration + describe** — mock registers; manifest carries all
   three capabilities with the fixed names above.
2. **Feed** — events appear in the aggregated model in order; resume
   from a cursor across a simulated restart with no gap and no
   re-ingest of already-folded `(origin, feed, seq)`.
3. **UI** — composed tree contains the mock surface at the expected
   slot; golden root matches; `ActionMessage` round-trips to the mock.
4. **MCP** — `mock_ping` is listed via the hub's aggregated tool view
   under the mock namespace.
5. **No-per-provider-code** — package tests assert that the
   provider-handling path has no hard-coded concrete third-party
   provider ids (`mnemo`, `doit`, …) outside fixtures/comments.
6. **Loop-safety** — a self-origin / already-seen event is dropped.
7. **Egress** — mock declares `egress: false`; suite records that
   opt-in is required for external reach (accepted residual until a
   live egress probe exists in 🎯T27.3).

Every downstream target and every real provider is verified by
**passing this suite**, never by description alone.

### 9.3 Load-bearing properties → oracle map

| Property | Oracle | Residual |
|----------|--------|----------|
| No per-provider code in hub path | Grep + generic mock conformance | Real providers must not require jevonsd patches (enforced at T27.8) |
| Loop-safety | Conformance feed fold test | — |
| Egress opt-in | Manifest field + mock `egress:false` | Live network probe deferred to supervisor work |
| Three capabilities observable | Conformance registration test | Full WS/MCP network path lands with T27.3/T27.4; in-process hub is the T27.1 thin net |
| Contract design quality | Class-3 human sign-off | One-time accept/reject; does not gate suite green |

## 10. What jevonsd must gain to host this

| Need | Target |
|------|--------|
| structured config + persistence (provider registry) | 🎯T27.2 |
| generic supervisor (launch) + connect + reconcile | 🎯T27.3 |
| MCP client → re-export into `/mcp` | 🎯T27.4 |
| feed ingestion → aggregated live model | 🎯T27.5 |
| server-side UI producer, multi-provider compose | 🎯T27.6 |
| desktop menu-bar/tray head over the aggregate | 🎯T27.7 |
| mnemo as first live provider (passes this suite) | 🎯T27.8 |
| automation-liveness on provider feeds | 🎯T27.9 |

`internal/provider` is the contract kernel those targets grow; they do
not redefine the protocol.

## 11. Decisions closed at T27.1 (was open questions)

1. **Feed transport framing** — dedicated `/ws/provider` over the same
   JSON fabric (not multiplexed on `/ws/remote` client lane).
2. **Feed durability floor** — bounded window for `replay:true` (§5.2);
   hub may snapshot folds.
3. **UI composition authority** — Jevons owns layout policy (§6.2).
4. **`_filter` envelope** — orthogonal mcpbridge feature; out of this
   contract.
5. **MCP aggregation** — **in scope** (parent 🎯T27 + 🎯T27.4). Hub
   re-exports provider tools; agents may also keep direct MCP links.
