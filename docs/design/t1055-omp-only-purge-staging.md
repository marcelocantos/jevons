# T1055 — OMP-only seat transport staging (not activated)

The owner permits clobbering old CLI conversation history and explicitly
accepts loss of attach/terminal bytes/native rewind. Legacy CLI must be
removed from **both** Session and one-shot Task paths. This removes the
transcript-transfer and UI-equivalence prerequisites, **not** the
transport-compatibility or security prerequisites. This document records the
isolated Jevons slice; none of these steps authorize restarting the development daemon, terminating
existing seats, or merging the isolated branch before claudia/T177 is ready.

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

## One-shot Task inventory and disposition

`internal/mcpserver/jwork.go` is Jevons' one-shot path. It chooses a plan
provider with `cli.SelectAgentProvider`, then calls `claudia.NewTask` and
`Task.Run`, where Claudia v0.52 still dispatches provider-specific CLI
backends. A new OMP Task verb/dispatch and event/usage/error contract must
exist before rewriting this caller. `jwork` must canonicalize plan ids onto
OMP runtime ids before task creation and reject unsupported providers rather
than falling through to a vendor CLI. Preserve its `doit.GateSpawn`, worker
tracking, progress/usage accumulation and failure classification. Bedrock is
an API Task backend, not a legacy CLI or fleet Session; retain it only if
Claudia explicitly continues to support the non-CLI Task path. Ollama needs
an equally explicit non-CLI disposition. Tests must exercise new Task dispatch
with a fake broker and assert no CLI subprocess starts on fresh tasks or
unsupported providers. The Session selector in this isolated commit cannot
be reused blindly for Tasks: it intentionally refuses Bedrock.

The owner instructed Claudia-po to drop 14 stale Claude and two stale Grok
grants without remint; Jevons does not edit grants or terminate windows.
These numbers are an instruction/snapshot, not a new census. Claudia-po owns
that operation and its evidence. No development daemon restart here.

## Legacy-specific reader disposition

- `internal/server/chat.go` native Claude JSONL rewind: delete the native
  branch and retain journal-first rotate+recap on OMP; owner accepts loss of
  native rewind. `internal/server/claude_model.go` JSONL resolver and
  `internal/discovery` Claude path readers: remove only once model and history
  are sourced from OMP/session events or declared nonessential.
- `internal/mcpserver/turn_evidence.go` must treat every OMP provider as an
  event-stream backend, not wait on a phantom Claude JSONL. Keep OMP event
  and spool-backed evidence; remove vendor-only transcript assumptions.
- `internal/fleet/claude_trust.go` preaccepts Claude Code's CLI trust modal;
  remove if OMP has no such modal, without widening workdir trust policy.
  `internal/mcpserver/codex_receipt.go` and sandbox/receipt tests may encode
  app-server behavior; replace only with an OMP receipt oracle, preserving
  the uncertainty/delivery invariant. Grok MCP reconnect CLI commands and
  Cursor ACP guards are candidates for deletion when OMP replaces them.
- Generic chatlog, transcript pagination, billing/plan analytics, queue
  records and spool-backed replay are not vendor CLI transport and survive.
  Historical incident docs can remain explicitly historical.

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
