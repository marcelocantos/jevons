# OCT-01 through OCT-10 call-site map

Source: `docs/audits/entropy-audit-2026-10-16.md`. Pair under inspection: Claudia `4aeba56817e76da1c8ec817615ee10a0ae1a745e`, Jevons `168adcdc38d6052928a82a0bf80553d74473c5d5`. Every landed commit named below is an ancestor of that pair. The published module graph is still Claudia `v0.53.0` and sqlpipe `v0.31.0`. This map does not close OCT-01 through OCT-10, and it does not achieve 🎯T1074, 🎯T766, 🎯T1074.1, 🎯T1074.2, 🎯T1074.3, 🎯T1074.9, or 🎯T1074.10.

## Unpublished pin

🎯T766, 🎯T1074.9, and 🎯T1074.10 stay open. `go.mod` requires Claudia `v0.53.0`. That module has no `controlplane` package and no `-mcp-seed-dir` flag. `GOWORK=off go list -m -json github.com/marcelocantos/claudia/controlplane@latest` exited 1 (`t1074-controlplane-latest.txt`). GitHub `contents/controlplane` on master is HTTP 404. Published-pin gate `id=fb5273b4` is GREEN on Jevons `ce67c7f46cae45441cae6eef32fcb004578b2b0d` with `empty=github.com/marcelocantos/jevons/internal/cost,github.com/marcelocantos/jevons/internal/mcpserver`. The real `TestT10749` bodies are behind `//go:build jevons_claudia_next` and did not run. That GREEN is not 🎯T1074.9. Gates `id=189eba8f` and `id=e2aa1469` stay RED and were not voided.

Claudia `make gate` on `23cd5169ed6c39419c04b8e45910e311262e993b` (tree `bd3eebe69c2a6b47c6f37c9d9470bc2f62e0fe90`) printed `owner gate checks failed; nothing attested` and `EXIT:2` (`t1074-claudia-gate-23cd516.txt`). `go test -race` checkptr-crashed in published sqlpipe `v0.31.0` `NewPeer` (`wrapper.go:1399`) from `controlplane.SyncLocked` (`controlplane/sync.go:44`) inside `TestT10749SyncLockedDeliversBothWaysOnce`. Nothing was published. No `replace` was committed. The test was not skipped and `-race` was not dropped. No third `make gate` was run. Unpublished sqlpipe `c902b1a788c0f4aa59c4111b9ac4169998d332ac` (parent `ab73879096dfde044dd9e5664b06052b59acc07a`) adds `TestNewPeerFileCallbackHandles`, which passed under `-race` (`t1074-newpeer-race.txt`). That commit is not the module Claudia's gate builds. Published sqlpipe `v0.31.0` still checkptr-crashes `NewPeer`.

🎯T766 stays blocked on 🎯T766.3, 🎯T766.4, and 🎯T766.5. 🎯T766.4 and 🎯T766.5 stay blocked on 🎯T1074.3. 🎯T1074.3 stays blocked on 🎯T1074.9 and 🎯T1074.10.

## OCT-01 — Known no-action notices still authorize expensive overseer turns

- Landed: Jevons `5601c9c3ba787e15d829aa2e30118bfcbc97bb10` keeps an intentional hold out of the model prompt. Jevons `6a2b4b0c7b6dd263903dcf2fd6964796570e69e7` journals each non-owner fleet-health note once. The isolate J39 ran against cleanup Claudia `2b10c121cf4c3b43c9550c34a9d1bf301c373a82`.
- Call site: `Server.SweepSendBacklogs` in `internal/mcpserver/send_backlog.go`, called from `Server.Reconcile` in `internal/mcpserver/reconcile.go`.
- Regression: `GOWORK=off go test -count=1 -timeout 180s -run TestT952 ./internal/mcpserver ./internal/server`. The prior run printed PASS for `TestT952IntentionalHoldIsStatusAcrossRestart`, `TestT952Occurrence9edf22e9b2a6StaysQuiet`, and the other `TestT952` names, and `EXIT:0` (`t952-hermetic-rereun.txt`). On this tree the same two tests printed PASS and `EXIT:0` (`t1074-oct-regressions.txt`, 0.712s). The isolate printed `ok J39-t952-hold (10m26.079s)` and `EXIT:0` (`t952-j39-allowance.txt`).
- Residual: 🎯T952 is achieved by that journey. The audit also requires a receipt-uncertainty query to reuse the same Claudia request instead of minting another prompt. A capable bound handoff now settles that question from the receipt, including a caller-owned interrupt. An incapable seat still falls through transcript evidence. See OCT-05. This row does not close that half.

