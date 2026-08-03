# Jevons Agent Guide

Jevons is a remote control system for **Grok Build** instances — a
butler/CEO over a fleet of agents. It consists of a coordinator daemon
(`jevonsd`) and a browser chat UI (also wrapped by the iOS app).

There is no Claude (or Codex) harness in jevons. The only provider is
Grok via claudia (`ProviderGrok`: Task mode and Session ACP).

## Architecture

```
  browser / iOS  ──WebSocket──►  jevonsd  ──spawns──►  Jevons (Grok Session ACP)
                                       ──manages──►  workers / threads (Grok)
                                  MCP ◄─────────────┘ (tool calls)
```

- **jevonsd**: HTTP/WebSocket server. Runs the overseer as a Grok ACP
  session, exposes an in-process MCP server for worker/thread management,
  tails `~/.grok/sessions` for cost accounting, and serves the web UI.
- **Primary UI**: browser at `http://localhost:13705/` (`/ws/chat`); the
  iOS app wraps the same UI over a paired QUIC relay.

## Install (multi-step — not done until all succeed)

1. **Binary**: `brew install marcelocantos/tap/jevons`
2. **Grok CLI**: install Grok Build and auth (`grok login` or `XAI_API_KEY`);
   ensure `grok` is on `PATH` or at `~/.grok/bin/grok`.
3. **Service** (always-on): `brew services start jevons`
4. **Verify listening** (do **not** use bare `curl` against `/mcp` —
   MCP only answers JSON-RPC POSTs):
   ```bash
   lsof -iTCP:13705 -sTCP:LISTEN
   ```
5. **Optional — register as an MCP client** after restarting the agent session:
   ```bash
   # Example for Claude Code as an MCP *client* of jevons (jevons itself is Grok-only):
   claude mcp add --scope user --transport http jevons http://localhost:13705/mcp
   ```
6. **Confirm tools** via `jevons_thread_list` or `jevons_cost`.

## Running manually

```bash
jevonsd --port 13705 --workdir ~/projects
open http://localhost:13705/
```

## Key concepts

- **Jevons (overseer)**: Grok Session ACP process managed by jevonsd.
- **Thread**: Durable semantic unit (transcript + metadata + status), not
  tied to a live process. Process = disposable cache.
- **Workers / agents**: Grok Task or Session workers.
- **Sessions on disk**: `~/.grok/sessions/<encoded-cwd>/<session-id>/`
  plus `~/.grok/active_sessions.json`.

## MCP tools

- **Threads**: `jevons_thread_adopt`, `_list`, `_status`, `_spawn`,
  `_direct`, `_takeover`, `_remove` — Grok sessions only.
- **Cost**: `jevons_cost` — burn-rate snapshot (collector tails
  `~/.grok/sessions`).
- **Workers**: `jwork` (sole ephemeral primitive — one self-contained
  task, runs to completion) and `jevons_agent_*` (named durable agents).
  The legacy `jevons_*_session` tools were removed (🎯T41).
- **MCP resilience (🎯T60)**: `jevons_mcp_reconnect` — from inside the
  overseer chat, re-attach dropped MCP servers (all, or one named
  server) without session rotate or TUI `/mcps`. Cycles
  `grok mcp disable` → `enable` so tools from servers like github/gmail
  work again in the same session.

## Fleet spawn path (🎯T78)

**Default for child implementation work:** create a Jevons fleet agent or
durable thread so the child **outlives the spawner** and can show in the
RHS fleet panel (🎯T72 family).

| Need | Tool |
|---|---|
| Named long-lived PO/boss/worker | `jevons_agent_start` → `jevons_agent_send` |
| Durable owned conversation / aside | `jevons_thread_spawn` → `jevons_thread_direct` (or unified push/send by name) |
| One-shot task, no ongoing ownership | `jwork` |

### Unified participant model (🎯T114)

An **aside is a kind of agent** (purpose=`aside`). Work agents use
purpose=`work`. One registry id space and one deliver path:
`jevons_event_push` / Deliver resolves **thread or agent by name** — no
"no thread X" when the agent exists. UI: work agents **and** asides on the
RHS fleet tree (asides use 💡 chrome; 🎯T136) — not a top attention chip
bar. Same underlying registry records.

