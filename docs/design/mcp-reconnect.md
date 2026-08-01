# Mid-session MCP reconnect (🎯T60)

## Problem

Boot attach is solved (T50/T58: user-scoped `~/.grok/config.toml` +
session/load). Mid-session drops (HTTP MCP daemon restart, flaky stdio)
leave servers gone until TUI `/mcps` Space/`r` or a fresh session. The
overseer had no in-conversation control.

Jevons is MCP **producer-only**; Grok CLI owns client attach.

## Thin path shipped

Callable tool: **`jevons_mcp_reconnect`**

| Arg | Effect |
|-----|--------|
| `server=<name>` | disable→enable that server via `grok mcp` |
| (omit) | `grok mcp list --json`, then cycle each name |

Mechanism relies on Grok's documented live toggle: enable/disable without
restarting the CLI, reloading MCP for active sessions (same family as TUI
`/mcps`, without leaving chat).

## Oracle

Hermetic unit tests in `internal/mcpserver/mcp_reconnect_test.go` fail if
reconnect is a no-op (must call disable then enable; empty list errors;
enable failures surface). Journey J6 requires the tool name on
`tools/list`.

## Residual

If a future Grok build stops live-reloading ACP sessions on enable/disable,
this tool becomes a config flip only — product truth would need a Grok-side
reconnect API. Auto-restart env (`GROK_MCP_AUTO_RESTART`) is orthogonal
background recovery, not the overseer control.
