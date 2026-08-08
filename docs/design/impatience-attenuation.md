# Impatience attenuation (🎯T318)

> **Scope.** This note covers the attenuation half of the convergence
> impatience construct. The satisfaction semantics it depends on belong to
> 🎯T316; when that target lands its doctrine note, fold this file into it and
> leave a pointer here. Cross-references are marked **[T316]** below.

Impatience without attenuation is a smoke alarm that gets louder while you
are already carrying the fire extinguisher. 🎯T315 re-pressures a stuck
agent, 🎯T317 escalates noise when that fails, 🎯T319 reports the incident
afterwards. 🎯T318 is the part that notices somebody is *already dealing with
it* and turns the volume down — without ever concluding the fire is out.

## The line that everything else defends

**Progress is not satisfaction.**

- **Satisfaction** ends the gap. It is exactly two things, and 🎯T316 owns
  the verdict: the open-mission agent is *working*, or the mission is
  *closed/reaped* with evidence. **[T316]**
- **Attenuation** lowers the noise and stretches the timing because progress
  is visible. It is temporary, bounded, and it leaves the gap standing in
  the reconcile set.

Nothing in `internal/converge/attenuate` can close a gap.
`Adjustment.GapOpen` is always true, and `Attenuator.Forget` is documented
and tested as satisfaction-only — it is called from the ladder's close path
and from nowhere else. The failure this guards against is the obvious one:
an engine that treats "the overseer restarted the PO" as "the problem is
handled" stops watching at the exact moment it should be watching hardest.

## What counts as progress

| Signal | Progress? | Lowers the ceiling? |
|---|---|---|
| Parent (overseer/PO) enters working **and** delivers re-pressure | yes | yes |
| Restart / rehydrate of parent or implementer | yes | yes |
| Spawn of a research or fix worker bound to the gap | yes | yes |
| Stuck agent transitions idle→working | yes | yes |
| 🎯T315 actuator's own deliver lands | yes | **no** |
| Notify-only ("we told someone") | **no** | no |
| Empty ack | **no** | no |

Two distinctions carry weight here.

**Notify-only and empty ack buy nothing.** Telling somebody is a step, not
progress. This is the same rule 🎯T317 applies to its own rungs, one level
down: an event that informs does not thereby fix.

**The engine's own re-pressure is weak progress.** A landed deliver did
reach the agent, so it buys delay — but it must not lower the noise ceiling,
or the engine would be scoring its own noise as its own cure and could talk
itself quiet without anything happening. Strong progress means somebody
*other than the impatience engine* moved.

The stuck agent going idle→working is listed as progress *and* is what
🎯T316 reads as satisfaction. Attenuation still only treats it as progress:
it observes, it does not adjudicate. The verdict arrives separately through
`Gap.Satisfied`. **[T316]**

## Two consumer seams

Attenuation never reimplements the ladder or the actuator. It modulates them
through two values from `Attenuator.Adjust`.

**🎯T317's ladder** — `internal/converge/attenuation.go`:

```go
effective, ceiling := l.attenuated(g.Agent, dwell, now)
rung := dueRung(effective, now, st, ceiling)
```

`EffectiveDwell` is the dwell less accumulated credit, so every threshold
shifts out. `Ceiling` is the loudest rung permitted, and it is an **input**
to `dueRung`, not a clamp on its result: clamping a due human-rung decision
down to re-pressure would fire the actuator out of turn and defeat T317's
per-rung anti-thrash intervals. Excluded rungs are skipped, and the quieter
rung fires only if it is due on its own terms
(`TestCeilingDoesNotThrashTheQuieterRung`).

**🎯T315's actuator** — `Adjustment.Backoff`:

```go
need := NextNudgeBackoff(o.NudgeCount, o.Backoffs)
need = adj.Backoff(need)
```

Extra delay is capped at the base wait, so attenuation at most doubles the
interval between re-pressures. *(Seam ready and tested; adoption in
`internal/mcpserver/idle_nudge.go` is one line, pending that file's
in-flight edits.)*

