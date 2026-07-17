# Stability

## Stability commitment

Version 1.0 will represent a backwards-compatibility contract. After 1.0,
breaking changes to the public CLI interface, WebSocket protocol, REST API,
configuration format, or database schema require forking the project into a
new product (e.g. `jevons2`). The pre-1.0 period exists to get these surfaces
right before locking them in.

## Interaction surface catalogue

Snapshot as of v0.5.0.

### CLI: `jevonsd`

| Flag | Type | Default | Stability |
|---|---|---|---|
| `--port` | int | `13705` | Stable |
| `--relay` | string | `""` | Fluid — URL format and registration protocol may change |
| `--relay-token` | string | `""` | Fluid |
| `--instance-id` | string | `""` | Fluid |
| `--set-openai-key` | bool | `false` | Stable — interactive key prompt |
| `--set-xai-key` | bool | `false` | Fluid — new in v0.3.0, interactive xAI key prompt for Grok voice bridge |
| `--workdir` | string | `"."` | Needs review — semantics may evolve |
| `--model` | string | `""` | Needs review — may consolidate with config |
| `--jevons-model` | string | `""` | Needs review — same concern |
| `--debug` | bool | `false` | Stable |
| `--version` | bool | `false` | Stable |
| `--help-agent` | bool | `false` | Stable |

### CLI: `remote` (removed)

The `remote` terminal UI client was removed post-v0.5.0 (🎯T43 dead-surface
pruning); the web UI is the canonical interactive surface. The binary is no
longer built or shipped in the Homebrew formula.

### MCP Server (`/mcp`)

The legacy manager-backed session tools (`jevons_list_sessions`,
`jevons_session_status`, `jevons_create_session`, `jevons_send_command`,
`jevons_kill_session`) were removed post-v0.5.0 (🎯T41); the durable
thread model and `jwork` are the only worker lifecycles.

| Tool | Parameters | Stability |
|---|---|---|
| `jevons_agent_list` | (none) | Fluid |
| `jevons_agent_start` | `name, workdir, model?` | Fluid |
| `jevons_agent_send` | `name, text` | Fluid — async fire-and-forget since v0.3.0 |
| `jevons_agent_stop` | `name` | Fluid |
| `jevons_active_work` | `hours?, include_clean?` | Fluid — new in v0.4.0, cross-repo work dashboard |
| `jwork` | `task, repo?, model?` | Fluid — new in v0.4.0, on-demand worker dispatch |
| `jevons_transcript_read` | `session?, limit?` | Fluid — new in v0.3.0, reads Jevon conversation history |
| `jevons_transcript_rewind` | `session, n?` | Fluid — new in v0.3.0, trims Jevon history |
| `jevons_thread_adopt` | `session_id, description?` | Fluid — new in v0.5.0, adopt-observe a session as a durable thread |
| `jevons_thread_list` | (none) | Fluid — new in v0.5.0 |
| `jevons_thread_status` | `id` | Fluid — new in v0.5.0 |
| `jevons_thread_spawn` | `id, workdir, description?, model?` | Fluid — new in v0.5.0 |
| `jevons_thread_direct` | `id, text` | Fluid — new in v0.5.0, rehydrates idle process on demand |
| `jevons_thread_takeover` | `id` | Fluid — new in v0.5.0 |
| `jevons_thread_remove` | `id` | Fluid — new in v0.5.0 |
| `jevons_cost` | (none) | Fluid — new in v0.5.0, live burn-rate snapshot |
| `jevons_reload_views` | (none) | Fluid |

Transcript memory search has moved out-of-process. Global search across all
Claude Code sessions is now provided by the standalone
[`mnemo`](https://github.com/marcelocantos/mnemo) MCP server (previously
`jevons_search_memory`, `jevons_memory_query`, `jevons_memory_stats`,
`jevons_list_memory_sessions` — all removed in v0.3.0).

### WebSocket protocol

#### `/ws/chat` (new in v0.2.0)

Server sends normalized provider chat events (Grok ACP via
`internal/server/chat_wire.go`).
Client interprets user, assistant, tool_use, tool_result, and system events.
Client sends plain text messages (or "stop" to interrupt).

| Direction | Format | Stability |
|---|---|---|
| Server → Client | Raw JSONL lines (history + live) | Fluid |
| Client → Server | Plain text | Fluid |

#### `/ws/remote` (legacy)

Structured JSON messages for the iOS remote client.

| Direction | Stability |
|---|---|
| Server → Client | Fluid — many message types tied to Lua view architecture |
| Client → Server | Fluid |

#### `/ws/reload` (new in v0.2.0)

Dev-only hot reload signal. Server sends "reload" on file changes.

