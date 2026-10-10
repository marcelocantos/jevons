# T1055 — OMP-only seat transport staging (not activated)

The owner permits clobbering old CLI conversation history. This removes the
transcript-transfer prerequisite, **not** the transport-compatibility or
security prerequisites. This document records the isolated Jevons slice;
none of these steps authorize restarting the development daemon, terminating
existing seats, or merging the isolated branch before Claudia T177 is ready.

## Provider contract

`cli.OMPSeatProvider` is a fail-closed *Session* selector. It maps the plan
ids `claude`, `codex`, `grok`, `cursor` to `anthropic`, `openai-codex`,
`xai-oauth`, `cursor`; it accepts those runtime ids and the historical `xai`
alias. Unknown ids and task-only `bedrock`/`ollama` refuse a fleet Session.
Task selection (`ResolveProvider`) remains separate. The selector is not yet
wired into the launch paths: Claudia v0.52 still dispatches bare `claude`,
`codex`, `grok` to legacy sessions. Wiring it prematurely would be a
misleading half-purge while live CLI handles and upgrade adoption remain.

## Required coordinated implementation

1. Pin a Claudia version with T177's documented OMP-only dispatch, including
   explicit fail-closed behavior for every Session provider and no process or
   tmux fallback. Check broker grant/reclaim and true resume, not only the
   in-process provider id. The existing v0.52 module does **not** suffice.
2. Wire the selector at **all** Jevons session entrypoints: MCP
   `stitchAgentStart`/`alignStartTransport`, fleet `ensureRegistered` and
   `LaunchRecovering` (which also handles stopped send/overseer recovery),
   registry startup/remint, and migration. Do not turn non-seat Bedrock Task
   requests into an OMP Session. Assert no provider can reach `Registry.Launch`
   as a bare CLI id and unsupported ids fail without spawning.
3. Once old seats have been explicitly disposed by Claudia-po, remove the
   T1047 live-provider and handoff preservation exceptions in
   `internal/mcpserver/agents.go`, `cmd/jevonsd/main.go`, and
   `internal/seatreg/remint.go`; delete the CLI transport-specific
   `internal/upgrade/handoff_transport.go` and its tests. Preserve generic
   upgrade snapshot/session metadata as needed for OMP recovery. Do not
   mistake the `RemintSubscription` preservation of `SessionID` for history:
   a CLI JSONL without an OMP spool is not an OMP conversation.
4. Audit provider-specific code individually: Claude CLI trust preflight and
   native JSONL/rewind; Codex app-server sandbox grants and receipts; Grok
   MCP reconnect; Cursor ACP resume/guard; tmux/connect-mode census and
   reattach; discovery/spool/sendq. Retain plan analytics and delivery
   evidence that consume OMP events; delete only code whose input is an
   unreachable CLI transport. Reconcile held sendq entries before swapping
   any still-active writer.
5. Run both repositories' routing, fresh/resume, restart, migration, queue,
   capability and security oracles; run full suites; then coordinate a live
   spawn/direct/stop/resume for every used plan provider with broker transport
   inspection and a zero-CLI-process census. Only the owner/PO controls
   existing seats and daemon activation.

## Known unresolved parity

In Claudia v0.52 `CapabilitySandboxPolicy` describes `SandboxMode` and
`ApprovalPolicy` as Codex CLI-only and refuses them outside that transport.
Jevons `CodexWorkSandbox` requests workspace-write only for bare `codex`;
`openai-codex` currently does **not** inherit that restriction. An OMP
replacement must either implement equivalent confinement or receive an
explicit owner security disposition. Do not silently relabel the seat as
sandboxed or claim parity. Cursor's OMP Session exists but full MCP tool
availability is marked unsupported in Claudia's capability matrix. These
are named blockers to an unconditional production purge, not reasons to
silently fall back to a CLI.
