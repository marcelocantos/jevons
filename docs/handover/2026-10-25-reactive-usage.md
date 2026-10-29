# Reactive subscription usage — 2026-10-25

## Owner intent

Claudia's scheduled pollers persist observations; the existing shared sqlpipe pair
replicates them; one sqldeep subscription produces the complete browser JSON.
WebSocket transport frames those bytes without assembling another snapshot.
Opening, focusing, reconnecting or reloading the browser does not collect usage.

## Local implementation and activation

The first coordinated local builds were activated at approximately 20:50 Melbourne.
The final native-watch refinement was activated at approximately 21:05:30.
No push, release, version bump or remote merge was performed.

| Component | Commit | Source worktree |
|---|---|---|
| Jevons | `e9c537afb70abba1d2a2aef63a3a30e50c1b3971` | `/tmp/jevons-sqlpipe-plan-usage-20261025` |
| Claudia | `ae62fa8d4235ed2b2b1d01a99af0b33f6713e09b` | `/tmp/claudia-sqlpipe-plan-usage-20261025` |
| sqlpipe Go binding | `f8601ec2c729694343f9d82090ba36bda1fcb307` | `/tmp/sqlpipe-sqldeep-subscription-20261025` |

Jevons commits `5c41f8bf`, `66f09d60`, `d2816763`, and `e9c537af` compose on the previously
running `662b7421` baseline. Claudia composes on previously running `83ca3cc8`.
These are isolated integration branches, not the shared checkout's current HEAD.
External modfiles `/tmp/jevons-sqlpipe-consumer.mod` and
`/tmp/claudia-sqlpipe-plan-usage.mod` bind these committed sibling sources.
Published dependency pinning remains for the eventual release; there are no
tracked local-path go.mod replacements.

Supervisor pins immutable local installs:

- `/Users/marcelo/.local/lib/jevons/owner-sqlpipe-e9c537af/bin/jevonsd`
- `/Users/marcelo/.local/lib/claudia/owner-sqlpipe-ae62fa8d/bin/claudia`

Each install has source and an `activation.json` with commits and binary SHA256.
The exact dependency source is archived under
`/Users/marcelo/.local/lib/sqlpipe/owner-sqlpipe-f8601ec2`.
The Jevons supervisor command explicitly pins both executable paths, so a later
shared-checkout build cannot silently replace either running binary.

Consistent pre-activation control database backups and previous/next supervisor
configs are in `/tmp/jevons-sqlpipe-activation-20261025` (private scratch).
Rollback uses the previous coordinated binary/config pair. Do not restore a
backup database underneath a running daemon. Old history and override files
remain untouched; their import into local replica tables is transactional and
marked once. New observations and history belong to Claudia's control DB.

## Behaviour

- Collection reserves its next attempt durably before network I/O. Current
  outcome, last successful reading, history and retry timing commit atomically.
  Failed reads retain the prior successful values and carry a distinct error.
- Normal success interval is five minutes. Refusals use bounded durable backoff;
  other errors retry normally. Test state is owned by the broker instance.
- The sqldeep query joins replicated observations/history with Jevons-local
  overrides and migrated historical readings. HTTP and mux send its JSON intact.
- Browser delivery is independent of orchestration policy execution.
- One database-lifetime native QueryWatch serves both local and replicated
  changes. SQLite session changesets identify net committed table changes;
  unrelated tables and no-op writes do not evaluate the query. Same-table
  changes may reevaluate; native result hashes suppress unchanged payloads.
  There is no Claudia query refresh loop or per-link re-registration.
- Native diff application is atomic (one transaction); the shared database
  mutex excludes concurrent queries during application. Extra synchronization
  flags were unnecessary and removed.
- sqlpipe dependency discovery now includes SQLite authorizer reads even when
  its relational analysis partially succeeds, fixing missed scalar-subquery
  tables (🎯T39). QueryWatch also rejects runtime SQL errors and only advances
  batch hashes after every query succeeds (🎯T40), preserving retry state.
- Browser freshness derives from observation timestamps. Last-known readings
  remain visible but neutral when unavailable or expired. A presentation clock
  ages them even if the connection is silent; it does not collect usage.
- Removed the competing Jevons collector, shared-file observation cache,
  Cursor raw-body augmentation and browser collection triggers.
- Retired `JEVONS_PLAN_USAGE_FIXTURE` fails before broker initialization rather
  than silently falling through to live provider collection.

## Evidence

Clean gates (records in `/tmp/jevons-sqlpipe-usage-gates`):

