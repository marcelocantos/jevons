# Jevons Agent Guide

Jevons is a remote control system for **Grok Build** instances — a
butler/CEO over a fleet of agents. It consists of a coordinator daemon
(`jevonsd`), a browser chat UI, and a TUI client (`remote`).

There is no Claude (or Codex) harness in jevons. The only provider is
Grok via claudia (`ProviderGrok`: Task mode and Session ACP).

## Architecture

```
  browser / remote  ──WebSocket──►  jevonsd  ──spawns──►  Jevons (Grok Session ACP)
                                        ──manages──►  workers / threads (Grok)
                                   MCP ◄─────────────┘ (tool calls)
```

- **jevonsd**: HTTP/WebSocket server. Runs the overseer as a Grok ACP
  session, exposes an in-process MCP server for worker/thread management,
  tails `~/.grok/sessions` for cost accounting, and serves the web UI.
- **remote**: Terminal UI client over WebSocket.
- **Primary UI**: browser at `http://localhost:13705/` (`/ws/chat`).

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
   # Example for Claude Code as an MCP *client* of jevons (jeons itself is Grok-only):
   claude mcp add --scope user --transport http jevons http://localhost:13705/mcp
   ```
6. **Confirm tools** via `jevons_thread_list` or `jevons_cost`.

## Running manually

```bash
jevonsd --port 13705 --workdir ~/projects
open http://localhost:13705/
# or
remote --addr localhost:13705
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
- **Workers**: `jwork`, `jevons_agent_*`, `jevons_list_sessions`,
  `jevons_create_session`, …

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
