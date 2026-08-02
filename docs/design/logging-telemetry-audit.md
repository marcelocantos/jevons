# Logging & telemetry audit (jevons)

**Status:** Phase 1 audit note (design only; instrumentation owned by jv-log-impl)  
**Date:** 2026-08-02  
**Plane:** Build (local commits; no Ship)  
**Related:** motivating incident T99 routing opacity; T111.1 agent busy/queue;
T113 send-queue; T118 fleet progress; T119 virtualize; T65 attention; T36 cost

## Goal

Every important **decision** and **lifecycle event** leaves a durable,
queryable trail that operators can grep without reconstructing intent from
post-hoc wire content.

**Motivating gap:** attention / 🎯T99 auto-route rewrote the owner draft into
an aside wire (`[attention:<id>|<title>]\n…`) with **no decision log**. The
chatlog only retained the post-rewrite frame — score, reason, and
match/no-match/ambiguous outcomes were invisible. That class of opacity
must die.

Single-user self-hosted product: **verbose is OK**. Still never log secrets
(API keys, tokens, pairing material, Authorization headers).

**Impl handoff:** jv-log-impl implements instrumentation against this note
and the 🎯T120 children. This document is the audit + architecture + oracle
spec, not the land of the instrumentation itself.

---

## 1. As-is map

### 1.1 Client (`web/`)

| Mechanism | Where | What is logged | Retention |
|-----------|--------|----------------|-----------|
| **`jLog` → POST `/api/log`** | `web/scripts/jlog.js` | `level` + `msg` + optional `fields`; mirrors console; auto `window.onerror` / `unhandledrejection` | Server slog only (process stderr); **no browser persistence** |
| **jLog call sites** | Almost only **voice/PTT/VAD** in `web/index.html` (~5 sites) | PTT engage/release, VAD onset/hold | Same |
| **`console.*`** | transport parse errors; ad-hoc | Sparse, uncorrelated | DevTools only |
| **Attention (T65)** | `attention_threads.js` + `send()` | **No decision trail** — localStorage holds product state (stack, focus, drafts); wire outcome only if sent | localStorage (state, not audit) |
| **T99 continuation route** | `thread_route.js` + `send()` | **No log** of `score` / `reason` (`match` \| `no-match` \| `ambiguous` \| `explicit-prefix`) before rewrite | — |
| **Send-queue (T113)** | `send_queue.js` + composer path | **No log** of `enqueue` vs `send` vs Control+Enter interrupt | In-memory only |
| **Shell+content (T119)** | `virtual_list.js` + index | No residency / enter-leave / hydrate-page logs | — |
| **Cost ticker** | poll `GET /api/cost` | Failures hide ticker; **no jLog** | — |
| **Fleet RHS** | poll `/api/agents` (+ WS agents_changed) | Silent catch on errors; progress derived without trail | — |
| **Hot reload** | `/ws/reload` (dev) | Page `location.reload()`; no decision/session-corr log of why | — |
| **localStorage** | `jevons-attention-threads-v1` (+ mermaid pin etc.) | Product state | Browser |

**Client→server pipe already exists** and is correct for the job:

- `jLog` → `POST /api/log` → `handleBrowserLog` (`internal/server/browserlog.go`)
  → slog with `browser: ` message prefix.
- Fire-and-forget; CSRF/origin guard via `rejectCrossSite`; always 204.
- **Underused outside voice.** Decision-opacity gaps are almost all pure
  client modules that never call `jLog`.

### 1.2 jevonsd / `internal/` (slog)

Default handler: `slog.NewTextHandler(os.Stderr, …)` in `cmd/jevonsd/main.go`
(Info default; Debug with flag). Captured by brew/launchd into the service
log (historically also `~/.jevons/jevonsd.log` depending on launch path).

