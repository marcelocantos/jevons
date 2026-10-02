# T997: nested heavy gate lease

The sanctioned command `bin/gate -- make test-web-clean` acquired
`heavy.lock` twice: the CLI's `gate.Run` held it while waiting for make;
make launched `scripts/test-web-clean`, whose `gate.RunClean` called
`gate.Run` for `make test-web` and waited for the same lock.

`TestT997NestedHeavyGate` reproduces the process boundary with an outer
gate, make, an inner gate, and a leaf make target, sharing a private store.
Before the fix, both success and failure cases timed out after eight seconds
and required process-group cleanup (gate record `576de9b4`, RED). The inner
gate invocation was printed but its leaf command never ran. The sandbox
denied `ps`, so process-tree inspection was unavailable.

Each acquisition now writes a fresh random capability to the locked file
and passes it only in the gated child's environment. A nested gate may
reuse the lease only when its inherited capability matches the current
record and the lock is held. It never unlocks the ancestor's lease.
Unrelated invocations acquire the real flock. Every new holder replaces
the capability, including holders using the non-reentrant public API.
The environment survives make and Go exec wrappers; a bare inherited file
descriptor would not survive the latter.

The capability is invocation plumbing in a trusted local environment,
not an authorization boundary: deliberately copying a current token into
another invocation delegates that lease. Do not set it in a shell profile.
Parallel descendants share their outer invocation's lease; independent
invocations continue to serialize.

Verification:

- `TestT997NestedHeavyGate`: both gate records survive success and failure.
- `TestT997NestedReleaseKeepsOuterLock`: an independent nonblocking flock
  still fails after the inner release.
- `TestT997InvalidTokensStillQueue`: absent, forged, stale, and other-store
  tokens wait until the current holder releases.
- `TestT603HeavyLeaseSerialisesConcurrentHolders` and
  `TestT603RunSerialisesConcurrentHeavyRuns`: unrelated runs serialize.
- Run `bin/gate -- make test-web-clean` against the committed fix to
  exercise the actual Go helper and clean-worktree path.

Journey exception: this is CLI gate infrastructure with no agent or chat
behavior change. Process-level regression tests and the actual clean-web
command cover the failure boundary; an agent journey adds no coverage.
