# T766.2: fourth independent verify cycle (from disk)

## Disk facts (re-read this cycle; not reused from prior prose)

| Item | Value |
|---|---|
| HEAD | `9af488cedb17cbb2e35f41fe87a00a961105dbaa` |
| Merge | `9af488ce` = merge of `cf350e3d` into `steer-modes-stop-guards-seat-stops` |
| Increment SHA | `cf350e3d94cca2ba9b421db2a273f0897c532fd9` |
| Ancestor check | `git merge-base --is-ancestor cf350e3d HEAD` → yes |

### Gate ea044387 (full mcpserver)
- verdict **GREEN**, exit 0, tree clean
- commit `cf350e3d94cca2ba9b421db2a273f0897c532fd9`
- log on disk: `ok  github.com/marcelocantos/jevons/internal/mcpserver  36.656s`

### Gate a49efa5f (docratchet in-flight ratchet)
- verdict **GREEN**, exit 0, tree clean
- commit `cf350e3d94cca2ba9b421db2a273f0897c532fd9`
- log on disk: `TestT766SeatStateDerivationsOnlyFall/in-flight` PASS

### Ratchet pin (independent recount)
- `seatInFlightPin = 2` in `scripts/docratchet/t766_seat_derivation_ratchet_test.go`
- Production `.PromptInFlight()` outside `seatInFlight` funnels, counted this cycle:
  1. `internal/mcpserver/fleet_recover.go:369`
  2. `internal/mcpserver/mcpserver.go:596`
- count == pin == 2 → **unaffected** (third increment did not touch in-flight sites)

### Census
- `scripts/fleetcensus/count.sh` → `seat_state_derivations 10` (unchanged; expected for fold-not-delete)

## Substantive review of third increment (`cf350e3d`)

**What landed:** `observeSessionPhase` + one call from `sampleSentinel` after the real `ClassifyAgentSessionPhase` decode.

**Why the design holds:**
1. **Type-safe funnel** — `ClassifyAgentSessionPhase` returns `turnev.Phase`; `seatstate.State.Phase` is the same type. No string mapping (correctly distinguished from the riskier `agentPhase` path the worker declined).
2. **Phase-only claim** — `Authority.Observe` only writes Alive/InFlight when `Tri.Known()`; zero-value Tri is not Known, so phase-only observations cannot fabricate process/event claims. Matches T766.2's "unknown is a value / absence is not a state" bar.
3. **Unknown not asserted** — helper returns early on `PhaseUnknown`; also matches Observe's own `Phase != PhaseUnknown` guard.
4. **Provenance** — `Source: "transcript.fold"` is non-empty (Observe refuses unnamed sources).
5. **Scope discipline** — deferred `sessionPhaseOf` / `IdleNudgeSweepArgs` reshape is real: `SweepIdleNudges` consumes phase locally and does not thread per-seat decode outward today. Forcing a fold there would be a different change class.

**What this is not:**
- Not a removal of any census derivation from production code.
- Not a tightening of the in-flight ratchet.
- Not acceptance clause 3 satisfaction.

## Verdict

- Third increment is **legitimate** and **still integrated** at HEAD.
- Gates and ratchet **reconfirmed GREEN / pin=2 from disk**.
- **Do not achieve T766.2.** Target status remains `identified`; removals and ratchet movement still outstanding.
- Deferred: `idle_nudge.go` / `sessionPhaseOf` wiring when/if `IdleNudgeSweepArgs` is safely reshaped; remaining direct `.PromptInFlight()` sites; panecensus/flightState; any path that still re-derives rather than reading the authority.

No action required from this verifier beyond the above. Continue further increments under the same disk-verify discipline.
