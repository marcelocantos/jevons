# Convergence impatience: observation, step, satisfaction

🎯T316. Status: pure model landed (`internal/converge`); daemon wiring is
🎯T315 (actuator), 🎯T317 (noise ladder), 🎯T318 (attenuation), 🎯T319
(auto-clear + postmortem).

## The failure this exists to prevent

On 2026-08-08 three open-mission workers sat `phase=idle` from 11:14 to
14:03. The daemon was not blind — it fired `worker-idle` events to
`jevons-po` the whole time. Every one of those events was treated as the
end of the matter: the observation was consumed, an action was taken, and
the loop moved on. The PO was itself idle or queued, so nothing happened,
and nothing in the system was still *waiting* for something to happen.

The bug is not "we failed to notify". The bug is that notifying counted.

## The distinction

Three words, deliberately not interchangeable:

| word | meaning | example |
|---|---|---|
| **observation** | what the world looks like right now | agent is `phase=idle` with target 🎯T316 open |
| **step** | an action taken toward closing the gap | `worker-idle` to the parent PO; a re-pressure deliver; rehydrate; overseer noise; owner-visible sticky |
| **satisfaction** | the desired state actually holding | the agent is `phase=working` on that open mission, or the mission is closed/reaped with evidence |

Only an observation can satisfy a gap. **No step ever can.** Having told
the PO is progress toward satisfaction; it is not satisfaction. This holds
for every rung, including the loudest: an owner-visible alert is still
noise, and noise is not achievement.

## What counts as satisfaction

Exactly two things:

1. **Working on the open mission.** The agent's phase is `working` and the
   mission is still open. Working on *something else* does not count — a
   re-bound agent starts a new gap episode rather than inheriting the old
   one.
2. **The mission closed or reaped, with evidence.** The ledger says
   achieved / retired / set aside, or the agent was stopped and removed
   from the live fleet (🎯T165 / 🎯T195).

What explicitly does **not** count:

- **A terminal report claiming done.** Attestation is not execution
  (🎯T31.1). A claims-done idle agent is classified `unverified_done` — a
  gap with a different route: the parent verifies and reaps, rather than
  the worker being re-pressured. The gap closes when the ledger or the
  reap says so.
- **A delivered event, a nudge, a restart, an ack.** All steps.
- **An empty ack** ("ok, looking at it") with no phase change.

### Withdrawal is not achievement

Deliberate stop, design-gated, not a work agent, or no open mission all
remove the gap from the standing set as `withdrawn`. The reconcile set
distinguishes this from `satisfied` on purpose: scoping something out is
not the same as getting it done, and a withdrawal owes no
mini-postmortem (🎯T319).

### Residual: reap off an open mission

Reaping an agent satisfies *that agent's* gap while the mission stays
wanted. The set reports `ResidualMissionOpen` rather than pretending the
desired state holds. Whoever owns respawn (the PO) owns the successor;
the set does not invent an owner for it.

## The standing reconcile set

`converge.Set` holds every open-mission gap the engine is still impatient
about. The invariant that makes this a convergence engine rather than
another nudge loop is an asymmetry in the API:

- `Reconcile(observation, now)` is the **only** method that can remove a
  gap, and it removes one only on an observation classifying as satisfied
  or out-of-scope.
- `RecordStep` and `Drive` can append history and move timers. They can
  never empty the set.

So the engine's impatience survives its own actions. A gap that has had
every rung of the ladder fired at it is exactly as open as a gap that has
had nothing done about it — louder, older, better documented, still open.

Per-gap state the consumers read: `OpenedAt` (dwell), `LastStepAt` and
`StepsByKind` (anti-thrash and rung history), `Kind` (idle / dead handle /
unverified done — different routes), and `Episode`, which survives
satisfaction so a flapping worker is visible as a pattern rather than as a
series of unrelated fresh gaps.

## Integration surface

Deliberately small, so the actuator and the ladder plug in without
inventing their own semantics:

```go
type Actuator interface {
    Step(g MissionGap, now time.Time) (StepKind, error)
}
type DuePolicy func(g MissionGap, now time.Time) bool

func (s *Set) Drive(now time.Time, due DuePolicy, act Actuator) []StepRecord
```

- **🎯T315** implements `Actuator` for re-pressure and rehydrate.
- **🎯T317** supplies the `DuePolicy` (which rung is due, and its
  anti-thrash spacing) and consumes the set through `Set.LadderGaps()`.
  Its minimal `Gap{Agent, Mission, Since, Satisfied}` view is a projection
  of `MissionGap`: `Satisfied` is false by construction for anything still
  in the set, because that is what being in the set means.
- **🎯T318** attenuates the due policy on visible progress. Attenuation
  delays the next rung; it never clears the gap.
- **🎯T319** reads `Outcome.LadderView()` — reported once, on the tick
  satisfaction happens — to clear human noise and owe a postmortem. The
  postmortem is a report step, not satisfaction.

The package is pure: no daemon, no registry, no clock. `now` is a
parameter throughout, so the whole lifecycle is hermetic.

## Oracles

`go test ./internal/converge/ -count=1`:

| test | pins |
|---|---|
| `TestClassifyObservationSatisfactionSemantics` | 13 rows: every gap / satisfied / withdrawn verdict above |
| `TestGapLifecycleIdleStepsThenWorking` | enter idle → steps fire → still open → working clears it |
| `TestStepsNeverSatisfyTheGap` | 20 steps across all five kinds, including the human alert: set size stays 1 |
| `TestClaimsDoneWithoutEvidenceStaysGap` | claims-done stays open until ledger closure |
| `TestWithdrawnAndResidualAreNotAchievement` | withdrawal owes no postmortem; reap reports the residual |
| `TestReopenAfterFlapCountsEpisodes` | flap and re-bind open new episodes with clean step history |
| `TestDriveRecordsStepsAndKeepsGapsStanding` | actuator failures recorded; membership untouched |
| `TestLadderViewBridge` | standing gaps never present as satisfied, even after the loudest rung |

## Residual

- The model is pure. Nothing in this commit makes the daily daemon
  impatient — the reconcile set has to be driven from the converge loop
  before any of this is owner-visible, and that wiring rides 🎯T315 /
  🎯T317 with 🎯T194 daily-path evidence.
- `MissionOpen` is supplied by the caller. The heuristic for an unbound
  implementer (open until accounted for) currently lives in
  `internal/mcpserver` (`HasOpenMissionForIdle`, 🎯T244); this package
  takes the verdict rather than re-deriving it.
- Gap identity is the agent name. A mission worked by several agents is
  several gaps, and a mission with *no* agent is not a gap here at all —
  that is frontier work, not impatience.
