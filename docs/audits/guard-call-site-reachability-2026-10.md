# Guard call-site reachability audit (🎯T419)

**Date:** 2026-10-14  
**Worker:** jv-t419-auto  
**Scope:** silent-failure guards whose unit tests can be green while the production caller never reaches them under the gating predicate.

## Property

> The suite distinguishes "the guard works when called" from "the guard runs in production". For any guard whose failure is silent, at least one test drives it through its real caller under the predicate that gates that caller — not by invoking the guarded function directly.

## Pinning instance (historical)

| Guard | Unit test | Production caller | Gating predicate | Outcome |
| --- | --- | --- | --- | --- |
| `handover.Store.MarkDelivered` | `TestStoreMarkDeliveredPreventsDoubleSeeding` | `fleet.SeedSuccessor` / `migrate.go` | `Claudia.Deliver == nil` (reply completion, `defaultReplyTimeout=10m`) | **Unreachable on turn-begin.** Delivered seed at 09:07:10Z stayed pending after hand-off failed 09:07:04Z. 17 records, 2 marked delivered. |

**Disposition:** path removed with in-place provider/model switch (claudia 🎯T195, commit `2815211c`). Catalogue entry `handover-mark-delivered` is `retired`; resurrection in `internal/` or `cmd/` fails the standing check.

## Living successor (enforced)

| ID | Guard | Intermediate | Predicate | Call-site test |
| --- | --- | --- | --- | --- |
| `delivery-record-on-begun` | `delivery.RecordPayload` | `Server.recordDeliveryEvidence` | `SendOutcome == OutcomeBegun` | `TestT419DeliveryEvidenceReachedThroughBegunSend` |

Store unit tests (`TestRecordAndLookupSurviveCompactionShapedWipe`) remain necessary and **not sufficient**.

## Other enforced entries

| ID | Guard | Why silent failure matters |
| --- | --- | --- |
| `delivery-claim-exclusive` | `claimRecordFile` / `linkJSONExclusive` | Same-second overwrite would drop a confirmation (🎯T749) |
| `sendq-resolve-on-drain` | `sendq.Resolve` via `drainAgentSendQueueOnce` | Unresolved attempt freezes the queue head (🎯T418/🎯T726) |
| `treeguard-ledger-discard` | `decideLedgerDiscard` via `Env.Pre` | Bare `git reset --hard` never names `bullseye.yaml` |

## Audit-only residuals (clause 3 — report, do not silently fix)

These were enumerated and left as catalogue `audit-only` rows. Promoting one requires a call-site test under its real gating predicate, not a drive-by unit test.

1. **`sendq.sanitise`** — documents the silent-rename hazard sibling to the old handover store path. Covered by store tests; no production caller found that can skip sanitise under a separate predicate.
2. **`spool.ResumeFromSpool`** — fail-closed resume is load-bearing; confirm a production resume path test under "spool present" before enforcing.
3. **Handover-era comments** in `sendq.go` still cite `handover.Store.path` as analogy — prose only; not a live guard.
4. **`delivery.Record` direct unit tests** — legitimate store tests; they must not be cited alone as proof that `reportSendOutcome` records evidence.

## Standing enforcement

- Package: `internal/guardreach` (catalogue + `CheckRepo`)
- Hermetic: `go test ./internal/guardreach`
- Production pin: `go test ./internal/mcpserver -run TestT419`
- Doctrine ratchet: `scripts/docratchet` `TestT419DoctrineAndCatalogue` (runs under `make test-go`)

## Related

- 🎯T416 / 🎯T417 / 🎯T418 — delivery family that exposed the instance (all achieved)
- 🎯T749 — exclusive evidence id claim
- 🎯T195 (claudia) — removed migration/handover
