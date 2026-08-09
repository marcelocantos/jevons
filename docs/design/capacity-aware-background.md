# Capacity-aware background work (🎯T359)

Status: **implemented** (pure policy + daemon hooks). Residuals named below.

## The problem

The fleet grew standing background cycles faster than it grew any sense of
what they cost together:

| Cycle | Target | Cadence |
|---|---|---|
| ambient research | 🎯T356 | 90m + async feed trigger |
| standing audits | 🎯T357 | its own schedule |
| RSI coach drip + retrospective mine | 🎯T243 / 🎯T353 | minutes / 6h |
| sentinel observe→classify→act | 🎯T219 | 2m |
| staff ops, frontier consume | 🎯T325.4 / 🎯T254.1 | minutes |

Each had a soft cap of its own, and none could see the others. A per-agent
cap answers "is this loop running too often?" — it cannot answer "is there
room for this tick *at all*?". So every loop ran until the 🎯T36 cost clamp
fired, which is a safety net, not a plan: by the time it trips, the budget is
already gone and owner-facing work is competing with ambient chatter for
what remains.

## The shape of the fix

One holistic admission pass, asked before each background tick:

```
budget snapshot + concurrent load  ──►  Assess  ──►  pressure + headroom
                                                        │
        ranked work queue  ────────────────────────►  Plan  ──►  admit / degrade / defer
                                                        │
        in-flight background  ─────────────────────►  Preempt  ──►  cancel advice
```

`internal/capacity` is pure — a `Snapshot` and a `Policy` in, `Decision`s out.
No clocks, no I/O, no fleet mutation. That keeps the whole policy hermetically
testable and keeps the package free of the cost/registry dependency graph. The
daemon-side `Governor` holds the in-flight counters and the sticky owner
notice; `mcpserver.CapacitySnapshot` is the single adapter from the live
subsystems into the pure input.

### Priority is explicit

| Rank | Class | Behaviour |
|---|---|---|
| 0 | `owner_turn` | never gated — the thing every other rule protects |
| 1 | `build_mission` | never gated by ambient policy (defers only on the 🎯T36 spawn halt) |
| 2 | `control_repair` | load-bearing background: keeps running while ambient defers |
| 3 | `staff_ops` | ambient |
| 4 | `audit` | ambient |
| 5 | `rsi_coach` | ambient |
| 6 | `research` | ambient |
| 7 | `experimental` | opt-in residual ambience — and where unknown class names land |

Unknown names normalize to `experimental` on purpose: a new ambient loop that
forgets to declare itself must not silently outrank research.

### Degradation is graded, not binary

| Pressure | Trigger | Effect |
|---|---|---|
| normal | headroom ≥ degrade line | everything runs in full |
| elevated | headroom < 40%, or a `warn` cost alert | ambient runs at the reduced tier (research halves lookback and mining bounds) |
| tight | headroom < the 20% owner reserve, or `throttle` | load-bearing background only; the rest defers |
| critical | spawn halted, zero headroom, or `pause`/`kill` | owner and Build work only + one sticky owner notice |

Deferral is a skipped tick, not a queue: these loops are level-triggered, so
the next admitted tick sees everything the skipped one would have.

## Composition with what already exists

- **🎯T36 cost clamp** still owns enforcement. Capacity reads its snapshot
  (`SpawnHalted`, highest alert level) as *input* and stands work down
  *before* the clamp has to act.
- **🎯T137 subscription honesty.** When accounting is `subscription`, USD is
  an API-equivalent estimate: `Assess` reports cost headroom as **unknown** and
  it can never deny work on its own. Tokens and concurrent load stay binding in
  every mode — tokens are counted, not priced. `cost.Store.SpentTokens` is the
  honest lever this needs.
- **🎯T325.2 portfolio.** Provider soft caps supply load headroom: one pinned
  provider is real pressure even when the fleet-wide session count looks calm.
  Routing (*which* provider) remains the portfolio's job; capacity only decides
  *whether*.
- **🎯T291 owner chat not starved.** An owner turn in flight defers ambient
  work (load-bearing repair excepted).

## Owner surfaces

- `GET /api/capacity` — pressure, headroom by dimension, per-class in-flight
  counts, and what each background class would be granted right now.
- `jevons_capacity_status` — the same picture for the overseer, which is how
  "why did research go quiet?" gets answered without reading logs.
- `~/.jevons/capacity.json` — durable policy: `daily_token_budget`,
  `owner_reserve_fraction`, `degrade_fraction`, `max_concurrent_background`,
  `max_per_class`, `load_bearing`, `disabled`.
- A **sticky** owner notice (broadcast + eventlog + overseer) fires once when
  background parks and once when it resumes — latched, not per-tick.

## Oracle coverage

Pinned (hermetic, `go test ./internal/capacity/ ./internal/mcpserver/
./internal/research/ ./internal/server/ ./internal/cost/`):

- ranking, admit/degrade/defer at every pressure, load-bearing exemption
- subscription accounting never denies on USD; tokens still bind
- provider soft cap saturation drives pressure
- token reserve defers an oversized ambient pass; owner work is never measured
  against the background allowance
- holistic `Plan` fills slots highest-rank-first regardless of caller order
- `Preempt` cancels lowest-rank preemptible work first, spares load-bearing at
  tight, spares non-preemptible always
- governor slot hold/release (including double-release), sticky notice fires
  once and clears once, serialisable status
- the adapter carries budget/load/clamp state and picks the *most severe* alert
- a deferred research tick runs nothing and records the skip durably; an
  admitted tick runs and releases its slot; owner-invoked cycles bypass the gate
- `GET /api/capacity` serves the real status; unwired reports disabled

Residual (deliberately not pinned):

- **Exact vendor quotas are class-3.** `daily_token_budget` is unset by
  default, and unset means *unknown*: token headroom reports unknown rather
  than inventing a ceiling the owner never agreed to. Until the owner sets it,
  admission runs on USD (when billable) plus concurrent load.
- **Preemption is advisory.** `Preempt` is pure and tested, and the governor
  surfaces its verdicts, but the product defers/skips rather than cancelling an
  ambient pass mid-flight: these passes are short and write durable notes, so
  mid-write cancellation would trade a budget saving for corrupt state.
- **Not optimal scheduling.** This is admission control over a ranked set, not
  an infinite-horizon scheduler. It never claims to maximise anything.
- **Live concurrency smoke** (background cycles running while an owner turn is
  in flight, on the daily daemon) is a manual/journey check, not a hermetic
  one.
