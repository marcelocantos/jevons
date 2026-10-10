# Target safety-hold authority (T1050 / T1051 seam)

`NewStore(stateDir + "/landingauth")` is the **single** authority for a
canonical absolute repository path + target. `Snapshot(repo,target)` yields
`{Epoch,Held}`; `AcceptHold(repo,target,typedHoldID,reason,at)` persists a
new epoch and Held=true; `ReleaseHold(repo,target,verifiedPOEventID,at)`
persists a second new epoch and Held=false. Only the caller that verified the
current registered PO-process event may release; the event ID parameter is
an audit identifier, **not authentication**. A typed `sendq` hold alone is a
message and is not yet a policy hold: T1051 must route a separately authenticated,
repo-and-target-scoped hold into `AcceptHold`; never infer target from prose.

The grant issuer snapshots only after verifying a genuine PO process event and
refuses to mint while Held. The landing service obtains the shared-ref landing
lock first, precomputes the bounded merge, then calls
`WithCurrent(repo,target,grantEpoch, func() error { durablySpendGrant();
finalRefAdvance() })`. The callback must not call Store methods and **must
include the final ref advance**, not just spend. `WithCurrent` holds Store.mu
and an OS flock until callback returns. The lock order is landing lock →
authority Store.mu → authority flock → grant-store lock. `AcceptHold` and
`ReleaseHold` take only the latter authority locks, so no inversion. If a
hold commits first, the old grant cannot enter the callback. If spend/ref
advance wins first, the hold waits and cannot undo an already committed
landing. There is no instant at which a hold is accepted while an older grant
can still mutate a ref. The spend is durable **before** ref mutation; if the
process crashes after spend, the grant is unavailable for retry and a fresh
PO decision is required. Disk or daemon outage is refusal, not an empty epoch.

The lock serializes even unrelated target redemptions; expensive merge
computation goes outside the callback, and the bounded final ref advance
stays inside. The store is not a grant issuer, a Git write confinement
mechanism, or an owner notification. This isolated package does not claim a
running daemon or a live owner-path observation.