| Area | Density | What | Silent / weak |
|------|---------|------|----------------|
| **cmd/jevonsd** | High at boot | Config, keys *present* (not values), CA, overseer health, upgrade handoff, shutdown | — |
| **internal/server** | Richest (~100+) | Chat connect/disconnect, `chat: received` (**full msg**), interrupt/rewind, durability failures, voice FSM, agents watch, images, remote | Decision *why* rarely fielded |
| **internal/mcpserver** | Sparse–medium | MCP request **debug**; fleet brief inject; agent kill/notify; jwork dispatch; dead-agent lines | **agent_send busy/queue/interrupt outcomes not slog'd** on the success path (status returns in MCP text only) |
| **agent_send.go** | Partial | Rehydrate info; drain success/warn; process-not-alive re-queue | `sent` / `queued` / `interrupted_*` **no structured slog** |
| **internal/cost** | Sparse | Collector scan warnings; enforcer warn/error; clamp-down | No per-tick burn decision trail in slog (metrics live in usage.db) |
| **butler / fleet / thread** | **~0 slog** | Pure orchestration packages | Lifecycle/decisions invisible at package boundary |
| **workers** | Store only | SQLite + SSE; package itself barely slogs | Events are durable *state*, not slog stream |
| **chatlog / discovery / upgrade / doit / selftest / config** | ~0–few | Errors only if any | — |

### 1.3 Durable stores (not slog, but “what is recorded”)

| Store | Path (default) | Role | Decision log? |
|-------|----------------|------|----------------|
| **chatlog JSONL** | `~/.jevons/chatlog/<overseer>.jsonl` | Append-only wire frames for reconnect replay (🎯T30.1) | **Content only** — post-rewrite user lines, assistant, tool_result, errors. No route score/reason. |
| **usage.db** | `~/.jevons/usage.db` | Token/cost events (T36); L1 collector tails Grok/Claude session JSONL | Metrics, not product decisions |
| **budget.json** | `~/.jevons/budget.json` | Clamp policy | Config |
| **workers.db** | `~/.jevons/workers.db` | jwork workers + events (T8.2) | Worker lifecycle (good); not fleet Grok agents |
| **agents.json** | `~/.jevons/agents.json` | Registry: name, workdir, session_id, parent, purpose, auto_start | Snapshot state, not event stream |
| **threads.json** | `~/.jevons/threads.json` | Durable butler threads (dual-write with fleet under T114) | Snapshot |
| **doit audit** | under state (jwork GateSpawn) | Hash-chained policy decisions | Policy only (T8.3) |
| **Grok voice log** | server GrokLog path | Voice turns when voice bridge on | Separate from chatlog; dormant path pending T37 |
| **Provider sessions** | `~/.grok/sessions/…` | Grok chat_history / updates JSONL | **Provider-owned**; jevons must log at *boundary* only |

### 1.4 HTTP / API surfaces with observability role

| Surface | Role today |
|---------|------------|
| `POST /api/log` | Browser → slog (`browser:` prefix) |
| `GET /api/cost` | Burn snapshot for ticker (T117 fixed Grok rates) |
| `GET /api/agents` | Fleet list + progress (T118) |
| `GET /api/workers` + SSE | jwork observability |
| `GET /api/history` | Older chatlog pages (T57) |
| `GET /api/self_test/*` | Pack grades (T110) |
| **No** Prometheus / OTel / statsd | — |

### 1.5 What is *not* logged (summary)

- Client routing / composer / send-queue **decisions** (score, reason, action).
- Successful agent_send **status** as structured slog (`queued`, `interrupted_sent`, …).
- butler Deliver / thread spawn / event_push **why** (outcome may surface in chat if someone notices).
- Progressive history hydrate / virtualize materialize band (T119).
- Cost poll failures and zero-rate reasons in the browser.
- Correlation across browser page-session ↔ overseer turn ↔ fleet agent.

---

## 2. Gaps ranked by decision-opacity

Opacity = “could an owner/overseer answer *why did the system do X?* from
logs alone, without UI reconstruction or guessing.”