## OCT-02 — The default execution engine drops usage at both consumer boundaries

- Landed: Claudia `6b90e5f9d820bade9120290032bb2f85664cfdc2` replicates usage facts and receipts. Claudia `113dfe5144cb6ba817fa9a197a2aebd5e2fe73b6` records them on the seat pump. Jevons `ea4449b1f5a8498c33909f82d29e7a19655dd20e` and `681da7cda4fd2a9e0e0c1b92f03550c170123dd2` ingest the replica once.
- Call site: `omp_agent.go` calls `RecordObservedUsage`. `controlplane.Plane.PublishUsage` calls `PublishUsageFacts` (`controlplane/replicate.go`). On the `jevons_claudia_next` build, Jevons `(*Collector).CatchUpReplicas` in `internal/cost/replica.go` reads that table. The default build is `internal/cost/replica_pin.go`: `OpenReplica` returns `control plane unavailable: published claudia v0.53.0`.
- Regression: Claudia `GOWORK=off go test -count=1 -timeout 180s -run TestPublishUsageFactAndReceiptRoundTrip ./controlplane`. Jevons `GOWORK=off go test -count=1 -timeout 180s -tags jevons_claudia_next -run TestT1074SqlpipeUsageAndReceiptReplication ./internal/cost`, and only against a paired Claudia that contains `controlplane`. The committed module cannot run it. Default `go test -race` of a `NewPeer` test checkptr-crashes on sqlpipe `v0.31.0`. That crash was not skipped.
- Residual: 🎯T1074.2 stays unachieved. No bounded live-provider dimension check is cited here. The published pin does not consume the control-plane tables.

## OCT-03 — Recovery rate limits do not bound authorized automatic work

- Landed: Jevons `a0b42862249dd6c8bdf0cbd17d60e0b82b3a6fc5` reserves the shared ledger in `IdleNudgeLedger.Reserve` before dispatch. Claudia `4aeba56817e76da1c8ec817615ee10a0ae1a745e` has no matching provider-request grant on this pair. `TestT10749ReconcileDoesNotResetAllowance` is `//go:build jevons_claudia_next` in `internal/mcpserver/t1074_9_allowance_test.go`.
- Call site: `StartIdleNudgeLoop` is started from `cmd/jevonsd/main.go`. Claudia goal continuation still lives in `goal.go`. The sidecar compaction path is separate.
- Regression: `GOWORK=off go test -count=1 -timeout 180s -run TestT1074IdleNudgeReservesBeforeDispatch ./internal/mcpserver`. On this tree the same command with `-v` printed PASS and `EXIT:0` (`t1074-oct03-nudge.txt`, 1.164s). The allowance test needs `-tags jevons_claudia_next` and a paired Claudia. It does not run on `v0.53.0`.
- Residual: the shared sqlpipe grant is not what Claudia and the sidecar enforce on every provider request. Tool activity can still reset a continuation count. 🎯T1074.5 stays blocked on 🎯T1074.2 and 🎯T1074.3.

## OCT-04 — Reconcile orders actuators but does not yet own every transition

- Landed: Jevons `72fa9ae545db12e2545204aed0f9d458f26e1d8f` holds `reconcileMu` for `Server.Reconcile`. Jevons `ba7ae0882a484752dbeb3fd37f64c892f556a4d1` pins that a rendered steer refusal is not a finished-work reap.
- Call site: `Server.Reconcile` in `internal/mcpserver/reconcile.go`. Callers include the cockpit hook in `cmd/jevonsd/main.go` and sentinel repair. `StartFrontierConsumeLoop` and `StartIdleNudgeLoop` still start from `cmd/jevonsd/main.go`.
- Regression: `GOWORK=off go test -count=1 -timeout 180s -run TestT7663ReconcilePassesDoNotOverlap ./internal/mcpserver` and the same form for `TestT766SteerRefusalIsNotFinishedWork`. `TestT7663ReconcilePassesDoNotOverlap` drives `Reconcile` and fails if a second pass enters while the first holds the lock. On this tree both printed PASS and `EXIT:0` (`t1074-oct-regressions.txt`, 0.712s).
- Residual: the actuators still decide. Replaced loops were not deleted. English `isPromptBusy` matching remains. 🎯T766.3 stays blocked on 🎯T766.4 and 🎯T1074.5. 🎯T766 stays blocked on 🎯T766.3, 🎯T766.4, and 🎯T766.5. Full prose-heuristic removal was not done.

