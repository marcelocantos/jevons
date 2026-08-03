# Upgrade without draining agents (🎯T40)

**Status:** connect-mode path landed — achieve when drill exit 0 + live
same-PID reattach proven on the fleet path.

## Goal

Restart `jevonsd` (binary upgrade) without SIGKILL/replacing live Grok ACP
agent processes, then reattach to the same `session_id` **and the same
OS process**.

## Current reality

| Layer | Status |
|-------|--------|
| Conversation durability (`agents.json` session_id + session/load + chatlog) | **Works** |
| Skip `registry.StopAll()` on upgrade signal | **Works** (SIGHUP / `JEVONS_UPGRADE_EXIT`) |
| Externalize handles (`~/.jevons/upgrade-handles.json`) | **Works** (session_id + connect URL/PID when connect-mode) |
| Process survives coordinator exit | **Works** with Grok connect-mode (default `CLAUDIA_GROK_CONNECT=1`) |
| Reattach same PID / live ACP session without minting process | **Works** via claudia Launch with `ConnectURL`+`ConnectPID` |

## Connect-mode (claudia)

Grok Session no longer has to be a parent-owned `grok agent stdio` child:

1. **Spawn** detached `grok agent serve --bind 127.0.0.1:<port> --secret <s>`
   (`Setsid` so consumer exit does not SIGHUP the serve).
2. **Dial** ACP over WebSocket:
   `ws://127.0.0.1:<port>/ws?server-key=<s>`.
3. **Persist** `ConnectURL` + `ConnectPID` on `AgentDef` / upgrade handles.
4. **Stop** kills the serve; **upgrade exit** skips `StopAll` so serve lives.
5. **Reattach** on next Launch when PID is still alive: dial + `session/load`.

Enable/disable:

| Mechanism | Effect |
|-----------|--------|
| `CLAUDIA_GROK_CONNECT=1` (jevonsd default when unset) | connect-mode on |
| `CLAUDIA_GROK_CONNECT=0` | force stdio (old behaviour) |
| `Config.GrokConnect` / `AgentDef.GrokConnect` | force on |
| `ConnectURL` set | reattach path |

## What landed

1. **`internal/upgrade`** — exit mode, handle snapshot (incl. connect
   endpoint), reattach plan with `ProcessReattachPossible` when URL+PID
   present. Oracle: `go test ./internal/upgrade/`.
2. **`cmd/jevonsd`** — default `CLAUDIA_GROK_CONNECT=1`; SIGHUP upgrade
   exit; boot merges upgrade-handles connect endpoints into registry
   before `StartAll`.
3. **claudia** — `startGrokACPConnect`, WS transport, `Agent.PID` /
   `ConnectURL`, registry persistence, detach/serve spawn.
4. **Drill:** `scripts/upgrade-drill.sh` (hermetic; `--live` for real grok).

## Documented brew / launchd path

```bash
# 1. Signal upgrade exit (skip StopAll; write upgrade-handles.json)
kill -HUP "$(pgrep -x jevonsd | head -1)"

# 2. Install new binary (brew upgrade / make install)

# 3. Start new coordinator
brew services start jevons
# or: make run
```

When launchd only delivers `SIGTERM` (e.g. `brew services restart`):

```bash
JEVONS_UPGRADE_EXIT=1 brew services stop jevons
# install binary
brew services start jevons
```

With connect-mode, detached serve PIDs outlive the coordinator; the new
process reattaches by `ConnectURL`/`ConnectPID` + `session_id`.

## Acceptance mapping

| Criterion | Status |
|-----------|--------|
| Process + session survive coordinator-only restart | connect-mode + drill |
| No mandatory drain window | upgrade exit skips StopAll |
| Reattach same session_id without new conversation | session/load on reattach |
| Documented brew/launchd + drill | this doc + `scripts/upgrade-drill.sh` |
| Process durability not just conversation load | detached serve PID + WS |

## Residual / caveats

- **MCP-on-load Grok CLI bug** still forces tool rotation on `session/load`
  when mcpServers are non-empty (existing ACP policy). Overseer uses
  user-scoped config MCP (🎯T58) so resume keeps tools.
- **Intentional `Stop`/`StopAll`** still kills serve (clears connect
  endpoint). Only upgrade exit leaves processes alone.
- **Claude/Codex** use their own durability (tmux / experimental); this
  target is Grok process durability for the default fleet backend.
