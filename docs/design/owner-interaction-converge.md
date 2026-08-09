# Owner interaction converges (🎯T355)

Status: implemented (hermetics green); live owner smoke is the standing
residual.

## Why this exists

The daemon can be perfectly healthy while talking to Jevons is broken. On
2026-08-09 the owner sat in front of a live `jevonsd` with a spinner that
would not stop, a turn that never came back, and no way to tell whether the
words had even landed. Every existing reconciler said the plant was fine:

- 🎯T204 cockpit converge keeps the **overseer process** alive, attached and
  not stuck-busy. A live process is not a usable chat.
- 🎯T316/🎯T317 keep **fleet missions** converging. The fleet was fine.
- 🎯T171/🎯T328 recover **after a daemon restart** — the right protocol
  shape (trigger → open intent → dual path → named residual, and event
  delivery is never satisfaction) applied to exactly one outage class.

What was missing is the owner-facing plane: a level-triggered observation of
whether the owner's interaction is healthy, and a recovery loop that treats
restart as *one* outage class among several.

## The contract

Three owner-visible dimensions, observed as levels rather than remembered as
edges, plus one for the client itself:

| Dimension | Desired state |
|---|---|
| `send_landed` | the newest owner turn is durable in the chatlog **and** acked out of the notify queue into the overseer's session |
| `chrome_truthful` | the working indicator the clients were told to paint matches server level truth |
| `reply_or_residual` | that turn ended in a sealed assistant reply, or in a **named** residual, within a documented bound |
| `interaction_usable` | the client can still be driven (composer usable, main thread ticking) |

Silence is never an outcome. `cancelled_by_owner`, `delivery_failed`,
`acp_unstuck` are outcomes; "nothing happened" is a gap.

## Outage classes, one loop

Restart is not the model — it is one way to arrive at a gap kind:

| Kind | What broke | Actuator |
|---|---|---|
| `send_not_landed` | owner text never reached the durable journal (🎯T305 class) | re-inject the owner text |
| `send_not_delivered` | journaled but never acked to the overseer | re-inject (skipped while it is still queued) |
| `chrome_false_working` | spinner over an idle server | publish level truth |
| `chrome_false_idle` | silent work behind idle chrome | publish level truth |
| `acp_stall` | prompt genuinely in flight, no provider progress | interrupt — **only** when a prompt really is in flight |
| `owner_turn_stall` | no reply, no residual, nothing in flight (bounce, dropped queue, lost session) | re-inject the owner text |
| `ux_degraded` | composer blocked or client heartbeat stale | publish level truth + `ux: yield_hydrate` hint |

Re-injection is 🎯T328 owner-intent-resume generalized past restart: the same
actuator serves a bounce, a dropped queue and a lost session.

## Doctrine reused, not re-invented

`internal/converge/owner.go` is domain-parallel to the fleet half in the same
package. It shares the vocabulary (`Condition`, `Resolution`, `StepKind`,
`StepRecord`) and projects into the 🎯T317 ladder's `Gap` view, but it does
**not** overload `MissionGap`: an owner gap is a dimension and an outage
class, not an agent and a mission.

The load-bearing invariant is inherited verbatim from 🎯T316:

> Only an observation satisfies. `OwnerSet.Reconcile` is the only method that
> can remove a gap, and it removes one only on a verdict of satisfied or
> out-of-scope. `RecordStep` and `Drive` can add history and move timers;
> they can never empty the set.

Escalation is the same ladder shape: the kind's own actuator up to
`OwnerMaxPrimarySteps`, then one report to the overseer, then an
owner-visible alert. A rung is noise, not satisfaction.

Scoping out is not achievement: with no client connected, chrome and
interaction are `out_of_scope` (nobody is being lied to), while durability
still matters.

## Bounds

`DefaultOwnerBounds` — send-landed 5s, chrome-truth 15s (covers the ordinary
send→drain race), reply 90s, ACP stall 2m (deliberately longer than the
cockpit's own 90s stuck-busy timeout, so the generic recovery acts first and
this is the backstop), UI heartbeat 90s.

The reply bound governs **silence, not duration**: a turn that keeps
producing provider events is satisfied however long it runs.

## Wiring

`internal/server/owner_health.go` is observation and actuation only:

- `NoteOwnerSend` on an accepted composer turn (also lights the client's own
  chrome level), `noteChatJournaled` from the durable append, and
  `NoteOwnerDelivered` from the owner batch leaving the notify queue.
- `NoteOwnerReplySealed` on a terminal stop that was an owner turn *or*
  produced visible assistant text — a fleet note drained mid-turn clears
  `overseerOwnerTurn` (🎯T291), and reading that as silence would re-ask a
  question the owner already had answered.
