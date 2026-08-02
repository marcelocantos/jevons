# Upgrade without draining agents (🎯T40)

**Status:** design / partial scaffold — **not achieved**. Full process
durability needs claudia connect-mode (agents outlive the coordinator
stdio session).

## Goal

Restart `jevonsd` (binary upgrade) without SIGKILL/replacing live Grok ACP
agent processes, then reattach to the same `session_id`.

## Current reality

- `cmd/jevonsd` calls `defer registry.StopAll()` on exit → agents stop.
- Grok ACP is child-process over stdio; closing pipes ends the session
  even if the OS process were detached.
- Conversation durability (session/load + journal) already survives
  coordinator restart; **process** durability does not.

## Required work (not done)

1. **claudia:** detach / external-process mode + reattach by PID or named
   pipe / unix socket ACP transport (not parent-owned stdio alone).
2. **jevonsd:** upgrade signal path that skips `StopAll` and records
   live agent handles for reattach.
3. **Drill:** start agent → upgrade binary → prove same PID + session_id.

## Out of scope for unattended batch

Shipping a dishonest “achieve” without process survival. Keep 🎯T40 open
until claudia supports connect-mode.