## OCT-05 — Delivery still guesses request identity from transcript content

- Landed: Jevons `3eaa9e48e269a961f787554c7e3de6461ec25bc4` settles a bound confirmHere handoff before the caller status. An unknown receipt returns `delivered_unconfirmed`. Jevons `1c27188191c8656a2bb4f563dd868f5a1025cf36` does the same for escalation when the bound handoff does not settle. Jevons `2140115cf22d1a63b667c3264fe1ebedb1e12cba` answers a bound confirmHere handoff from the receipt, including the interrupt backstop. Jevons `183772f129a2f3b6d25e900a74883daac2d2163e` does the same for a capable opening brief. Jevons `168adcdc38d6052928a82a0bf80553d74473c5d5` does the same when that interrupt backstop is caller-owned. The mission frozen at bind stays that mission.
- Call site: `deliverToSenderModeWithRequestID` in `internal/mcpserver/agent_send.go`. The caller-owned interrupt is the second offer after `prompt already in flight`, still inside that function. Spawn calls it with interrupt false.
- Regression: `GOWORK=off go test -count=1 -timeout 180s -race -v -run 'TestT1074CallerInterrupt|TestT1074InterruptBackstopSettlesFromReceipt|TestT1074SpawnSettledReceiptIgnoresTranscriptAbsence|TestT1074SpawnPayloadSeenDoesNotConfirmUnknownReceipt|TestT1074SpawnTimeoutSettlesFromReceipt|TestDeliverStartPromptMarksTurnBegan|TestT518QueuedStartBriefKeepsSeat|TestT518ControlProvenNoBriefStillReaps' ./internal/mcpserver`. On this tree that command printed PASS and `EXIT:0` (`t1074-caller-interrupt-receipt.txt`, 2.802s). Prior logs `t1074-caller-receipt.txt` and `t1074-escalate-caller.txt` printed PASS and `EXIT:0` against a scratch `-modfile`, not the published pin.
- Residual: an incapable or unbound `confirmHere` or `confirmByCaller` still uses the transcript watch. There is no receipt to settle, and the opening-brief tests depend on that watch. Drain of a bound entry settles from the receipt and still opens a watch it does not read. 🎯T1074.3 stays blocked on 🎯T1074.9 and 🎯T1074.10. This row does not close OCT-05.

## OCT-06 — Cursor's second coding-tool path bypasses the common sandbox

- Landed: Claudia `e27b69c254851cc3dba9a82a7f88e967da42cd4c` is the ancestor that carries the common execution policy. The design doc records full clean gate `7f1ef40d` on that commit. This map did not re-run that gate.
- Call site: `sidecar/seat.ts` builds one `sandbox` and passes it to both `codingTools` and `cursorExecHandlers`. `seat.ts` does not call `createOmpSession`. `cursorExecHandlers` in `sidecar/cursorexec.ts` takes that policy into read, write, and shell.
- Regression: `cd sidecar && bun test execution-policy.test.ts`. The test `file grants, protected roots and symlink escapes have identical effects` runs the ordinary tool, the Cursor native handler, and the Cursor pi handler against one policy and checks the filesystem.
- Residual: 🎯T1074.4 is the common tool-grant scope that gate already attested. 🎯T1074.11 stays open: production `seat.ts` is not the coding-agent SDK seat, and this map does not show Cursor exec frames for read, ls, grep, write, delete, shell, and the pi names with coding-agent built-ins off.

## OCT-07 — The standing verification contract no longer matches the shipped runtime

