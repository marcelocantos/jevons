# OCT-01 through OCT-10 call-site map

Source: `docs/audits/entropy-audit-2026-10-16.md`. Pair under inspection: Claudia `2b10c121cf4c3b43c9550c34a9d1bf301c373a82`, Jevons `72fa9ae545db12e2545204aed0f9d458f26e1d8f`. This map names the production seam, the regression that exists, and the limit that keeps the finding open. It does not close OCT-01 through OCT-10, and it does not achieve 🎯T1074.

## OCT-01 — Known no-action notices still authorize expensive overseer turns

- Seam: `Server.SweepSendBacklogs` in `internal/mcpserver/send_backlog.go`, called from `Server.Reconcile`. The quiet-hold regression is `TestT952Occurrence9edf22e9b2a6StaysQuiet` in `internal/mcpserver/held_status_test.go`.
- Residual: the isolate journey was not run, and no owner exception exists. 🎯T952 stays unachieved.

## OCT-02 — The default execution engine drops usage at both consumer boundaries

- Seam: Claudia `controlplane.PublishUsageFacts` and Jevons `TestT1074SqlpipeUsageAndReceiptReplication` in `internal/cost/sqlpipe_replication_test.go`. A second apply of the replicated facts keeps the canonical totals.
- Residual: no bounded live-provider check. 🎯T1074.2 stays unachieved. The committed Jevons module still requires Claudia `v0.53.0`.

## OCT-03 — Recovery rate limits do not bound authorized automatic work

- Seam: idle-nudge, fleet-recover, and repressure reserve the ledger before they act. Regressions include `TestT1074IdleNudgeReservesBeforeDispatch` and `TestT10749ReconcileDoesNotResetAllowance`.
- Residual: the shared sqlpipe grant is not what Claudia and the sidecar enforce on every provider request. 🎯T1074.5 stays blocked on 🎯T1074.2 and 🎯T1074.3.

## OCT-04 — Reconcile orders actuators but does not yet own every transition

- Seam: `Server.Reconcile` in `internal/mcpserver/reconcile.go`. Callers: the cockpit hook in `cmd/jevonsd/main.go` and the sentinel repair in `internal/mcpserver/sentinel.go`. Jevons `72fa9ae545db12e2545204aed0f9d458f26e1d8f` holds `reconcileMu` for the whole pass. `TestT7663ReconcilePassesDoNotOverlap` drives `Reconcile` and fails if a second pass enters while the first holds the lock.
- Residual: the actuators still decide. `StartFrontierConsumeLoop` and `StartIdleNudgeLoop` still start from `cmd/jevonsd/main.go`. Replaced loops were not deleted. 🎯T766.3 stays blocked on 🎯T766.4 and 🎯T1074.5.

## OCT-05 — Delivery still guesses request identity from transcript content

- Seam: `deliverToSenderModeWithRequestID` in `internal/mcpserver/agent_send.go`. Jevons `3eaa9e48e269a961f787554c7e3de6461ec25bc4` settles a bound confirmHere handoff before the caller status. An unknown receipt returns `delivered_unconfirmed`.
- Residual: `confirmByCaller` still returns `sent`. Escalation still returns `steered` on a nil write. `reportSendOutcome` still reports caller `sent` from transcript evidence on the paths that fall through. 🎯T1074.3 stays blocked on 🎯T1074.9 and 🎯T1074.10.

## OCT-06 — Cursor's second coding-tool path bypasses the common sandbox

- Seam: `sidecar/seat.ts` installs both `codingTools` (`SandboxPolicy`) and `cursorExecHandlers` from `sidecar/cursorexec.ts`. `seat.ts` does not call `createOmpSession`.
- Residual: the Cursor native executors and the shared sandbox are still two paths. This slice did not run one corpus against both.

## OCT-07 — The standing verification contract no longer matches the shipped runtime

- Seam: Claudia `Makefile` target `gate-checks` depends on `test-sidecar`. `scripts/gate-attest.sh` is still the attestation script.
- Residual: no fresh `make gate` was run on Claudia `2b10c121cf4c3b43c9550c34a9d1bf301c373a82` or on Jevons `72fa9ae545db12e2545204aed0f9d458f26e1d8f`.

## OCT-08 — Full-context diagnostic snapshots amplify storage and obscure the event contract

- Seam: `sidecar/seat.ts` still emits `turn_end` with `snapshot: agent.state`. `sidecar/spool.ts` `Spool.append` writes a fixed field list and does not persist that snapshot. Jevons `internal/spool/turn.go` does not decode a `turn_end` snapshot.
- Residual: the live turn-end event still carries the agent state. Per-turn byte bounds were not benchmarked.

## OCT-09 — Generic Claudia still carries Jevons defaults and a second execution architecture

- Seam: public `resolveCodexBin`, `resolveGrokBin`, and `resolveCursorBin` return `ErrLegacyCLISeatTransport`. Task dispatch returns `ErrTaskCLIRemoved`. Claudia `2b10c121cf4c3b43c9550c34a9d1bf301c373a82` removed the legacy locator helpers. Regression present in the tree: `TestT1055FleetSpawnUsesOMPOnly`.
- Residual: 🎯T1055 was not achieved by that purge. No clean full suite and no `make gate` were run for it. The three personal `openai-codex` seats were not deleted.

## OCT-10 — Verification and doctrine are too tightly coupled to individual incidents

- Seam: `scripts/docratchet` remains the standing documentation and clean-checkout ratchet. Incident classifiers such as the reap prose fallthrough in `internal/mcpserver/reap_done.go` are still product controls.
- Residual: wording checks and incident classifiers were not separated. A lower substring census is not acceptance. This finding stays open.