#### `/ws/agent-terminal` (new in v0.3.0)

Live PTY viewer for a running agent. Click an agent in the web UI to
stream its agent session output.

| Direction | Format | Stability |
|---|---|---|
| Server → Client | Raw PTY bytes | Fluid |

#### `/ws/voice` (new in v0.3.0)

Grok Realtime voice bridge. Full-duplex audio between the browser/iOS
client and the xAI Realtime API (`wss://api.x.ai/v1/realtime`). Server
transcodes, applies adaptive local VAD, and relays audio and events.

| Direction | Format | Stability |
|---|---|---|
| Server ↔ Client | Binary audio frames + JSON events | Fluid |

### REST API

| Method | Path | Stability |
|---|---|---|
| `GET` | `/health` | Stable |
| `GET` | `/` | Fluid — serves web UI from `web/` directory |
| `GET` | `/api/agents` | Fluid |
| `GET` | `/api/cost` | Fluid — new in v0.5.0, live spend snapshot for the web ticker |
| `GET` | `/scripts/*` | Fluid — new in v0.3.0, serves JS modules (transport.js, etc.) from `web/scripts/` |
| `POST` | `/api/realtime/token` | Fluid — rate-limited + cross-site rejected since v0.5.0 |

The `/api/sessions` REST endpoints (list/get/kill) were removed with the
legacy manager (🎯T41); thread state is served through the MCP tools.

### Agent registry (`~/.jevons/agents.json`)

New in v0.2.0. JSON array of agent definitions.

| Field | Type | Stability |
|---|---|---|
| `name` | string | Fluid |
| `workdir` | string | Fluid |
| `session_id` | string (UUID) | Fluid |
| `model` | string (optional) | Fluid |
| `auto_start` | bool | Fluid |
| `parent` | string (optional) | Fluid |

### Configuration

| Path | Purpose | Stability |
|---|---|---|
| `~/.jevons/` | Data directory | Stable |
| `~/.jevons/jevons.db` | SQLite database | Stable |
| `~/.jevons/agents.json` | Agent registry | Fluid |
| `~/.jevons/threads.json` | Durable thread registry (butler/CEO) | Fluid — new in v0.5.0 |
| `~/.jevons/usage.db` | Token-spend accounting (cost clamp-down) | Fluid — new in v0.5.0 |
| `~/.jevons/budget.json` | Spend budgets / thresholds | Fluid — new in v0.5.0 |
| `~/.jevons/jevons/AGENTS.md` | Generated overseer instructions | Fluid |
| `~/.jevons/jevons/.mcp.json` | MCP server config for Jevons | Fluid |
| `~/.jevons/lua/views/` | Lua view scripts | Fluid |
| `~/.jevons/remote_history` | `remote` TUI input history (orphaned — client removed) | Deprecated |
| `web/` | Web UI (served from disk, hot-reloaded) | Fluid |

Transcript memory (`~/.jevons/memory.db`) was removed in v0.3.0. The
mnemo MCP server now provides global session indexing; jevonsd no
longer maintains its own transcript database.

## Gaps and prerequisites

### Security
- WebSocket Origin is validated (no `InsecureSkipVerify`) since v0.5.0;
  cross-site browser POSTs to mutating HTTP routes are rejected.
- Default bind is still all-interfaces with mTLS off; LAN clients without
  an Origin header are not fully authenticated. Pairing ceremony verified
  but not the sole gate for every route.
- Workers and Jevons run with permissions bypassed.
- Cost clamp-down (L1–L3) and MCP spawn-halt guards ship in v0.5.0.

### Architecture
- Agent session management lives in `claudia` (Grok-only); butler/CEO
  thread model (`internal/thread` + `internal/butler` + `internal/fleet`)
  and token-spend clamp-down (`internal/cost`) ship in v0.5.0.
- Grok realtime voice bridge still lives in-tree.
- Lua view script runtime (🎯T9) is partially implemented — server-side
  rendering works; client-side Lua on iOS is not yet wired.
- sqlpipe state sync (🎯T10) is incomplete.
- Active work dashboard (🎯T16.1) complete.

### Testing
- Butler e2e oracles and cost clamp-down unit + synthetic runaway drill
  ship in v0.5.0. WebSocket / voice e2e paths remain lightly covered.

### Documentation
- NOTICES file missing for vendored iOS/ge dependencies.
- Homebrew formula includes a `brew services` block since v0.5.0.

## Out of scope for 1.0

- Mobile UI via ge engine (ge submodule and C++ app removed in 🎯T43).
- Worker-to-worker communication.
- Multi-user / multi-tenant support.
- Plugin or extension system.
