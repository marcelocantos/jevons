# Fleet control census (🎯T766.1)

Taken 2026-09-21 against `internal/` and `cmd/` at the time of writing.

This document exists for one reason. 🎯T766 is achieved when the number of
mechanisms goes **down**, because the failure being fixed is the habit of
adding one. You cannot delete what nobody has enumerated, and you cannot
judge a deletion criterion that is argued rather than counted. So this is a
list with a disposition against every entry, and a script that counts.

It is a survey, not a design. Where it quotes a conclusion from the
2026-09-21 studies rather than a line I read myself, it says so.

## Baseline

`scripts/fleetcensus/count.sh`, run at filing time:

| Measure | 2026-09-21 |
|---|---|
| Production files owning a standing loop | 24 |
| Independent seat-state derivations | 11 |
| Production files named after a single target | 22 |
| Substring-classification call sites | 665 |
| Distinct target ids referenced in production code | 560 |

The counts are deliberately crude and stable rather than clever: a grep whose
meaning drifts is worse than no ratchet. Each is an upper bound on a real
thing, and the trend is what matters. Two independent facts frame them, both
established by `git log --diff-filter=D` and by comparing target references
per file over time:

- **No control loop has ever been deleted** in this repository's history.
- **No file has ever shed a target reference.**

If 🎯T766 closes and those are still true, it became the twenty-fifth
mechanism rather than the thing that replaced twenty-four.

## Seat-state derivations

Eleven ways to answer "what is this seat doing", with no aggregator. They
disagree by construction, not by race, because each reads a different signal
with a different failure mode for "I could not observe".

| # | Derivation | Signal | Disposition |
|---|---|---|---|
| 1 | `turnev.ClassifyPhaseFile` via `ClassifyAgentSessionPhase` | incremental fold over the transcript | fold into the authority |
| 2 | `IdleActivity` ACP tracker | in-process ACP event stream; empty when the sink is dark | fold into the authority — this is the closest thing to a real feed |
| 3 | `ClassifyAgentPhase` | alive + turnBegan + materialised + session exists | delete once 1 and 2 are unified |
| 4 | `proc.Alive()` (claudia) | `tmux list-windows`, 2s TTL | keep — provider truth, but read it through the authority |
| 5 | `proc.PromptInFlight()` → `SeatIdle` (`seat_load.go`) | ACP prompt flag | delete; this is the signal that authorises a SIGKILL and it disagrees with 1 |
| 6 | butler `statusFromEntries` → `thread.StateIdle` | last-N transcript tail | delete; second parser, opposite action to 2 |
| 7 | born-stuck (`born_stuck.go`) | birth record + transcript absent past a grace | fold into the authority as a derived condition |
| 8 | `SweepDeadAgents` dead-handle | registry handle vs process | fold into the authority |
| 9 | `fleetintent.Snapshot` | **intent, not observation** | keep, and make it the desired-state input to the reconciler |
| 10 | `panecensus.Plan` | tmux panes vs registry names | fold into the authority |
| 11 | `seatload.Tracker.Sources` | `ps` process-group anchors | keep as a host signal, not a seat-state signal |

Plus two further notions of "is this seat's work still live":
`poproactive.AlreadyEngaged` (a registry walk per leaf) and
`MissionOpenFromCwd` (a 2.9 MB ledger parse per target, uncached).

Only 9 is a shared source today, and it is deliberately the desired state.
Six of the eleven observers do not consult it at all.

## Standing control loops

Twenty-four production files own a ticker. The ones that act on seats:

| Loop | Interval | Can do | Disposition |
|---|---|---|---|
| `overseer_converge` (cockpit) | 3s | attach, interrupt overseer, launch | keep the attach duty; move the acting to the reconciler |
| cockpit fleet hooks | 30s | fires the seven-sweep bundle | delete — it exists to avoid a separate 1m loop that still runs |
| `runIdlePressureLoop` | 60s | nudge, brief, born-stuck notice | delete into the reconciler |
| `StartAgentWireSweep` | 30s | re-attach the ACP sink | keep; this is a repair of the feed the authority depends on |
| `StartSentinelLoop` | 2m | fires both bundles, files targets | keep the diagnosis; remove its actuation |
| `startContextCeiling` | 2m | report unworkable to parent | keep; fold its transcript read into the authority |
| `reapIdleThreads` (butler) | 2m | **stops processes** | delete; duplicate of the idle path with the opposite action |
| `StartFrontierConsumeLoop` | 10m | spawn, park | keep as the only spawner; read desired state from the ledger |
| `startWorktreeReap` | 10m | delete worktrees | keep — disjoint concern |
| `startPlanUsage` → `SweepPlanPolicy` | config | migrate, park | fold the migrate/park decision into the reconciler |
| `cost` collector and enforcer | 3s/15s | kill worker, global stop | keep; it is a budget authority, not a seat observer |
| `provider.LivenessMonitor` | config | mark provider down | keep — provider scope |
| `supervise` agent and report | 60s/15s | reinstate launchd, notify | keep — different process tree on purpose |
| `portown.WatchLoopInspect` | 30s | notify only | keep |
| `config.watch` | config | reload | keep |
| `rsi` coach, retro, mint | 15m/6h/30m | post judgments | keep; ambient, no actuation on seats |
| `research`, `audit` | 90m/24h | write notes, brief | keep; ambient |

