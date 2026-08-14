Now T380's acceptance.One real gap remains: my gate ran on a candidate tree based at `5d42053`, and two commits have landed since. Re-running the T380 oracles at current HEAD in a clean worktree.**🎯T380 — in progress; the detection landed, one item remains and it is not mine to decide.**

**Landed (mine, do not re-run):** `9f93f16` *"an idle product owner on a kickable frontier is a fault, not sleep"* — proven reachable: `git merge-base --is-ancestor 9f93f16 HEAD` passes against current `c97d187`. 1049 insertions across `internal/pofanout/` (pure classification), `internal/mcpserver/po_fanout.go` (sentinel sample), `internal/staffops/observe.go` (signal shaping), plus three test files.

**Gate evidence**

| gate | verdict |
|---|---|
| `GATE t380-candidate-gotest exit=0 GREEN id=31800369` | full `go test ./...` on the scoped candidate (1m16.1s) |
| `GATE t380-prefix-red exit=1 RED id=7ff7f677` | same oracles with the wiring removed fail on their **own assertions** (`--- FAIL: TestSentinelPOIdleOnReadyFrontierIsFault`), not on compilation |
| `GATE t380-control-runlock-head exit=0 GREEN id=5ce9c558` | clears the earlier runlock suspicion as unrelated |
| `GATE t380-head-oracles exit=0 GREEN id=71f69c98` | **new this turn** — the oracles re-run at current HEAD, since my candidate was based at `5d42053` and three commits have landed since |

**Acceptance, criterion by criterion**

1. **Detection** — met. `po_fanout_stall` signal, classified `file+PO`, named on the wire report *and* written to the eventlog via `logLifecycle(compSentinel, "po_fanout", …)`, so the evidence outlives the scroll.
2. **Sleep vs failure, from the ledger** — met. Gating comes from the same `poproactive.LeafObs` set the frontier-consume sweep builds; legitimate sleep is exactly `poproactive.Sleep` over it. Name shape only decides which registry rows are POs at all, and `targetfile.SameLedger` keeps a foreign-repo PO out.
3. **Answered vs dropped the order** — met. A turn is read as a phase excursion away from idle and back; `turn_no_fanout` (critical) is strictly stronger than `stalled` (high) — the PO was demonstrably awake and spawned nothing.
4. **Hermetic fixtures, red pre-fix** — met. The three named fixtures plus mid-turn, dead, foreign-ledger, within-grace, and the PO that answered; RED demonstrated by `7ff7f677`.
5. **Local master only** — met. No push, no PR.

**Daily path (🎯T194):** the daemon is serving on `:13705` (HTTP 200), the running binary contains `po_fanout_stall`, and a live `jevons_sentinel_cycle` returns through the new sample path. It reports **no** fan-out fault — `idle_po=3`, frontier depth 41, no `po_fanout` record in the eventlog — which is the correct verdict, not a silent path: the frontier-stall reading agrees there are no unattended ready leaves, so all three POs classify `sleep_ok`. **Declared residual:** a live *positive* emission has not been observed on the daily fleet, and I will not manufacture one there; the fault branch is proven only hermetically.

**What remains:** the ledger. `🎯T380` is still `status: identified`, and my commit did not touch `bullseye.yaml`. Achieve is the overseer's call as the independent gate (🎯T31.1 — attestation ≠ execution), so I have left it. Say the word and I will file the achieve.

One thing worth your eye, not mine to fix: the shared index currently stages my four T380 files as **deleted** (`D internal/pofanout/pofanout.go` et al. in `git status`) — stale index state from before my private-index commit, exactly the 🎯T377 family. Harmless while everyone commits `--only <their paths>`; a bare whole-index commit would revert T380, and the `pre-commit` hook is what stands between those two outcomes.