**Do not default to** Grok `spawn_subagent` (or worktree subagents that
die with the parent). Those children are not first-class fleet entries,
vanish on parent interrupt, and break multi-agent observability.

Hard suppress of harness subagent spawn is optional where the Grok CLI
allows it; until then this convention plus jevons MCP tools is the
enforced path. Brief every new agent with target IDs and ownership —
never bare "go".

### Multi-slice fan-out (🎯T111.4)

PO/boss agents on **multi-slice** missions must spawn `jevons_agent_start`
children (with `actor`/`parent` lineage) rather than unbounded solo
exploration. Single-agent tasks remain fine. Zero children after planning
on a multi-slice brief is a failure mode (`jevons_agent_list` fan-out
check). Prefer agents over threads for named long-lived workers.

### PO never implements (🎯T125)

**Stratum-1 product owners never implement themselves** — including small
patches, oracle/tests, and docs commits. Mirror rule: **spawn-only for Build work**;
no solo code/docs commits by the PO.

| Role | Does |
|---|---|
| **PO (Stratum 1)** | Plan, brief, spawn workers/bosses, collect evidence, stay free for overseer/owner directs |
| **Boss / worker** | Execute (edit, test, commit) under the brief |

POs stay **interruptible** so redirects from above are not blocked by a
solo coding session. **Residual:** instructional doctrine, not a hard
daemon spawn-gate, unless a later target adds enforcement.

### Overseer never parents product workers (🎯T129)

For **jevons-repo Build work**, the overseer (`jevons`) routes owner
intent to **`jevons-po`** and does **not** `jevons_agent_start` product
workers with `parent=jevons` (or actor=jevons as parent).

| Role | Spawns product workers with parent= |
|---|---|
| **Overseer (`jevons`)** | Does **not** — routes to PO only |
| **`jevons-po` (sole spawn parent)** | Yes — bosses/workers under T125 |

**Exception:** PO dead/unregistered → rehydrate or start PO first, then
PO spawns. **Residual:** instructional until a later target adds registry
enforcement (reject wrong parent).

### Filing reflex (🎯T130) — doctrine first, narrative second

When a **real product gap**, **repeated failure mode**, or **standing
behavioural rule** appears mid-work → **file or prompt-file a bullseye
target** (name + acceptance) in the **same turn** — not only chat promises.

**Trigger phrases** that require filing (not "I'll remember"):
- "standing rule"
- "going forward"
- "from now on"
- "we should always…"
- plus: repeated failure, hierarchy slip, logging gap, UX pain, fleet doctrine

**Ceremony:** `jevons_target_file` and/or bullseye MCP (`bullseye_commit`
op=track / file tools). Related: ambient RSI **🎯T92**, hierarchy **🎯T129**.
**Residual:** one-off flukes may skip filing; judgment allowed.

## Delivery: local by default (🎯T104)

Owner vocabulary is **literal**:

| Said | Means |
|---|---|
| **master** | Local `master` branch |
| **locally** / **local only** | No `git push`, no GitHub PR, no CI merge |
| **merge to master locally** | Cherry-pick/merge onto local `master` only |

**Done** for fleet work = commits + evidence + notify overseer — **not**
"opened a PR" / "merged to origin/master".

Do **not** re-expand a local merge order into continuous origin/PR
shipping because a PO already opened remotes. Remote delivery only when
the owner **explicitly** asks to ship/push/PR.

## Configuration

| Path | Purpose |
|---|---|
| `~/.jevons/` | Data directory |
| `~/.jevons/threads.json` | Durable thread registry |
| `~/.jevons/usage.db` | Token-spend accounting |
| `~/.jevons/budget.json` | Spend budgets / thresholds (optional) |
| `~/.jevons/agents.json` | Agent registry |
| `~/.grok/sessions/` | Grok session store |
| `~/.jevons/jevons/AGENTS.md` | Generated overseer instructions |

## Gotchas

- Only Grok is supported. There is no `--provider` switch.
- Cost events from Grok may not carry Claude-style `costUSD` yet; the
  collector still tails session files for activity; pricing tables will
  improve as Grok usage telemetry is understood.
- Do not diagnose MCP readiness with bare `curl` — use `lsof` + a tool call.
