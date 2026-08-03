# Upgrade without draining agents (🎯T40)

**Status:** converging — coordinator scaffolding landed; **process
durability residual** blocks achieve.

## Goal

Restart `jevonsd` (binary upgrade) without SIGKILL/replacing live Grok ACP
agent processes, then reattach to the same `session_id` **and the same
OS process**.

## Current reality

| Layer | Status |
|-------|--------|
| Conversation durability (`agents.json` session_id + session/load + chatlog) | **Works** today on normal restart |
| Skip `registry.StopAll()` on upgrade signal | **Works** (SIGHUP / `JEVONS_UPGRADE_EXIT`) |
| Externalize handles for next boot (`~/.jevons/upgrade-handles.json`) | **Works** (session_id list; PID usually 0) |
| Process survives coordinator exit | **No** — ACP is stdio-child; pipe close ends agent |
| Reattach same PID / live ACP session without minting process | **No** — needs claudia connect-mode |

## What landed (coordinator scaffolding)

1. **`internal/upgrade`** — exit mode, handle snapshot, reattach plan,
   residual constant. Oracle: `go test ./internal/upgrade/` (upgrade mode
   does not StopAll).
2. **`cmd/jevonsd`** — `SIGHUP` → upgrade exit (skip `StopAll`, write
   handles). `SIGINT`/`SIGTERM` still StopAll unless
   `JEVONS_UPGRADE_EXIT=1`. On boot, load prior snapshot and log
   reattach-by-session_id + residual.
3. **Drill:** `scripts/upgrade-drill.sh` (hermetic unit green; exits 2
   with residual until process survival is proven).

## Documented brew / launchd path

Coordinator-only upgrade intent (does **not** yet keep agents alive):

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

**Honest limit:** skipping `StopAll` avoids the deliberate registry kill
path. The Grok ACP child is still parented with stdio pipes to jevonsd;
when the coordinator process exits, the OS closes those pipes and the
agent dies. Conversation reappears via session/load; **process does not**.

## Residual (must clear before achieve)

1. **claudia connect-mode:** detach / external-process agent hosting +
   reattach by PID or unix-socket ACP transport (not parent-owned stdio
   alone). Expose PID (or socket path) on the public Agent API for
   handle externalization.
2. **Drill prove:** start agent → record PID + session_id → upgrade
   coordinator only → same PID still alive → new jevonsd continues chat
   on that session without minting a new conversation or process.
3. Optional: launchd `KeepAlive` / wrapper that upgrades coordinator
   without orphaning the agent supervisor.

## Acceptance mapping

| Criterion | Scaffolding | Full achieve |
|-----------|-------------|--------------|
| Process + session survive coordinator-only restart | residual | needs connect-mode |
| No mandatory drain window | upgrade exit skips StopAll | same + process live |
| Reattach same session_id without new conversation | conversation path yes | + same process |
| Documented brew/launchd + drill | documented; drill residual | drill exit 0 |
| Process durability not just conversation load | residual explicit | proven |

## Out of scope for dishonest achieve

Shipping “achieved” without process survival. Keep 🎯T40 **converging**
until the drill proves same-PID reattach.
