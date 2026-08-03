# Logging production-readiness audit (jevons)

**Status:** Audit + design only (no feature implementation in this change)  
**Date:** 2026-08-02  
**Plane:** Build (local commit; no Ship)  
**Prior art:** `docs/design/logging-telemetry-audit.md` + achieved 🎯T120.*  
  (re-evaluated from scratch for greenfield long-tail readiness — not an
  incremental patch list of T99/T124 incidents)

---

## 1. Verdict

**Not enough.** For a personal CEO/fleet product in heavy daily use — multi-agent
thrash, restarts, partial failures, silent empties, races — the system is **not
awash with greppable fingerprints** on the paths that will dominate the long
tail. 🎯T120 productized a **narrow decision stream** (client route / attention /
send-queue / history hydrate + server `agent_send` status + browser→journal
pipe). That is real progress for “why did the wire look like that?” on the owner
composer path. It is **not** production readiness: most fleet lifecycle tools
succeed without slog, pure orchestration packages (`butler`, `fleet`, `thread`)
emit **zero** slog, the durable event journal is **browser-only** (server slog
is process-ephemeral unless launchd captures it), REST soft-success paths
return 200-with-empty with no fingerprint, MCP traffic is Debug-only, and there
is no request/turn correlation across HTTP ↔ WS ↔ MCP ↔ agent. Hours later, the
owner still cannot answer most “why did X happen?” questions from logs alone
without reading source or replaying state files.

---

## 2. Executive scorecard

Coverage grades: **none** · **sparse** · **partial** · **good** · **aggressive**

| Subsystem | Grade | One-line why |
|-----------|-------|--------------|
| **cmd/jevonsd boot / shutdown / overseer health** | **good** | Rich structured boot, key-presence (not values), OVERSEER NOT RUNNING / TOOLLESS, upgrade exit handles |
| **Chat WS (`/ws/chat`)** | **partial** | Connect/disconnect, interrupt, rewind, durability failure, send failure → wire error; full `chat: received` dump; notify drain only **Debug** |
| **Chatlog JSONL** | **good** (content) | Durable wire replay (T30.1); not a decision log |
| **Browser decision path (T120)** | **good** (narrow) | DecisionLog + jLog for route/attention/send_queue/history/cost·fleet *errors* |
| **Browser transport (`transport.js`)** | **sparse** | Reconnect backoff/watchdog silent in module; index logs reconnect/hydrate only |
| **Agent RHS transcript API** | **none** | Zero slog; 200 + `empty:true` on missing session / read error — soft success invisible |
| **Fleet list / progress hub** | **sparse** | `agents_changed` + progress hub; no decision trail for secondary-line choice; client logs only `refresh_error` |
| **MCP `agent_send`** | **good** | Structured `component=agent_send` status on every success path (T120.2) |
| **MCP agent start/stop/kill** | **sparse** | Kill + brief inject + notify log; **start/stop success silent**; errors return MCP text only |
| **MCP threads / event_push / target_file / mcp_reconnect** | **none–sparse** | Tool results only; no structured lifecycle slog |
| **MCP request middleware** | **sparse** | `mcp request` at **Debug** — off by default in production |
| **butler / fleet / thread packages** | **none** | Heart of Deliver/Launch/Spawn/Direct — pure return errors, zero slog |
| **Fleet health sweep** | **partial** | Re-launch / clear dead handle Info/Warn; no cadence summary |
| **Cost (collector / enforcer / ticker)** | **partial** | usage.db metrics + collector/enforcer warns; UI only `poll_error`; zero-burn *success* unexplained |
| **Workers (jwork + SQLite + SSE)** | **partial–good** | Durable worker events in workers.db; jwork slog on dispatch/progress/complete; API itself nearly silent |
| **Upgrade handoff** | **sparse** | main.go load/save Snapshot; package `upgrade` itself zero slog; reattach residual not fingerprinted per agent |
| **Event journal (`logs/events.jsonl`)** | **partial** | Solid sink + GET `/api/logs` + `jevons_logs_tail`; **only browser ingest appends**; server lifecycle never dual-written |
| **Voice stack** | **aggressive** | Densest slog surface in the product (FSM transitions, Grok session, audio) |
| **Auth / provision / mTLS** | **partial** | CA load/generate, device provisioned, cert fail; many 4xx reject paths silent |
| **Relay / remote** | **partial** | Connect/register/encrypt/read errors; no per-message decision trail |
| **Images / selftest HTTP** | **none–sparse** | Images: one success screenshot log; selftest HTTP: zero slog |
| **Transcript reader / integrity** | **sparse** | Fork Info; parse/empty classification not slog’d at API boundary |
| **Discovery / doit / config / selftest packs** | **none** | Library packages; errors surface only if callers log |
| **iOS thin client** | **sparse** | Relies on web path when embedded; native connect paths not inventoried as slog sources |
| **Correlation (corr / request_id)** | **sparse** | Page-session `corr` on browser decisions only; no server request_id / turn_id spanning MCP↔agent |
| **Metrics / counters / OTel** | **none** | No Prometheus/statsd; no counters for queue depth, route_match rate, empty-transcript rate |