| Rank | Gap | Opacity | As-is evidence | Priority for impl |
|------|-----|---------|----------------|-------------------|
| **1** | **Attention / T99 routing** | **Total** — decision never recorded; chatlog only post-rewrite wire | `thread_route.route` returns `{threadId, score, reason}`; `send()` rewrites then discards hit | **P0** |
| **2** | **Composer / prefix handling (T65)** | **Total** for local commands; wire-only for asides | `AttentionThreads` handleComposer: park/capture/pursue/main vs routed send | **P0** |
| **3** | **Owner send-queue (T113)** | **Total** | `SendQueue.decideSend` enqueue vs send vs interrupt — no jLog | **P0** |
| **4** | **Agent busy / queue / interrupt (T111.1)** | **Partial** | MCP text carries status; drain/rehydrate slog; **success path has no structured slog** for status/queued | **P1** |
| **5** | **Reload / reconnect / history hydrate** | **High** | chat connect/disconnect + chatlog replay slog; client progressive `/api/history` + T119 materialize silent | **P1** |
| **6** | **Cost ticker trust path (T117)** | **Medium** | usage.db + collector warn; UI silent on poll/zero | **P1–P2** |
| **7** | **Fleet status / progress (T118)** | **Medium** | ACP→progress hub→`/api/agents`; no decision log of secondary-line choice | **P1–P2** |
| **8** | **Virtualize enter/leave (T119)** | **High volume if naive** | Pure layout math; debug-only when needed | **P2** (debug level) |
| **9** | **butler / fleet package silence** | **Structural** | Zero slog in packages that own Deliver/Launch | **P2** (boundary logs at mcpserver/server first) |
| **10** | **Metrics counters / decision sink** | N/A product | No counters for route_match, enqueue, agent_queue_depth | **P3** (later child) |

---

## 3. Target architecture

### 3.1 Principles

1. **Decision logs ≠ content logs.** Chatlog keeps wire content for replay.
   Decision stream records *why* the wire / queue / agent path looks that way.
2. **Structured fields** over free-text only — stable keys for `rg` and tests.
3. **Correlation IDs** (when available):
   - `corr` — browser page-session UUID (generate once per load; pass in jLog fields).
   - `agent` — fleet agent name when relevant.
   - `session_id` — provider/registry session when known (truncated OK).
   - `turn_id` / `request_id` — optional; mint when a clear unit exists.
4. **Reuse `POST /api/log`** for all client decisions — do not invent a second pipe.
5. **Levels**
   - `debug` — high volume (virtualize enter/leave, VAD).
   - `info` — decisions and lifecycle (route match, enqueue, agent_send status).
   - `warn` — recoverable (drain fail, re-queue, cost poll fail).
   - `error` — fail-closed (chatlog append, send undeliverable).
6. **Redaction** — truncate free-text previews (~120 chars); never log API keys,
   pairing certs, or full multi-k transcript bodies on scroll events.

### 3.2 Client pattern

```js
// Pure, DOM-free formatters (hermetic Node tests) → jLog
// msg convention: "decision.<area>" so server logs read as
//   browser: decision.thread_route  component=thread_route ...
DecisionLog.route(hit, { draftPreview, corr });
DecisionLog.composer(result, { corr });
DecisionLog.sendQueue(decision, { corr, depth });
```

Stable fields on every decision event:

| Field | Meaning |
|-------|---------|
| `component` | `thread_route` \| `attention` \| `send_queue` \| `history` \| `fleet` \| `cost` \| … |
| `decision` | Enum string: `match` \| `no-match` \| `ambiguous` \| `enqueue` \| `send` \| `interrupt` \| … |
| `corr` | Page-session correlation id |
| area-specific | e.g. `score`, `threadId`, `reason`, `queued`, `status` |

### 3.3 Server pattern

```go
slog.Info("agent send",
  "component", "agent_send",
  "name", name,
  "status", res.Status, // sent | queued | interrupted_sent | ...
  "queued", res.Queued,
  "rehydrated", rehydrated,
  "interrupt", interrupt,
)
```