- Landed: Claudia `d218789fb404731fb69ea14ad28ec5be6383d7d1` is the ancestor whose `make gate` runs `test-sidecar` before `go test -race` and whose ownergate refuses a dirty start. Claudia `23cd5169ed6c39419c04b8e45910e311262e993b` commits `nativeCompactionOptions` so a frozen install passes `sidecar/omp-compaction.test.ts` (`t1074-fresh-all.txt`, 18 pass, `EXIT:0`). Claudia `4aeba56817e76da1c8ec817615ee10a0ae1a745e` removes the restart test's private deadline and links reconcile from `cmd/claudia/main.go`.
- Call site: `Makefile` target `gate-checks` depends on `test-sidecar`. `scripts/gate-attest.sh` runs `cmd/ownergate`. `internal/ownergate/run.go` runs `make gate-checks` in a fresh checkout with `GOWORK=off`.
- Regression: `make gate` in the Claudia cleanup tree. The run on `2b10c121cf4c3b43c9550c34a9d1bf301c373a82` exited 2 (`t1074-claudia-full-gate.txt`) during sidecar compaction. The run on `23cd5169ed6c39419c04b8e45910e311262e993b` exited 2 (`t1074-claudia-gate-23cd516.txt`) during `go test -race`. Focused follow-ups on `4aeba568` passed: the daemon import and restart tests (`t1074-owned-daemon.txt`, `EXIT:0`) and `TestHermeticTestsHaveNoWallClockDeadline` (`t1074-owned-wallclock-race.txt`, `EXIT:0`). Those are not a full gate.
- Residual: both full gates attested nothing. The sqlpipe `v0.31.0` `NewPeer` checkptr crash remains and blocks another honest full gate. Jevons baseline hunks still have no implementation author (`01d9006deade3a4a5f476cec76f3e53170907db5`). No full Jevons gate was run on this pair. 🎯T1074.1 stays unachieved.

## OCT-08 — Full-context diagnostic snapshots amplify storage and obscure the event contract

- Landed: Claudia `8ce350c7372fc077a5007c31a5e98330ed9ce776` keeps spool diagnostics to usage and receipt facts. `Spool.append` in `sidecar/spool.ts` does not persist agent snapshots. Jevons `b6031c211d8dbed92d7b22dfe125a06351b864c5` queries seat turns without reading the agent snapshot (`internal/spool/turn.go` does not decode a `turn_end` snapshot). Production `sidecar/seat.ts` on Claudia `4aeba56817e76da1c8ec817615ee10a0ae1a745e` still emits `snapshot: agent.state`.
- Call site: `sidecar/seat.ts` emits `type: "turn_end"` with `snapshot: agent.state` on both the refusal path and the normal path.
- Regression: Claudia `omp_agent_test.go` still requires `seat.ts` to contain `snapshot: agent.state`. Jevons `internal/spool/t870_turn_test.go` asserts a snapshot body is not classified as the turn.
- Residual: the live turn-end event still carries the agent state. Per-turn byte bounds were not benchmarked. 🎯T1074.7 stays unachieved.

## OCT-09 — Generic Claudia still carries Jevons defaults and a second execution architecture

- Landed: Claudia `2b10c121cf4c3b43c9550c34a9d1bf301c373a82` is the cleanup ancestor after the locator purge. `resolveCodexBin`, `resolveGrokBin`, and `resolveCursorBin` return `ErrLegacyCLISeatTransport`. Task dispatch returns `ErrTaskCLIRemoved`.
- Call site: `provider.go` and `task.go`. `TestT1055FleetSpawnUsesOMPOnly` in `t1055_legacy_seat_transport_test.go` calls those production refusals.
- Regression: `GOWORK=off go test -count=1 -race -run TestT1055FleetSpawnUsesOMPOnly .` in the Claudia tree. This map did not re-run it. Earlier T1055 slices on this branch recorded focused `-race` passes and left 🎯T1055 unachieved because there was no full fleet spawn and no clean full suite.
- Residual: 🎯T1055 stays unachieved. 🎯T1074.6 stays blocked on 🎯T1055, 🎯T1074.9, and 🎯T1074.10. The three personal `openai-codex` seats (`pimp-arrai-779-native`, `pimp-xbnf-phase0`, `pimp-vellum-housekeeping-katex`) were not deleted.

## OCT-10 — Verification and doctrine are too tightly coupled to individual incidents

- Landed: `scripts/docratchet` is still the standing documentation and clean-checkout ratchet. `internal/mcpserver/reap_done.go` still classifies finish prose, including the steer-refusal pin at `ba7ae0882a484752dbeb3fd37f64c892f556a4d1`.
- Call site: `TestT766SteerRefusalIsNotFinishedWork` drives the reap classifier. `scripts/docratchet` walks instruction and embed inputs.
- Regression: `GOWORK=off go test -count=1 -timeout 180s -run TestT766SteerRefusalIsNotFinishedWork ./internal/mcpserver`. The docratchet package is `go test -timeout 45m ./scripts/docratchet`. This map did not run the docratchet package.
- Residual: wording checks and incident classifiers were not separated into different gates. A lower substring census is not acceptance. 🎯T1074.8 stays unachieved. 🎯T766.4's full prose-heuristic removal was not done.