---

## 3. As-is inventory

### 3.1 Sinks (where logs go)

| Sink | Location (default) | Format | Retention / notes |
|------|--------------------|--------|-------------------|
| **slog → stderr** | process (brew/launchd/`make run`) | text handler; Info default; Debug with `--debug` | Depends on service config; **not** under `state_dir` by default |
| **eventlog journal** | `~/.jevons/logs/events.jsonl` | JSONL `Event{ts,source,level,msg,component,decision,corr,fields}` | Append + fsync; **only** `POST /api/log` appends today |
| **chatlog** | `~/.jevons/chatlog/<overseer>.jsonl` | JSONL wire frames | Durable reconnect replay; full content |
| **usage.db** | `~/.jevons/usage.db` | SQLite cost events | Metrics, not product decisions |
| **workers.db** | `~/.jevons/workers.db` | workers + events | jwork lifecycle |
| **agents.json / threads.json** | `~/.jevons/` | Snapshot state | Not event streams |
| **doit audit** | under state (GateSpawn) | Hash-chained policy | Policy only |
| **Grok provider sessions** | `~/.grok/sessions/…` | Provider-owned JSONL | Boundary-only for jevons |
| **Grok voice log** | server GrokLog path | Voice turns | Separate; voice-gated |
| **Browser console** | DevTools | jLog mirror + ad-hoc `console.error` in transport | Not durable |
| **localStorage** | `jevons-attention-threads-v1` etc. | Product state | Not audit |

### 3.2 Client pipe

`web/scripts/jlog.js` → `POST /api/log` → `handleBrowserLog` → slog (`browser: …`) **and** eventlog Append when journal open.  
Fire-and-forget; CSRF/origin guard; always 204 (even on decode failure — **malformed body is silent**).

`web/scripts/decision_log.js` pure formatters; wired from `web/index.html` for T120 components.  
`pageCorr` UUID per page load (survives WS reconnect).

### 3.3 Server slog density (non-test call sites, hermetic count)

| Package | ~slog sites | Non-test .go files |
|---------|-------------|--------------------|
| `internal/server` | ~113 | 19 |
| `cmd/jevonsd` | ~46 | 2 |
| `internal/mcpserver` | ~27 | 17 |
| `internal/cost` | 6 | 10 |
| `internal/auth` | 2 | 5 |
| `internal/transcript` | 1 | 2 |
| **butler, fleet, thread, chatlog, upgrade, workers, discovery, doit, eventlog, selftest, config, cli** | **0** | (majority of orchestration) |

**Total product slog sites ≈ 226** (including test binaries’ helpers counted in tree scan of cmd/internal). Concentration is extreme: voice + chat + boot carry most of the weight.

