# Jevons Agent Guide

Jevons is a remote control system for Claude Code instances — a
butler/CEO over a fleet of agents. It consists of a coordinator daemon
(`jevonsd`), a browser chat UI, and a TUI client (`remote`).

## Architecture

```
  browser / remote  ──WebSocket──►  jevonsd  ──spawns──►  Jevons (Claude Code)
                                        ──manages──►  workers / threads
                                   MCP ◄─────────────┘ (tool calls)
```

- **jevonsd**: HTTP/WebSocket server. Runs the overseer (Jevons) Claude
  Code session, exposes an in-process MCP server for worker/thread
  management, tails transcripts for cost accounting, and serves the web
  UI from `web/`.
- **remote**: Terminal UI client over WebSocket.
- **Primary UI**: browser at `http://localhost:13705/` (`/ws/chat`).

## Install (multi-step — not done until all succeed)

1. **Binary**: `brew install marcelocantos/tap/jevons`
2. **Service** (always-on): `brew services start jevons`
3. **Verify listening** (do **not** use bare `curl` against `/mcp` —
   MCP only answers JSON-RPC POSTs):
   ```bash
   lsof -iTCP:13705 -sTCP:LISTEN
   ```
4. **Optional — register as an MCP client** (after restart of the agent
   session that will call it):
   ```bash
   claude mcp add --scope user --transport http jevons http://localhost:13705/mcp
   ```
5. **Confirm tools** via a lightweight call (e.g. `jevons_thread_list`
   or `jevons_cost`) after the agent session restarts.

Generic MCP client config:

```json
{
  "mcpServers": {
    "jevons": {
      "url": "http://localhost:13705/mcp"
    }
  }
}
```

## Running manually

```bash
# Start the coordinator (default port 13705)
jevonsd --port 13705 --workdir ~/projects --model sonnet

# Connect a terminal client
remote --addr localhost:13705

# Or open the web chat
open http://localhost:13705/
```

## Key concepts

- **Jevons (overseer)**: Claude Code session managed by jevonsd that
  coordinates work. Receives user messages; delegates via MCP tools.
- **Thread**: Durable semantic unit (transcript + metadata + status),
  **not** tied to a live process. Process = disposable cache (spawn /
  idle-stop / rehydrate via `--resume`).
- **Workers / agents**: Claude Code sessions that do coding work.
- **Cost clamp-down**: Real-time spend monitoring with warn → throttle
  → pause → kill escalation and a hard spawn-halt on budget breach.

## MCP tools

jevonsd exposes an in-process MCP server at `/mcp`. Key tools:

### Threads (butler/CEO)

- **`jevons_thread_adopt`** — Register an existing Claude Code session
  observe-only (non-invasive). Params: `session_id`, `description?`.
- **`jevons_thread_list`** / **`jevons_thread_status`** — List / inspect
  durable threads (`active` / `working` / `blocked` / `done` / `idle`).
- **`jevons_thread_spawn`** — Create a thread and launch its process.
- **`jevons_thread_direct`** — Send a turn; rehydrates an idle process.
- **`jevons_thread_takeover`** — Promote adopt-observe → directable.
- **`jevons_thread_remove`** — Drop a thread record.

### Cost

- **`jevons_cost`** — Live burn-rate snapshot (fleet / global / alerts).

### Workers / agents

- **`jevons_active_work`** — Cross-repo active-work dashboard.
- **`jwork`** — On-demand worker dispatch (`text`, `cwd?`, `model?`,
  `provider?`). Provider is `claude` (default), `codex`, or `grok`
  (claudia v0.15+ Grok Build CLI Task harness). If `provider` is
  omitted, models like `grok-4` infer `grok`. Grok/Codex are **Task
  mode only** — persistent Session mode fails closed for those
  providers until claudia ships it.
- **`jevons_agent_list` / `_start` / `_send` / `_stop`** — Persistent
  agents (Claude Session mode via claudia Registry). Not Grok-backed
  yet (Session experimental).
- **`jevons_list_sessions` / `_create_session` / `_send_command` /
  `_kill_session`** — Session-level worker management. Create accepts
  `provider?` the same way as `jwork`.

Budget spawn-halt blocks `jwork`, `jevons_create_session`,
`jevons_agent_start`, and butler spawn/direct when the hard ceiling or
fleet-kill level is in force.

## WebSocket protocol

Primary path is `/ws/chat` (raw Claude Code JSONL). Legacy structured
JSON is on `/ws/remote`. Origin is validated on all upgrades.

## Configuration

| Path | Purpose |
|---|---|
| `~/.jevons/` | Data directory |
| `~/.jevons/threads.json` | Durable thread registry |
| `~/.jevons/usage.db` | Token-spend accounting |
| `~/.jevons/budget.json` | Spend budgets / thresholds (optional) |
| `~/.jevons/agents.json` | Agent registry |
| `~/.claude/managed-repos.md` | Optional managed-repo list |

## Gotchas

- The C++ app (`bin/jevons`) requires Git LFS objects and is not included
  in release binaries. Only the Go binaries are distributed.
- Jevons's CLAUDE.md and .mcp.json are generated at startup under
  `~/.jevons/jevons/`. Do not edit them manually.
- Cross-site browser POSTs to mutating HTTP routes are rejected; native
  clients without an Origin header still work on the LAN.
- Do not diagnose MCP readiness with bare `curl` — use `lsof` + a tool call.