- `NoteOwnerResidual` from cancel, surfaced delivery failure, and ACP unstick.
- `NoteOwnerUIHeartbeat` from the chat transport ping — a frozen main thread
  stops pinging, which is how UX degrade is observed server-side.
- `ReconcileOwnerHealth` runs on the 🎯T204 cockpit tick: observe → classify
  → reconcile → drive.

The idle level frame carries settle wording on purpose: the client only lets
an idle level clear an open owner turn on recovery/cancel phrasing (🎯T260).
Level frames are broadcast live, not journaled — ephemeral chrome truth is
not a turn.

One owner turn can open two dimensions at once (nothing acked it, nothing
answered it). Re-injection is deduped per send id per round so the owner is
never asked their own question twice.

## Oracles

- `internal/converge/owner_test.go` — classifier table over every outage
  class, bounds (nothing fires inside the grace), named residual satisfies
  while silence does not, scoping-out ≠ satisfaction, steps never satisfy,
  actuator routing and escalation, ladder projection.
- `internal/server/owner_health_test.go` — hermetic recovery paths on a real
  `Server` with a durable chatlog and a stubbed delivery seam: healthy turn
  leaves no gaps; false working chrome is cleared from level truth and closed
  only by the next observation; a queued turn is not asked twice; a lost turn
  is re-injected; a stalled turn is satisfied only by reply or named
  residual; an unrecovered gap escalates to the owner; ACP unstick refuses to
  fire without a prompt in flight; no connected client scopes chrome out.

## Client half (🎯T361)

The three client-side residuals of T355 are closed; the loop now has both
ends. `web/scripts/owner_ux.js` is the pure layer, `web/index.html` the
wiring.

- **Yield/hydrate.** A status frame carrying `ux: "yield_hydrate"` makes the
  page cancel every queued paint callback and forget the work — no layout is
  read, so the yield itself cannot reflow — then re-hydrate the visible band
  on a *later* macrotask, through the same phased, budgeted T349 pass. The
  later task is the point: it hands the browser a full turn for input and
  paint before the next pass is asked for. A plain level frame never yields.
- **Sticky class banner.** `owner interaction degraded: …` raises the
  degraded banner for its own class and stands until the server publishes
  recovery, because the condition it reports leaves no other visible trace
  once the error bubble scrolls away. Overseer-down (🎯T138) outranks it in
  the one banner — a dead overseer explains the degrade, and two lines read
  as two faults. The alert is journaled, so it is honoured only on a live
  frame: replaying it on reconnect would re-raise a banner for a gap that may
  be long closed, and a gap that still stands re-alerts on the next tick.
- **Recovery frame.** An escalation the owner can see needs a recovery the
  owner can see, so satisfying a dimension that reached the alert rung
  broadcasts `{"type":"status","ux":"recovered"}` — live, not journaled. It
  carries no `state`, so it cannot settle working chrome mid-turn (🎯T260) on
  its way to clearing the banner, and it survives the soft-reconnect filter
  (🎯T143) because it is a status.
- **Composer reporting.** The client sends `{"type":"ux_state",
  "composer_blocked":…}` so `OwnerObservation.ComposerBlocked` is observed
  rather than assumed false. Blocked is the owner's question — can I submit a
  turn right now? — so a disabled send button, a hard-disabled input and the
  T138 degraded hold all count. The level is *sampled* on a slow tick rather
  than hooked at every mutation site (a missed hook reports a stale level,
  which is worse than a late one) and reported on the edge, so a healthy
  composer costs one frame per connection. A closed socket never reports: it
  cannot, and the server already sees the connection go.

Oracles: `web/scripts/owner_ux_test.js` (pure policy + wiring gates) and
`scripts/chat-ui-test/t361-owner-ux-test.js` — Playwright over a mocked
socket, driving the exact frames `owner_health.go` emits and asserting the
page yields then hydrates, the banner shows / stays / clears / ignores
replay, and a blocked composer reaches the wire. Server side:
`TestOwnerHealthObservesClientReportedBlockedComposer`,
`TestOwnerHealthUXStepHintsYieldHydrate`,
`TestOwnerHealthPublishesRecoveryOnlyAfterAnAlert`.

## Residuals

- **Live owner smoke.** Hermetics cover the loop; the daily seat is the class-3
  gate on wording and feel.
- **Frozen-tab reporting.** A page whose main thread is genuinely wedged
  cannot report its own composer either; that class is still observed from
  heartbeat staleness, which is what the yield/hydrate step exists to break.
- **Multi-client chrome.** Chrome truth is modelled per server, not per
  connection; a second client with divergent local chrome is not tracked
  separately, and composer reports from two clients are one level.
