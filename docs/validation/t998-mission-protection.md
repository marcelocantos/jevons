# T998 mission start protection

The daemon counts work-seat start admissions by repository and target in a
rolling window. The default is 15 starts in 24 hours. The 2026-10-03 baseline
had approximately 219 workers: jv-t766.2-worker was rank 1 at 18 starts (~6.2
standard deviations), the only seat at or above 15 starts. Its context integral
was 48.9 MB (~10.5 standard deviations), maximum message snapshot 2.25 MB (~6.6
standard deviations). These are historical observations, not a fitted statistical
model or billing amounts. Overseer and product-owner populations stay separate.

`state_dir/mission-protection.json` may set positive integer `max_starts` and
`window_hours`; restart to apply. The default catches the incident before the
18th start without limiting a long-running seat's turns. `force_engage`, worker
renaming, a new linked worktree, and T753 reopen do not reset the target count.
Both the manual/PO start handler and unattended frontier launch reserve capacity
before registering a replacement. Failed launches release their reservation;
a crash retains it conservatively. Historical successful lifecycle starts seed
the store on first installation. Admissions after installation are durable in
`state_dir/fleet/mission-starts.json` independently of lifecycle logging.

Once the bound refuses a start, one parent notice per target/window names the
seat, target, count, rank, population, and sigma. This online statistic compares
target start counts, not the historical per-seat baseline. It logs the same
structured evidence. The notification claim persists across daemon restarts;
delivery errors are logged and escalated through fleet-health. Like other
at-most-once notices, a crash between the durable claim and delivery can lose
that notice; the refusal remains visible on every attempted start.

An explicit `jevons_agent_start` with `remint_override=true`, `override_reason`,
and `actor=owner` or the configured overseer authorizes **one** extra start.
That target-scoped intent records actor, reason, seat, timestamp and reservation
in the durable store and lifecycle journal. A PO's `owner_asked` or
`force_engage` does not grant this privilege. Actor identity uses the existing
fleet control trust boundary (self-attested MCP actor), not new authentication.
An override does not unpark a target or override unrelated admission checks.

The bound covers replacement starts through PO/manual and unattended frontier
paths. Existing-process reattachment, ordinary sends/resumes, and provider
migration are separate lifecycle operations; this change does not freeze those.
Unbound seats cannot be aggregated into a target count. Historical events with
no target/workdir cannot be retrospectively attributed and are excluded.

Journey exception: deterministic MCP/frontier tests hit the production admission
paths, prove no registry row is created on refusal, capture the single parent
notice, and exercise the privileged override. Persistence/concurrency tests
cover restart, rename, rollback, window expiry, malformed state, and historical
seeding. Provider-generated repeated remints would expend real budget without
adding a different admission path. This exception does not stand in for
activation and observation of the development daemon.

## Development observation, 2026-10-03

After parent integration and activation of the assembled implementation,
`http://127.0.0.1:13705/mcp` tools/list exposed `remint_override`. Only after
checking that schema, a tools/call request for `jv-t998-override-probe` used
`actor=jv-t998-blowout`, `parent=jv-t998-blowout`, `target_id=T998`,
`workdir=/private/tmp`, `force_engage=true`, `remint_override=true`, and a
nonempty probe reason. It returned `isError=true` with:

> T998 start refused: remint override requires owner/overseer actor and a reason

GET `/api/agents` before and after contained zero rows named
`jv-t998-override-probe`. The durable mission-start store existed. No provider
seat was launched by this probe. The all-package clean gate for mcpserver,
missionbound and missionmeter is `c7742213` at `2004853ec1a7`; the daemon build
is `a3e26093` at the same commit. The package gate used
`BULLSEYE_DATA_DIR=/private/tmp/jv-t998-bullseye-tests` for the sandbox-writable
Bullseye test cache. These observations cover the activated admission guard;
threshold crossing, single-notice behavior and attributed override persistence
are covered by the hermetic production-path tests described above.
