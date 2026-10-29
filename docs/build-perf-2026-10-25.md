# Verification performance audit — 2026-10-25

The full verification loop is too slow for routine feedback. Existing records
identify one avoidable ten-minute negative test; no new full suites were launched
for this audit, and no checks have been weakened or moved.

**Owner policy:** mutation testing runs in the final regression pass, not during
the development cycle. Intermediate edits use focused behavioral/contract tests.
The full mutation gate remains required for the assembled final candidate.
The owner also permits abbreviated Claudia release checks during this Jevons
development cycle, since Claudia has only this consumer: focused changed-contract
tests and a clean build, with the full regression deferred to final integrated
acceptance. Report the narrower evidence honestly; release authorization remains
separate.

## Recorded baseline

| Check | Wall time | Evidence |
|---|---:|---|
| Claudia full clean gate, bb1391e | 17m30.3s | 0130553f, GREEN |
| Jevons full clean Go suite, 7ec255d4 | 6m12.3s | f9776466, RED |
| Composed control/seat/store package race | 16.4s | d1d6bbcf, GREEN |
| Built-service restart journey, Je36e0cb88 / Clcd3af08 | 51.9s | 81b88198, GREEN |

Gate JSON start/end timestamps determine wall time. Package/test durations overlap
and must not be added to estimate elapsed time. The Jevons suite's failure remains
a failure; timing it does not certify correctness. These are different commands
and revisions, not a before/after speed comparison.

## Hotspots

1. **Claudia T39 negative control: 599.506 seconds.** The retained mutation log
   records `TestGoalJourneyUnfinishedMultiCycle` passing in 1.090s unmodified,
   then failing with one send instead of three after 599.01s under the
   `drop-host-continuation` mutation. This is about 57% of the entire full gate.
   `goal_journey_test.go`'s `waitBackendSends` waits on
   `wallclockguard.UntilTestTimeout`; `scripts/check-mutation-evidence.py` runs
   each selected test without a separate timeout, inheriting Go's ten minutes.
   The negative is detected correctly, but far too slowly.
2. **Repeated mutation launches.** The standing gate runs baseline/mutant tests
   and then a separate proof that the mutation check itself detects decay.
   Those are different guarantees. The launcher runs individual `go test`
   processes serially, with repeated package build/link overhead. No measured
   saving is claimed for batching, and mutations must remain isolated.
3. **Large Jevons test packages.** In the retained full output, docratchet took
   173.324s, mcpserver 158.725s, watchdog 104.444s and jevonsd 91.394s. These
   package times overlap. Structural checks also build/analyze product binaries;
   indiscriminate extra parallelism risks repeating the recent process-exhaustion
   incident. There is no evidence here that removing CGo, race detection or clean
   checkout provenance is safe.
4. **Orchestration waste.** A startup-test worker continued expanding simulated
   restart scaffolding after the actual built-service restart journey passed.
   Its remaining acceptance was resliced to two accounting modes and two seats.
   This wasted implementation time is separate from suite execution time.

## Proposed first fix

Give the goal-continuation test a deterministic completion/observation boundary
that detects the missing continuation promptly. Verify both the unchanged test
and the actual mutation through the existing standing mutation checker. Preserve
the healthy asynchronous continuation case; do not substitute an arbitrary short
wall-clock deadline or shorten a production watchdog. Target: 🎯T1225.

This negative-control optimization is deferred from the current development
cycle. At final regression, measure it on the same dependency graph if changed.
Consider explicit timing
output per mutation and narrowly batching equivalent runs. Keep full final
acceptance enforced. Use the existing focused gates during edits, and reuse
already-passing live results when subsequent changes do not affect their path.

## Method and limits

Read the Makefiles, mutation runner, goal test and existing gate JSON/logs. Raw
Claudia mutation evidence:
`/var/folders/fn/8ffdcmbd13lfwzsm2h93xrx00000gn/T/mutation-evidence-bc_gm126.log`.
Jevons full output:
`/tmp/jevons-t1074-fullgo-f9776466-failures.log`.
Gate records reside under `~/.jevons/gates`.

No new clean/no-op/one-file build profiling, CPU profiling or performance fixes
ran. Their timings and any improvement remain unmeasured. The active cd3af08
full gate is allowed to finish; its result is separate from this audit. Source
and test changes require the usual independent review and exact gate evidence.