Plus two event-driven actuators that belong in the reconciler:
`ReapWorkAgentsOnTargetAchieve` (fires on a ledger write, 80ms debounce) and
`maybeReapDoneWorkAgent` (fires on a terminal report).

## Contradictions, and the single decision that replaces each

These are reachable from the code, established by the 2026-09-21 loop study.
Whether each has fired in production needs the eventlog; the point is that
none of them requires a race.

| Pair | Divergent read | One decision that replaces it |
|---|---|---|
| Idle-nudge skips `achieved_should_reap` while fleet-recover re-briefs | one consults the ledger hook, the other passes no `MissionOpen` at all, so every work seat looks open | the reconciler reads desired state once per pass and both actions consume it |
| Idle-nudge skips `in progress` while fleet-recover interrupts as stuck | identical process condition, opposite verdicts, one sweep apart | one precedence rule in one place |
| Transcript fold says working, load reaper says idle and SIGKILLs the process group | fold over the tape vs `!PromptInFlight()` | the authority owns "in flight"; the load reaper reads host pressure only |
| Butler stops a thread the idle loop is nudging | tail parser vs fold parser, opposite actions | derivation 6 is deleted |
| Reap-on-achieve races frontier-consume onto the same target | ledger write vs ledger poll, 80ms against 10m | both become inputs to one reconciler pass; the reap-verify ledger added to mediate them (🎯T753) is deleted with them |

## Guard coverage

Retrofitted guards cover disjoint subsets. The loop with the most destructive
levers has none.

| File | host-load | fleet intent | capacity |
|---|---|---|---|
| `fleet_recover.go` (interrupt, model swap) | 0 | 0 | 0 |
| `idle_nudge.go` | 8 | 6 | 0 |
| `sentinel.go` | 0 | 2 | 1 |
| `frontier_consume.go` | 0 | 4 | 0 |
| `born_stuck.go` | 0 | 0 | 0 |

Five of roughly twenty loops ask the capacity governor anything.

## Progress against the baseline

Recorded here rather than in a commit message, because the baseline table
above is what the parent is judged against and a reader needs both in one
place.

| Date | Change | Effect on the counts |
|---|---|---|
| 2026-09-21 | `internal/seatstate` lands: the authority, `Tri`, and three feeds | none — this is the replacement, and it counts against 🎯T766 until the things it replaces are gone |
| 2026-09-21 | Derivation 5 loses two of its eight sites: `seat_load.go` and `t254_5_1_tool_bound.go` now ask the authority | `substring_classifiers` 665 → 664; derivation count unchanged, because the symbol still has six callers |
| 2026-09-21 | **The butler idle reaper is deleted**: `reapIdleThreads`, `Butler.ReapIdle`, `SeatGate`, the send/reap admission on both sides, `fleet.Claudia.Busy`/`Pending`/`IdleTranscript`, the write-only `idle_activity.go` event subscription, and the `rsi` `NoteReaped` hooks. Across four log generations it ran 8,476 sweeps and stopped one thread (a boundary probe, 2026-09-06); its only remaining candidate was the overseer, kept alive by `Busy()` alone | `standing_loops` 24 → 23 — the first control loop ever deleted in this repository; in-flight ratchet 7 → 6. Derivation 6 (`statusFromEntries`) survives, because the butler's `List`/`Status` display path still reads it, so `seat_state_derivations` is unchanged |
| 2026-09-21 | **One authority for the whole daemon**: `cmd/jevonsd/main.go` builds a single `seatstate.Authority` before anything reads it and hands the same instance to `mcpserver`, `internal/server` and `internal/fleet`. `internal/server`'s four in-flight reads become one funnel that records what it saw; the fleet reports the seats it stops and forgets the ones it removes | in-flight ratchet 6 → 2 — the two left are the `observeSeat` feed and `fleet_recover.go`, whose file holds another worker's uncommitted edits |

Derivation 5 is the one being dismantled first, because it is the signal
that authorises `LoadTerminate` to kill a process group and it defaulted to
idle for a seat nothing was known about. Its remaining sites:

| Site | Kind | Note |
|---|---|---|
| `internal/mcpserver/mcpserver.go` (`observeSeat`) | feed | legitimate: this is how claudia's answer reaches the authority |
| `internal/mcpserver/fleet_recover.go:359` | control | left alone on 2026-09-21: another worker held uncommitted edits in that file (🎯T376) |

The four `internal/server` sites and the `internal/fleet` one cannot be
converted without a single authority instance built in `cmd/jevonsd/main.go`
and handed to all three packages — they are siblings with no import edge
between them. That injection is the next slice, and it is the point at which
the derivation genuinely disappears rather than shrinking.

`scripts/docratchet/t766_seat_derivation_ratchet_test.go` pins the count at
seven so it can only fall. Liveness (derivation 4) is not pinned: `.Alive()`
is spelled identically by unrelated types, and a ratchet that fires on
unrelated code gets switched off.

## How to read this later

Run `scripts/fleetcensus/count.sh`. If the numbers have not fallen, the work
has added rather than replaced, whatever the behaviour looks like. That is
the whole test.