### 3.4 HTTP / WS surface (no access log)

| Route | Observability today |
|-------|---------------------|
| `GET /health` | Uninstrumented health |
| `POST /api/log` | Ingest (always 204) |
| `GET /api/logs` | Journal tail |
| `GET /api/agents` | JSON list; recovery may NotifyAgentsChanged; **no slog** |
| `GET /api/agents/{name}/transcript` | **0 slog**; 200 empty on soft failure |
| `GET /api/history` | Errors → HTTP; **no success slog** |
| `GET /api/cost` | Encode warn only |
| `GET/POST` workers, images, self_test | Sparse / none |
| `/ws/chat` | Good connect/control; full msg dump |
| `/ws/voice` | Aggressive |
| `/ws/remote`, `/ws/agent-terminal` | Partial |
| **No** middleware request_id / latency / status access log | — |

### 3.5 MCP surface

Tools include: `jevons_agent_{list,start,send,stop,kill}`, `jevons_thread_*`, `jevons_event_push`, `jwork`, `jevons_cost`, `jevons_logs_tail`, `jevons_mcp_reconnect`, `jevons_target_file`, `jevons_active_work`, `self_test.*`, transcript/screenshot helpers.

| Tool class | Lifecycle slog |
|------------|----------------|
| `agent_send` | **good** (status fields) |
| `agent_kill` / brief / notify / fleet health | partial |
| `agent_start` / `agent_stop` / all thread tools / event_push / reconnect / target_file | **success path silent** |
| RPC method name | Debug only (`mcp request`) |

### 3.6 What T120 already fixed (honest credit)

- Client: route score/reason always logged; composer/focus; send-queue action; history reconnect/hydrate; cost/fleet **errors**.
- Server: `agent_send` structured status; browserlog promotes `component`/`decision`/`corr`; durable browser journal + MCP/API tail.
- Operator can `rg 'browser: decision\.|component=agent_send'`.

That is **necessary and insufficient** for long-tail production.

---

## 4. Invisible surfaces

Places with **no log on success and no log on soft-failure** (or failure only as MCP/HTTP body the operator never greps).

| Surface | Invisible today |
|---------|-----------------|
| **`jevons_agent_start` success** | name, parent, purpose, session_id, existed vs mint — only tool text to model |
| **`jevons_agent_stop`** | Entirely silent |
| **`jevons_thread_spawn` / adopt / direct / takeover / remove** | Zero slog at mcpserver/butler boundary |
| **`jevons_event_push`** | Success and typed undeliverable → MCP text only |
| **butler.Deliver / fleet.Launch / fleet.Deliver** | Package-silent; callers may not slog |
| **Overseer notify queue** | Defer path is **Debug** — busy re-queue invisible at Info |
| **GET transcript empty** | 200 + empty for no session_id, unreadable file, zero turns — no fingerprint of *which* case |
| **GET /api/agents empty registry** | Encodes `[]` with no slog |
| **History ReadRange success** | Client may jLog hydrate_*; server silent on empty window |
| **Cost snapshot $0** | No “why zero” (no events vs parse miss vs collector lag) |
| **MCP tool RPC** | Default Info build: no tool name stream |
| **Malformed POST /api/log** | 204, no warn |
| **Transport watchdog force-close / backoff delay** | Module silent (index only high-level reconnect) |
| **Registry dual-write drift** (agents.json vs threads.json) | Snapshot only; no event when writes diverge |
| **Upgrade reattach residual** | One boot line; per-agent reattach outcome not logged |
| **Parse / wire drop** | chat_wire drops some ACP echos; not always classified in slog |
| **jLog POST failure** | Swallowed (by design); no client-side ring buffer / fallback |

---

## 5. Aggression gaps

“Aggressive enough” = **every decision, lifecycle, and failure mode leaves a fingerprint** — not dump every payload.

### Must log **every time** (not only errors) — currently missing or Debug-only

