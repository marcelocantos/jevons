# T1002: heavy lease lifetime and diagnosis

The T997 source already opens the flock file close-on-exec and passes only
an environment token to commands. It does not export a lock descriptor via
`ExtraFiles`. The incident's Python FD3 is therefore evidence of an existing
retained descriptor, not proof that this source exported it. This change does
not unlock, delete, replace, or bypass that descriptor's lock.

Previously, matching `JEVONS_GATE_HEAVY_LEASE` against the locked file was
enough to reuse a lease. A dead gate's token could consequently authorize
reuse while a non-gate wrapper retained the descriptor. Now each acquisition
also owns a close-on-exec loopback responder. Reuse requires the matching
token and a response from that acquisition. Closing or abruptly exiting the
gate removes the responder; stale file text and PID reuse cannot supply it.
The token is trusted invocation plumbing, not an authentication boundary.
Old two-part tokens cannot authorize new-code reuse. Mixed-version nesting
under an old running outer gate must wait for its lease to end; upgrade the
outer invocation too, rather than replacing a nested executable mid-run.

The responder has bounded request size and time, and no per-request goroutine
fan-out. A delayed response is retried with the kernel lock so transient host
load cannot permanently deadlock an otherwise valid nested invocation.
Unrelated gates still contend on the original flock inode. No alternate
admission policy, force-unlock, timeout-based takeover, or stale-PID unlock
has been introduced. T958's narrow-run admission remains separate.

`gate queue` reads the acquisition metadata (PID, command, cwd and elapsed
acquisition age), checks its responder, and inspects actual open descriptors
with lsof. For every descriptor process it requests cwd and full command /
process age. It distinguishes recorded acquisition from observed openers:
an open FD alone is not proof of exclusive ownership. Missing permissions
or inspection tools produce explicit incomplete-inspection output. Metadata
uses atomic write-and-rename; malformed metadata is an error.

## Oracles and bootstrap

- `TestT1002WrapperCannotRetainNewLease` uses private subprocesses and a
  temporary store. Its legacy branch deliberately hands a locked description
  to a surviving wrapper: stale-token reuse must wait until that fixture exits.
  Its current branch leaves a wrapper alive without exporting descriptors:
  an independent waiter blocks while the owner lives, then acquires after the
  owner exits without running deferred cleanup.
- `TestT1002OwnerVisibility` checks command/cwd/age and responder visibility.
- The T997 process-boundary nesting tests retain both success/failure records
  and prove inner release cannot unlock the outer lease.
- The T603 serialization tests and entire `internal/gate` package remain
  the regression net, including the CLI tests that build `cmd/gate`.

The development lease is stranded and is not a usable bootstrap oracle.
Build the gate with `GOCACHE=/private/tmp/t1002-go-cache`, then run the bounded
package gate with `-store /private/tmp/t1002-gate-evidence`. Its inner fixture
stores are separate temporary directories. This authorized isolated
reproduction does not claim admission through the development lease or
duplicate its queued suites. Commit verification uses `-clean`; exploratory
DIRTY runs are not commit evidence. Gate records stay in that explicit store
and can be read with `gate -store /private/tmp/t1002-gate-evidence show ID`.

Journey exception: this changes CLI gate coordination, not chat or agent
behavior. Private real-process oracles directly exercise flock retention,
owner exit, nesting and serialization; an agent journey adds no coverage of
that boundary.

## Incident boundary

Read-only observation with the new CLI still found token PID 41134 with no
responder, Python PID 51846 FD3 with cwd `/private/tmp`, and gate waiters.
The sandbox denies `ps`, so full command and process-age inspection is
explicitly incomplete here. No existing holder or waiter was signalled.
Existing retained descriptors require coordinated incident disposition after
integration; an emergency release requires owner order. This fix cannot
retroactively close a file descriptor in a different running process.