## The bounds

Every one of these is a documented constant in `attenuate.DefaultPolicy`,
and each has an oracle.

| Bound | Default | What it prevents |
|---|---|---|
| Credit per progress signal | 8m | — |
| Floor per signal (`MinCredit`) | 1m | credit decaying to literally nothing |
| `MaxCredit` (outstanding, per gap) | 20m | one burst of progress freezing the ladder |
| `MaxLifetimeCredit` (per gap, ever) | 90m | a permanent drip holding impatience off forever |
| `StallBound` | 12m | attenuation outliving the progress that earned it |
| Ceiling floor | `RungRepressure` | progress ever silencing the actuator |
| Actuator backoff stretch | ≤ 2× base | attenuation stopping re-pressure |

### The stall re-climb bound

**If no further progress signal arrives within `StallBound` (12m), the
outstanding credit is void, the ceiling is restored to the human rung, and
impatience climbs again** — through overseer noise to human escalation on
🎯T317's normal timeline.

Each stall episode also takes a **strike**, and each strike halves what the
*next* progress signal is worth (floored at `MinCredit`). This is the
anti-Goodhart tooth. Without it, anything that emits a progress signal every
eleven minutes — a restart loop, a spawn that immediately dies, an agent
flapping idle→working — buys unlimited quiet while converging on nothing.
With it, a cycle of "twitch, go quiet, twitch" pays less every round and the
ladder reaches the owner regardless. One quiet stretch counts as one strike
however many ticks observe it.

Oracles: `TestStallVoidsCreditAndReClimbs`, `TestStallStrikesDiminishCredit`,
`TestStallReClimbsToTheHumanRung`, and
`TestAttenuationIsBoundedSoHumanRungStillArrives` — six hours of unbroken
two-minute progress signals still passes the human threshold.

## Anti-thrash, across all four targets

Four bounded mechanisms compose rather than compete:

1. **🎯T317 per-rung intervals** (`RepressureEvery` 5m, `OverseerNoiseEvery`
   20m, `HumanAlertEvery` 30m) and at most one action per agent per tick.
2. **🎯T318 credit** stretches the thresholds; the ceiling caps loudness.
3. **🎯T318 stall strikes** stop credit from being farmed.
4. **🎯T319 postmortem coalescing** — one report per closed incident.

Because the ceiling is an input to rung selection rather than a clamp, (1)
and (2) cannot cancel each other: a capped ladder still honours the quieter
rung's own interval.

## Where it lives

| File | Contents |
|---|---|
| `internal/converge/attenuate/attenuate.go` | pure policy — signals, credit, stall, ceiling, `Backoff` |
| `internal/converge/attenuate/attenuate_test.go` | policy oracles (11) |
| `internal/converge/attenuation.go` | ladder wiring — `SetAttenuator`, `ObserveProgress`, `Attenuation`, `forgetAttenuation` |
| `internal/converge/attenuation_test.go` | integration oracles (7) |

A `Ladder` with no attenuator installed is the pre-🎯T318 ladder exactly, so
T317's own oracles pin the identity path
(`TestNilAttenuatorIsUnattenuatedTiming`).

## Residual

- **Signal production is not wired.** `Ladder.ObserveProgress` is the
  entry point; the daemon paths that witness restarts, spawns, parent-working
  deliveries, and idle→working transitions do not call it yet. Until they do,
  a live ladder runs unattenuated — which is safe (louder, never quieter) but
  is not the target's product behaviour.
- **🎯T315 actuator adoption** is one line in `idle_nudge.go`, deferred while
  that file has in-flight edits.
- **Constants are untuned.** The defaults are reasoned against T317's
  ladder (4m/15m/45m), not measured against real incidents. Expect the first
  live episodes to move them.
- **No live evidence.** Everything above is hermetic. Daemon-path activation
  needs 🎯T194 evidence (detached restart + live probe).