| # | Event | Desired fields (sketch) | Priority |
|---|-------|-------------------------|----------|
| 1 | Agent lifecycle: start / stop / kill / rehydrate | `component=agent_lifecycle`, name, purpose, parent, session_id (trunc), existed, outcome | **P0** |
| 2 | Thread lifecycle: spawn / adopt / direct / takeover / remove | `component=thread`, id, parent, purpose, outcome | **P0** |
| 3 | Deliver / event_push outcome | target, path (thread\|agent), rehydrated, status, err class | **P0** |
| 4 | Soft-empty API responses | `component=agent_transcript`, empty_reason (`no_session`\|`read_error`\|`zero_turns`), name, session_id | **P0** |
| 5 | Overseer note queue enqueue / drain / defer | depth, deferred, err class — **Info**, not Debug | **P0** |
| 6 | Server lifecycle → **eventlog dual-write** (not slog-only) | same component schema; source=server | **P0** |
| 7 | MCP tool invoke (sampled or always at Info for mutating tools) | tool, agent, ok/err class, duration_ms | **P1** |
| 8 | Transport reconnect attempt / watchdog / version mismatch | attempt, delay_ms, reason | **P1** |
| 9 | Upgrade snapshot save + per-handle reattach plan result | handle count, residual list | **P1** |
| 10 | Cost zero-burn classification when UI shows $0 | events_in_window, collector_age, reason enum | **P1** |
| 11 | Registry write / agents_changed cause | reason (`fsnotify`\|`list_recovery`\|`progress`\|`spawn`) | **P1** |
| 12 | HTTP mutating routes access line | method, path, status, duration, request_id | **P2** |
| 13 | Virtualize enter/leave | debug only | **P2** |
| 14 | Counters (route_match, queue_depth, empty_transcript_rate) | later metrics child | **P2** |

### Architecture deltas (recommended)

1. **`component` enum** (stable string set) shared by client DecisionLog and server helpers — extend beyond T120:  
   `agent_lifecycle`, `thread`, `deliver`, `event_push`, `agent_transcript`, `notify_queue`, `mcp`, `transport`, `upgrade`, `registry`, `cost`, `http`.
2. **Server → eventlog dual-write helper**  
   `eventlog.Info(component, msg, fields…)` used by mcpserver/server for P0 events so `GET /api/logs` and `jevons_logs_tail` see **fleet** truth, not only browser decisions. slog remains the live mirror.
3. **Correlation**  
   - Keep browser `corr` (page session).  
   - Mint `request_id` per HTTP/WS accept and `turn_id` per owner chat turn; pass into overseer notify and agent_send fields when available.  
   - Optional: `agent` + truncated `session_id` on all fleet lines.
4. **Levels**  
   - Info = lifecycle + decisions.  
   - Warn = recoverable (re-queue, rehydrate, soft-empty with error).  
   - Error = fail-closed.  
   - Debug = high-frequency (virtualize, VAD, every MCP list).  
   - Mutating MCP tools at Info; read-only MCP at Debug or sampled.
5. **Sampling vs always-on**  
   Always-on for P0 lifecycle. Sample or Debug for: `/api/agents` poll success, virtualize, audio frames, raw MCP `tools/list`.
6. **Do not** dump full owner chat bodies in new fields (existing `chat: received` is an anti-pattern to ratchet down — see §6).

---

## 6. Noise / anti-patterns

