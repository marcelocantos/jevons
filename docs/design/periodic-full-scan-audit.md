# Periodic full-scan audit (🎯T357)

Status: built (staff-cycle shape). Live advanced-model residual noted at the end.

## Problem

Audits only happen when the owner asks for one. That makes them episodic and
shallow in exactly the places that rot quietly: skills that reference commands
that no longer exist, persona rules that contradict the shipped product, and
product code whose defects nobody is looking for because no one filed a bug.
Worse, an ad-hoc audit has no memory — the same finding is re-reported every
time, and a fixed finding is never recorded as fixed.

## Shape

A standing **staff cycle** (the 🎯T243 / 🎯T356 shape), not a permanent
monologue auditor. It lives in `internal/audit` and runs inside `jevonsd`:

```
schedule tick ─→ capacity ask ─→ scope manifest ─→ advanced-tier auditor
                  (🎯T359)         (3 surfaces)            │
                                                           ▼
   overseer notice ◀── residue merge ◀── parse + durable report artifact
   (new/reopened criticals)   (converging ledger)
```

Three surfaces in one pass, because a partial habit is how prompts and skills
end up never audited at all:

| Scope | Default roots |
|---|---|
| `code` | `cmd/`, `internal/`, `web/scripts/`, `scripts/` |
| `skills` | `<repo>/.claude/skills`, `~/.claude/skills` |
| `prompts` | `internal/config/persona.md`, `agents-guide.md`, `AGENTS.md`, `CLAUDE.md`, `~/.claude/AGENTS.md`, `~/.claude/CLAUDE.md` |

Missing roots are reported as missing, never fatal — a machine without a
skills tree still gets its code and prompts audited.

## Advanced tier, on purpose

A full-scan audit is a low-frequency, high-leverage read, so it is pinned to an
advanced model (`claude-fable-5` by default) rather than the cheap default
tier. The pin lives in **durable config** (`state_dir/audit/config.json`), not
at the call site, so it is inspectable and retunable with
`jevons_audit_configure`. Whether *root and POs* should also default to a
Fable-class model is a separate owner decision (🎯T358); this pin covers audit
passes only.

The auditor is invoked headless with the prompt on stdin and reads the
manifest files itself. The prompt carries the bounded **file list**, not file
contents — that is what keeps a full-scan manifest affordable.

## Residue: the memory that makes repetition converge

`state_dir/audit/residue.json` is one entry per finding **fingerprint**
(scope + normalized path + normalized title — deliberately *not* the line
number, so a finding that drifts as code moves updates its entry instead of
minting a duplicate).

| Observation | Outcome |
|---|---|
| unseen fingerprint | `open`, `new` — may notify |
| seen again | `seen_count++`, provenance refreshed, **no re-alert** |
| absent from a pass that **covered its scope** | `resolved` |
| resolved, then seen again | `reopened` — notifies again |

The load-bearing rail is that **only a covering pass may resolve**. A pass that
found no files for `skills` cannot conclude the skills findings are fixed, and
a failed or unparseable pass leaves residue untouched entirely — a broken
auditor must never read as "nothing to report".

Severity is remembered at its worst (`max_severity`), so a later pass that
downgrades a finding does not erase that it once read critical.

## Auditor suggests, overseer disposes

Nothing in this package files a bullseye target or rewrites a line of source.
Findings may carry a `suggested_target` (name + acceptance assertions); the
overseer records what it did with `jevons_audit_residue`:

`filed` (with `target_id`) · `ignore_with_reason` (reason **required**) ·
`accepted` · `pending`

That split is the same one the RSI coach uses (🎯T243), for the same reason:
an ambient process that can file its own work becomes an unread queue.

## Bounds

Every ambient loop needs a reason it cannot run away:

- `interval_sec` (24 h) and `max_cycles_per_day` (4) — cadence + standing cost guard.
- `min_cycle_gap_sec` (300) — anti-thrash floor on manual triggers.
- `timeout_sec` (1200) — wall-clock bound on one pass.
- `max_files_per_scope` (400), `max_bytes_per_scope` (4 MiB),
  `max_file_bytes` (256 KiB) — manifest bounds. Truncation is stated to the
  auditor **and** carried on the report, so a truncated surface is never
  reported clean.
- `max_findings` (40), `max_output_bytes` (1 MiB) — answer bounds.
- `max_notifies_per_cycle` (3), `notify_severity` (critical) — interrupt budget.
- `keep_reports` (30) — artifacts retained on disk.
- **Capacity (🎯T359):** every scheduled tick asks under `ClassAudit` before
  dispatching. `defer` skips the tick and records why in `state.json`;
  `degrade` runs a **reduced** pass (halved manifest bounds and finding cap)
  that still covers all three surfaces, because dropping a surface would let
  the residue merge resolve findings it never looked for.

## Surfaces

| Surface | Use |
|---|---|
| `jevons_audit_cycle` | run one bounded pass now (`force` bypasses the cost guards) |
| `jevons_audit_status` | config, run record, outstanding residue by severity |
| `jevons_audit_residue` | list findings, or record the overseer's disposition |
| `jevons_audit_report` | read one durable report artifact (latest by default) |
| `jevons_audit_configure` | cadence, model pin, scope roots, bounds, notify policy |
| `state_dir/audit/{config,state,residue}.json`, `reports/` | durable artifacts |

Notices reach the overseer through the same fire-and-forget path the research
cycle and RSI coach use (queue/interrupt, never `WaitForResponse`) — an audit
notice is news, not a request, and must not block the cycle on a busy seat.

Daemon env: `JEVONS_AUDIT=0` disables the schedule (tools stay registered);
`JEVONS_AUDIT_INTERVAL` overrides the cadence.

## Oracles

Hermetic, in `internal/audit` and `internal/mcpserver`:

- a pass covers all three surfaces, on the advanced pin, leaving a durable
  report (`TestAuditCycleCoversAllScopesAndLeavesArtifacts`)
- residue converges: a repeat pass mints nothing new and does not re-alert; a
  covering clean pass resolves (`TestAuditResidueConvergesAcrossPasses`)
- a partial scan never resolves an uncovered scope
  (`TestResiduePartialScanNeverResolvesUnseenScopes`)
- fingerprints are stable across line moves (`TestFingerprintStableAcrossLineMoves`)
- disposition is recorded, and `ignore_with_reason` refuses to be silent
  (`TestAuditResidueDisposition`)
- retune is durable and a bad severity is refused (`TestAuditConfigureRetunesDurably`)
- the cost guard skips an unforced repeat (`TestAuditCycleRespectsCostGuard`)
- capacity defers a scheduled tick durably, and degrades to a reduced pass
  without dropping a surface (`TestScheduledCycleDefersUnderCapacityPressure`,
  `TestScheduledCycleRunsReducedUnderElevatedPressure`)
- malformed residue is a hard error, never a silent reset
  (`TestResidueStoreMalformedIsHardError`)

## Residual

- **Live advanced-model pass:** the hermetics drive the cycle with a fixture
  runner. One successful pass against the real `claude-fable-5` CLI on the
  daily path is the remaining live evidence.
- **Class-3 (owner taste):** whether the findings a full-scan pass produces are
  the ones worth interrupting for, and where `notify_severity` should sit.
- **Out of scope unless the owner opens it:** infinite full-repo scanning and
  any automatic bulk rewrite. This package reads; it never edits.