Browser path: keep `browser: ` prefix; pass through `component` / `decision` /
`corr` from JSON fields unchanged (already works if client sends them).

Optional later: JSON slog handler or dual-write to
`~/.jevons/decisions.jsonl` (separate child target; not required for P0).

### 3.4 Operator query (today’s sink)

```bash
# service log (path depends on brew services / launchd)
rg 'browser: decision\.|thread_route|agent send|component=' ~/.jevons/jevonsd.log
# or process stderr capture for local make run
```

### 3.5 Boundary only

Instrument **jevons-owned** surfaces: web decisions, `/api/log`, chat WS,
mcpserver agent tools, cost/workers APIs. Do **not** wrap Grok CLI internals
or dump provider session files into slog.

---

## 4. Proposed bullseye children (with acceptance oracles)

Parent assertion: **Decision and lifecycle events that affect owner-visible
behaviour are observable end-to-end without reconstructing from chatlog alone.**

| ID | Assertion (desired state) | Acceptance oracles |
|----|---------------------------|--------------------|
| **T120** | Parent: decision/lifecycle observability is productized | Audit note committed; children achieved; residual declared |
| **T120.1** | Client send path logs attention, T99 route, and send-queue decisions via jLog | Hermetic tests on pure DecisionLog formatters; `send()` wires jLog for route/composer/queue; manual or log-fixture: a route `match` appears as `browser: decision…` with `score`+`reason` |
| **T120.2** | Server agent send/queue/interrupt emits structured slog status | Go test or slog-handler test double asserts fields `component=agent_send`, `status`, `name`, `queued` on sent/queued/interrupted paths (not only drain) |
| **T120.3** | Browser log API preserves decision fields (`component`, `decision`, `corr`) | Go test on `handleBrowserLog` body → slog attr pass-through |
| **T120.4** | Reload/history hydrate and cost/fleet opacity reduced | jLog (or slog) on progressive history page load + cost poll error; fleet progress derivation optional debug; hermetic where pure |

**Impl order for jv-log-impl:** T120.1 → T120.3 (pipe trust) → T120.2 → T120.4.

**Out of scope for first land:** Prometheus, decisions.jsonl sink, wrapping
butler package internals, virtualize flood at info level, third-party APM.

---

## 5. Privacy & size

| Rule | Detail |
|------|--------|
| Single-user verbose OK | Decision enums + short previews; no artificial silence for “cleanliness” |
| No secrets | Never log API keys, Keychain material, pairing private keys, cookie/Authorization headers |
| Truncate free text | Draft/route previews ≤ ~120 runes; do not re-log full chatlog lines via jLog |
| High-frequency at debug | Virtualize enter/leave, VAD frames — not Info |
| Content vs decision | chatlog may hold full owner text (product); decision log must not become a second full transcript |
| Existing risk | `slog.Info("chat: received", "msg", msg)` already logs full owner text server-side — do not expand that pattern into new decision fields without truncation |

---

## 6. Coordination with jv-log-impl

| Role | Owns |
|------|------|
| **jv-log-audit (this)** | As-is map, gap ranking, architecture, bullseye filing, this note, commit |
| **jv-log-impl** | `decision_log.js` (+ tests), index.html wires, agent_send slog, browserlog tests, achieve T120.* with oracles |

Do not double-edit `web/index.html` without PO lane assignment if both
agents touch send path simultaneously — prefer impl owns the wire; audit
only if impl is stuck.

---

## 7. Residual (declared, not fudged)

- Provider-side Grok session files remain opaque; boundary logs only.
- Voice path has relatively rich slog + jLog; text chat decision path is the hole.
- Full metrics registry / JSONL decision sink / OTel: later.
- butler/fleet zero-slog packages: first land logs at mcpserver/server
  boundaries; package-deep slog is optional follow-up.
- Correlation across multi-device pigeon clients: not specified here.