| Pattern | Where | Risk | Remedy |
|---------|-------|------|--------|
| **Full owner message in slog** | `chat: received`, `"msg", msg` | Secrets in chat; log volume; greppability noise | Truncate ≤120; hash or turn_id; keep full text in chatlog only |
| **Voice FSM flood** | voice.go / voice_fsm | Can drown fleet signals in the same stderr | Keep aggressive but `component=voice`; filterable |
| **Browser always-204 on bad JSON** | handleBrowserLog | Silent loss of decision stream | Warn + counter field once per N |
| **jLog fire-and-forget loss** | jlog.js | Network blip loses decisions | Optional in-memory ring (last N) for `/api/logs` gap diagnosis |
| **Debug-only load-bearing events** | overseer notes deferred; mcp request | Production default hides the trail | Promote P0 to Info |
| **Unstructured free-text slog** | e.g. `FormatDeadAgentReport` as single string | Hard to filter | Prefer attrs `name`, `action`, `component` |
| **State files mistaken for logs** | agents.json, threads.json | Snapshots without “when/why” | Eventlog for mutations |
| **Dual sinks diverge** | slog vs events.jsonl | Operator greps the wrong place | Dual-write server events; document cookbook |
| **console.error only** | transport parse | DevTools-only | jLog with component=transport |

---

## 7. Recommended program

### 7.1 Challenge to T120 residual honesty

T120 residual (§7–§8 of prior note) deferred:

- butler/fleet package-deep slog  
- virtualize info  
- metrics counters  
- zero-burn cost reason  
- dual-device correlation  

Framed as optional / later, that residual **did abandon long-tail readiness** for the multi-agent thrash class: start/stop/spawn/deliver/empty-transcript/notify-queue are exactly the production questions. T120 correctly closed **owner-composer decision opacity**; it should not be read as “observability done.”

This audit **does not reopen T120** (achieved). It proposes a **new parent** aimed at production long-tail aggression.

### 7.2 Parent assertion (bullseye)

**🎯T128:** An operator can reconstruct fleet lifecycle, soft-empty UI causes, and
overseer delivery outcomes from durable logs (eventlog and/or service slog) hours
later without re-running the product or reading source.

Filed on 2026-08-02 with this note. Status starts **identified** (audit only).

### 7.3 Phased children (acceptance = desired-state oracles)

| Phase | Bullseye | Assertion | Acceptance oracles |
|-------|----------|-----------|-------------------|
| **P0.1** | **🎯T128.1** | Mutating fleet MCP tools emit structured lifecycle slog **and** eventlog rows | Go tests: agent_start/stop/kill, thread_spawn, event_push assert `component` + outcome; hermetic journal Append count ≥1 |
| **P0.2** | **🎯T128.2** | Soft-empty transcript and related 200-empty paths leave fingerprints | Test: no_session / read_error / zero_turns each produce slog or journal with `empty_reason` |
| **P0.3** | **🎯T128.3** | Overseer notify queue enqueue/drain/defer at Info with depth | Test or slog capture: busy defer visible at default Info level |
| **P0.4** | **🎯T128.4** | Server dual-write helper used by P0.1–P0.3 | `GET /api/logs?component=agent_lifecycle` returns server source events in hermetic temp state_dir |
| **P1.1** | (file when starting P1) | Transport reconnect/watchdog jLog | decision_log format + index/transport wiring; hermetic formatter tests |
| **P1.2** | | Cost zero-burn reason enum when snapshot rates are 0 | slog/jLog field `reason`; unit test on classifier |
| **P1.3** | | Upgrade reattach plan outcomes per handle | boot path logs residual list with counts |
| **P1.4** | | Mutating MCP Info access line (tool, ok, ms) | middleware test with fake handler |
| **P2.1** | | HTTP access log for mutating REST + request_id | optional middleware; skip high-frequency GET polls at Info |
| **P2.2** | | Truncate `chat: received` + redact policy ratchet | test that long bodies truncated in slog attrs |
| **P2.3** | | Counters / rate metrics (optional) | deferred unless owner prioritizes dashboards |

### 7.4 Implementation principles (for later impl agents)

- Build plane only until owner opens Ship.  
- Prefer one helper (`logDecision` / eventlog dual-write) over ad-hoc slog strings.  
- Hermetic tests with slog handler capture + temp journal — no live daemon required for gates.  
- Journey-or-exception for owner-visible empty/reconnect classes when product behaviour changes.  
- Privacy: still no API keys, pairing material, full multi-k dumps in decision fields.

