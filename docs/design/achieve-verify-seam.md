# Achieve-time gate verification: the seam (🎯T765.1)

Status: slice B (the verifier, this repo) is implemented. Slice A (the
bullseye call site) is specified here for the bullseye product owner and is
**not** built. Slice C (a Claude-only hook) is deliberately not built.

## Problem

`bin/gate check-ledger` (🎯T765) audits achieves after the fact. A fabricated
green (🎯T757: "a clean-gate green" that never ran) still lands in the ledger
first. The write itself must be refusable, on every provider, with the ledger
byte-identical when it is refused.

## Slice B: the verifier (built)

```
gate check-attestation -ledger <path> -id <Tn>   < attestation-text
make -s achieve-verify LEDGER=<path> ID=<Tn>     < attestation-text
```

It calls `gate.CheckAchieve` with the real gate store, and with `Contains` /
`IsCommit` bound to the **ledger's own git repo** (`git rev-parse
--show-toplevel` of the ledger path, never the process cwd). A yourworld2,
minicades or claudia achieve is therefore judged against that repo's history.

| Exit | Meaning | stdout |
|---|---|---|
| 0 | `verified`, `ungated` (no gate claimed) or `marked` | first line `<Tn>: verified`, `<Tn>: ungated`, or `<Tn>: UNVERIFIED — ACCEPTED RISK: <sentence>` |
| 4 | `refused` | `<Tn>: refused`, then one line per flag: `• <kind> [field=<record field>]: <detail>` |
| 70 | the check could not be made (ledger unreadable, target absent, ledger outside a git work tree, store unopenable) | reason on stderr |
| 2 | bad command line | usage on stderr |

`make achieve-verify` reports any failure as make's own exit 2; a caller that needs the 4/70 distinction runs `bin/gate check-attestation` directly.

Anything other than 0 must refuse the achieve; 70 is a refusal to judge, never
a pass.

Field mapping (`gate.AchieveField`): `gate_id` (uncited or unknown id),
`verdict` (misquoted, not GREEN), `tree` (no provenance), `tree.clean`
(dirty tree), `tree.commit` (gate ran on a commit that lacks the attested fix).

A `marked` result is the accepted-risk escape: the attestation must state an
accepted risk that names the gate, `-clean` or the dirt. It is printed with the
marker so it can never be read as verified. It never launders an unknown gate
id or a misquoted verdict. This is also the escape for repos where `-clean` is
structurally impossible (yourworld2 until its SDL3 libs ship): the achiever
writes the sentence, the ledger carries the marker.

Oracle: `internal/gate/t765_check_attestation_cli_test.go` (shipped binary,
throwaway git repo, cwd in the jevons tree): missing, unknown, misquoted,
non-GREEN, `tree.clean` false, commit lacking the fix, clean GREEN on a
descendant, ungated, marked-vs-verified, and cannot-judge cases. Run with
`make test-achieve-verify`.

## Slice A: the bullseye call site (for the bullseye PO)

**Where.** `src/apply.rs`, in `apply::apply`, at the transition to
`Status::Achieved` for an existing target (about line 948) and at
create-as-achieved (about line 801). Run the verifier **before** assigning
`target.attestation`, and return an `ApplyError` with `ErrorCode::Validation`
on refusal. `apply::apply` runs inside `store::with_locked_mutation`
(`handler.rs` about line 1848), so an `Err` discards the mutation and the
ledger is byte-identical. The MCP tool, `bullseye commit --op achieve` and
`bullseye apply` all funnel into this one function, so one call site covers
every provider.

**Configuration.** An optional verifier command, first found wins:

1. environment `BULLSEYE_ACHIEVE_VERIFY` (a command line, split without a shell);
2. a top-level ledger key `achieve_verify: <command>` in `bullseye.yaml`.

Neither set means today's behaviour (attestation must be present). For this
repo the value is `bin/gate check-attestation`; bullseye appends
`-ledger <absolute ledger path> -id <Tn>` to the configured command.
Bullseye execs the command; it does not import the check natively, so gate's
rules can change without a bullseye release.

**Contract.**
- cwd: the directory containing the ledger. Never bullseye's own cwd.
- stdin: the proposed attestation text, verbatim, UTF-8.
- exit 0: proceed (stdout is informational; store a `marked` first line in the
  transition's event/context so it stays visible).
- any other exit, or failure to spawn: refuse. A missing or unexecutable
  verifier is a refusal, not a pass, otherwise deleting the binary disables the
  gate. The escape is unsetting the configuration, which is a visible diff.
- refusal text returned to the caller: `achieve refused by <command>:` followed
  by the verifier's stdout, unchanged. It names the failing flag and record
  field (`gate_id`, `verdict`, `tree.clean`, `tree.commit`).
- timeout: 30 s, then refuse.

**Per-repo rule.** The verifier resolves the ledger's own repo. Repos whose
gate ids live in the same `~/.jevons/gates` store (yourworld2, minicades,
claudia) need no per-repo config beyond setting `achieve_verify`. A repo where
`-clean` cannot pass writes an accepted-risk sentence about the gate in the
attestation and is `marked`, not refused.

**Reopen and re-achieve** go through the same transition and are checked again.
`assign`, `unassign` and non-achieving edits are untouched.

## Slice C: a Claude-only hook (not built)

A `PreToolUse` matcher on Bash `bullseye commit --op achieve` / `bullseye apply`
and `mcp__bullseye__*` is insufficient:

- It only runs in the Claude harness. Grok is the default provider, and Cursor
  and Codex seats run no such hook, so their MCP achieves never see it
  (whether they run any PreToolUse equivalent is unknown).
- The current `.claude/settings.json` matcher is
  `Write|Edit|MultiEdit|NotebookEdit|StrReplace`. Bash is absent, so treeguard's
  Bash half is inert for a stock Claude seat, and no matcher covers
  `mcp__bullseye__*`.
- A hook sees the tool call, not the resulting ledger write, so it cannot
  guarantee a byte-identical ledger, and it can be bypassed by any path that is
  not the matched tool (`bullseye apply` from a script, a human at a terminal).
- The git `pre-commit` hook is provider-independent but runs after bullseye has
  written the ledger.

The write path inside bullseye is the only point that covers every caller.
