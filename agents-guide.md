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

## Fleet spawn path (🎯T78)

**Default for child implementation work:** create a Jevons fleet agent or
durable thread so the child **outlives the spawner** and can show in the
RHS fleet panel (🎯T72 family).

| Need | Tool |
|---|---|
| Named long-lived PO/boss/worker | `jevons_agent_start` → `jevons_agent_send` |
| Durable owned conversation | `jevons_thread_spawn` → `jevons_thread_direct` |
| One-shot task, no ongoing ownership | `jwork` |

**Do not default to** Grok `spawn_subagent` (or worktree subagents that
die with the parent). Those children are not first-class fleet entries,
vanish on parent interrupt, and break multi-agent observability.

Hard suppress of harness subagent spawn is optional where the Grok CLI
allows it; until then this convention plus jevons MCP tools is the
enforced path. Brief every new agent with target IDs and ownership —
never bare "go".

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