---

## 8. Operator cookbook (today + after program)

### Today (post-T120)

```bash
# Live process / brew service log (path varies)
rg 'browser: decision\.|component=(thread_route|attention|send_queue|history|agent_send)|agent_send|chat: |OVERSEER|fleet health|jwork:' \
  /tmp/jevonsd.log   # or launchd path

# Durable browser decisions only
rg '"component":"(thread_route|attention|send_queue|history)"' ~/.jevons/logs/events.jsonl

# API / MCP
curl -sS 'http://localhost:13705/api/logs?component=thread_route&limit=50' | jq .
# MCP: jevons_logs_tail with component/decision filters

# Content replay (not decisions)
tail -n 50 ~/.jevons/chatlog/jevons.jsonl

# Cost metrics
sqlite3 ~/.jevons/usage.db '.tables'

# Fleet snapshot (state, not log)
jq '.[] | {name,purpose,parent,session_id}' ~/.jevons/agents.json
```

### After P0 program (target)

```bash
# Full product decision+lifecycle journal
rg '"source":"(browser|server)"' ~/.jevons/logs/events.jsonl
rg '"component":"(agent_lifecycle|thread|deliver|event_push|agent_transcript|notify_queue)"' \
  ~/.jevons/logs/events.jsonl

curl -sS 'http://localhost:13705/api/logs?component=agent_lifecycle&limit=100' | jq .
curl -sS 'http://localhost:13705/api/logs?q=empty_reason&limit=50' | jq .

# Correlate one page session
rg "corr=<pageCorr-uuid>" ~/.jevons/logs/events.jsonl /path/to/jevonsd.log

# Mutating MCP trail
rg 'component=mcp|mcp tool' /path/to/jevonsd.log
```

### Diagnosis playbooks (intended)

| Symptom | Grep |
|---------|------|
| Aside routed oddly | `component=thread_route` + `decision=` |
| Enter queued vs interrupt | `component=send_queue` |
| Agent never got work | `component=agent_send` + `agent_lifecycle` + `deliver` |
| RHS empty pane | `component=agent_transcript` + `empty_reason` |
| Worker reply vanished | `notify_queue` depth/defer + `chat: send to overseer failed` |
| After brew upgrade agents gone | `upgrade` snapshot + reattach residual |
| Cost shows $0 | `cost` reason + collector warn |

---

## 9. Hermetic evidence appendix (this audit)

Commands run against tree at audit time (illustrative; re-run after large refactors):

```text
slog sites by package (non-test): server ~113, mcpserver ~27, jevonsd ~46,
  cost 6, auth 2, transcript 1; butler/fleet/thread/upgrade/workers/… = 0

HTTP handlers with zero slog in file:
  agent_transcript.go (handleAgentTranscript)
  selftest_http.go (packs + run)

eventlog.Append producers (non-test): only internal/server/browserlog.go

Client modules with DecisionLog/jLog outside index wiring:
  decision_log.js, jlog.js only — pure modules (thread_route, send_queue,
  transport, agent_transcript, …) have zero jLog (logging is at call site in index.html)
```

### Comparison matrix vs T120 scope

| Dimension | T120 | This audit |
|-----------|------|------------|
| Agenda | Owner-visible decision opacity (composer/route/send) | Whole-product long-tail readiness |
| Fleet start/stop/spawn | Residual / out of scope | **P0** |
| Soft-empty APIs | Not in scope | **P0** |
| eventlog producers | Browser | Browser **+ server** |
| Orchestration packages | Deferred | Must leave boundary fingerprints |
| “Achieved” meaning | Decision path greppable | Hours-later production reconstruction |

---

## 10. Delivery note

This document is **audit + design only**. Implementation is a separate Build-plane
program under new bullseye children (see §7). Prior note
`logging-telemetry-audit.md` remains the as-implemented record for 🎯T120; do not
treat it as the production-readiness ceiling.