| Evidence | Gate |
|---|---|
| sqlpipe subscription transpilation | `5b12ba95` |
| Final native dependency, transaction, runtime-error, retry and transport checks | `70bd2d3e` |
| Final Claudia native-watch/socket/offline/reconnect checks | `83e9da87` |
| Claudia scheduled failure/recovery, durable pacing, actual socket replication/reconnect and local invalidation | `feb623c9` |
| Final Claudia clean binary build | `56553b37` |
| Jevons full web suite: 762 passed, 48 pre-existing skips | `c0900f59` |
| Jevons final clean binary build | `2ed4c74f` |
| Final clean tracked-bundle check | `f0c28828` |
| Final clean real browser/sqldeep/HTTP/mux failure, recovery and blackholed-socket reconnect | `d972d4a7` |

The final browser test took 35.95 seconds. It asserted no navigation on recovery,
no forced collection requests, neutral retained last-good values, and receipt of
a distinct new reading after reconnect. It used synthetic observations through
the production projection and transport: it proves transport behaviour, not
provider authentication reliability. Focused backend race checks also passed.

The query measured about 73 ms over a copied 86,081-row legacy history in the
first composition; the later history-filter variant measured about 95 ms on the
legacy-only corpus. Neither is a production latency guarantee or proof of a
speedup. The new regression instead proves that out-of-period canonical rows
are excluded before JSON expansion, and that candidate/dedup lookups use indexes.
No-reset or missing-duration windows retain the full-history fallback because
there is no sound period boundary to infer.

Upstream sqlpipe 🎯T38 records an assertion abort when EXPLAIN QUERY PLAN is
prefixed to the sqldeep object query. Normal execution succeeds; the query-plan
check uses the equivalent ordinary-SQL CTE prefix. No upstream parser fix is
claimed here.

Immediately after activation, Claude, Codex, Grok and Cursor all reported
available. Their exact success timestamps matched in Claudia's control DB,
Jevons's replica and the HTTP snapshot. Bedrock explicitly reports that it does
not expose this subscription-remaining surface.

Passive live browser observation on the first deployed composition passed at
21:00:50 Melbourne: all four providers advanced from the 20:55 collection to
the 21:00 collection over mux, zero forced-refresh requests and exactly one
document navigation (initial load). Evidence is in
`/tmp/t1224-live-evidence-initial.json`. Same-document URL changes are excluded
from the navigation count; an earlier harness incorrectly counted those as
reloads and failed before this corrected observation. This proof predates the
final native QueryWatch refinement and must not be cited as that build's result.

## Remaining scope

🎯T1224 remains converging. Its original authentication evidence child 🎯T1224.1
is not retired by a transport test. 🎯T1224.2 records migration of the older
fixture-driven journeys onto canonical replica observations before the final
broad regression. Full/mutation/live-auth suites were not claimed for this
intermediate build. Imported legacy history has no recorded historical band;
those points remain neutral instead of inventing one.

Do not restart the broad 🎯T1074 cleanup goal from this handover alone. The
owner's immediate request was the reactive usage pipeline and local recovery.

The dependency bundler could not run because its submodules are absent in the
isolated checkout. The narrow native changes were mirrored identically into
tracked dist/Go/Swift copies without replacing unrelated existing differences.
The Go production path was compiled and exercised by the final clean gates.
Swift compilation was not claimed. Canonical sqlpipe ledger records 🎯T38–🎯T40
remain in that repository's already-dirty shared checkout; no other source
changes there were staged or overwritten.

## Final activation observation

At 21:05:30 the final builds replaced the first composition. The existing
headless browser reconnected and received the unchanged 21:00 readings first;
the durable next-attempt times were preserved. The next normal collection
arrived over mux at 21:06:34–35 for Claude, Codex, Grok and Cursor.

The cross-deployment observer asserted one document navigation and failed
because it counted two: initial load plus the existing 🎯T993 reload when the
server build identity changed (`ui/src/mux/client.ts`, `noteServerBuild`). It
made zero forced-refresh requests. Do not cite this run as a zero-reload pass.
The final clean isolate browser gate `d972d4a7` independently passed no-reload
failure/recovery and blackholed-socket reconnection. A whole additional live
collection cycle on the final unchanged build was not waited out.

Final supervisor status: Claudia PID67899, Jevons PID68326. Both were RUNNING.
The final source changes are committed on their isolated branches; published
dependency pins and integration into the shared broad-cleanup branch remain
separate from this locally activated intermediate build.
