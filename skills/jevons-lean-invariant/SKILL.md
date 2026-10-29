# Skill: close a Jevons invariant in Lean (🎯T863 probe)

Use this when a bullseye reliability invariant should be *named* in Lean 4
without building a Jevons↔Lean platform bridge.

## Workflow

1. Read the Go oracle: find the hermetic test (e.g. `internal/writconf/t862_17_test.go`).
2. Copy `lean/stubs/T862_17_FailClosed_stub.lean` → `lean/Invariants/T862_17_FailClosed.lean`.
3. Replace each `sorry` with a proof (start with `rcases`/`simp` on finite enumerations).
4. Run `./scripts/lean-oracle.sh sorries` until `SORRY_COUNT=0`.
5. Run `./scripts/lean-oracle.sh check` (lake build + zero sorry).
6. Keep the model *thin*: abstract tags for JSON fixtures, not a full parser.

## Oracle commands

| Command | Meaning |
|---------|---------|
| `./scripts/lean-oracle.sh build` | `lake build` only |
| `./scripts/lean-oracle.sh sorries` | list sorry sites + count |
| `./scripts/lean-oracle.sh check` | build must pass, sorry count 0 |

## Invariant map (T862.17 example)

| Go surface | Lean name |
|------------|-----------|
| `writconf.Parse` on malformed JSON | `parseSample` → `.err` |
| `CLIExecutor.Exec` preflight | `cliWouldLaunch` |
| `ClassifyAccess` with nil manifest | `classifyFsAccess none` → deny |
| `NetAllowed(nil)` | `netAllowed none` = false |

Do **not** attempt to formalize all of `bullseye.yaml` or the daemon API in Lean.
