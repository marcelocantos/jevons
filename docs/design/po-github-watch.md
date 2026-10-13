# POs watch their GitHub repos (🎯T1022) — scout report and plan

Status: scout output, 2026-10-11 (jv-t1022-auto). Owner review residual.
Children filed under 🎯T1022; see "Subdivision" below.

Owner ask (2026-10-07): POs watch their repos, triage issues and review
PRs; straightforward low-risk items are addressed directly, the rest is
escalated to the owner over blurter. Bullseye's webhook path "has never
been fully tested" — study and decide.

## What exists today

### bullseye: two issue adapters, no PR adapter

| Path | Code | Trigger | Status | Fit for 🎯T1022 |
|---|---|---|---|---|
| 🎯T34 gh-sync | `src/github.rs` (1797 lines), MCP tool `bullseye_github_sync` | pull: caller invokes it; `gh issue list --json` since a watermark in `.bullseye/github-sync.json`; open issues become `GH<n>` targets; achieve/set-aside closes the issue | tested with a `GhClient` fake; live `gh` is declared residue in the 2026-08-22 audit | **Use.** Proven, reachable from any seat with the bullseye MCP, no infrastructure, uses the caller's `gh` login. |
| 🎯T33/🎯T35 issuepipe | `src/github_issues.rs` (845 lines) behind cargo feature `github-issues`; CLI-only `issues-poll`, deliberately absent from the MCP surface (`cli.rs` `CLI_ONLY`) | push: GitHub webhook → Fly-hosted issuepipe Master → sqlpipe replica on localhost → poller | achieved from one 2026-07-26 scratch-log run; audit: "Live issuepipe Master \| No fixture server \| Residue"; no CI job compiles the feature; issuepipe repo last commit 2026-08-22; needs a webhook registered per repo and a Fly deployment kept alive | **Do not rely on it.** Real-time latency buys nothing: a PO reads its brief on a staff-cycle cadence (minutes to hours), the same cadence a `gh` poll runs at. Hardening it means a recorded fixture server, a feature-compile CI job, per-repo webhook registration and ongoing Fly ops — all cost, no decision it changes. |

**Decision (acceptance #2):** the standing intake is polling `gh` from the
jevons daemon at a bounded cadence. The webhook path stays residue, recorded
here and in the bullseye-side decision record (🎯T1022.1). The one cheap
hardening the audit itself recommends — a CI job that at least compiles
`--features github-issues` so the dormant code cannot rot silently — is
bullseye's own hygiene item, for bullseye-po to file; it is not a 🎯T1022
dependency.

Neither adapter sees pull requests. PR awareness and review are net new.

### jevons: the pieces a watch cycle composes from

- **Ambient staff cycles** are the established shape: `internal/audit`
  (🎯T357, 24h, capacity-gated, durable `state_dir/audit/{config,state}.json`,
  findings to the overseer) and `internal/research` (🎯T356, feed polls with
  per-hour caps). A repo watch is the same shape with `gh` as the feed.
- **PO seats carry the repo.** Live registry today (`GET /api/agents`):
  `jevons-po`, `bullseye-po`, `claudia-po`, `spyder-po`, `arrai-po`, `ge-po`,
  `yourworld2-po`, `multimaze2-po`, each with a `workdir` whose `origin`
  remote names the GitHub repo. `internal/roles` already recognises the
  `-po` suffix. No new registry field is needed.
- **Owner escalation already has a daemon-side transport.** 🎯T1028.3
  `internal/ownerquestions` gives each question its own durable
  outbox/dedup and sends through the `blurter` CLI from the daemon process
  (`BlurterSend`), with spooled-vs-delivered kept honest. A seat calling
  `mcp__blurter__blurter_send` itself is refused by a permission prompt in
  non-interactive sessions (recorded under the 2026-09-20 supervision
  incident in the ledger), so **escalation must go through the daemon**,
  not from the PO's own tool list.
- **Direct action on GitHub** is `gh` from the PO's workdir: `gh issue
  comment|close|edit --add-label`, `gh pr diff|checks|review
  --approve|--request-changes|--comment`, `gh pr merge --squash`. The owner
  is logged in with `repo` + `workflow` scopes.
- **Fleet doctrine** lives in `internal/config/persona.md`,
  `agents-guide.md` and the standing brief `internal/mcpserver/fleet_brief.go`.

### The live landscape (2026-10-11)

Across jevons, bullseye, claudia, mnemo, blurter, sawmill, pigeon, vellum:
zero open issues; two open draft PRs — claudia #61 (owner's own) and
mnemo #197 (external contributor, 23 files, +5681/−11, two checks). The
worked example therefore has one real escalation specimen (mnemo #197:
large, external, not low-risk) and needs a synthetic low-risk item for the
direct path.
## Decision record (🎯T1022.1)

**Decision:** the standing intake for PO repo watch is polling `gh`
through bullseye's existing T34 mechanism — exposed to jevons as the
`bullseye_github_sync` MCP tool — not the T33/T35 issuepipe webhook path.

**Why:** real-time delivery buys nothing here. A PO consumes its repo-watch
brief on a staff-cycle cadence (minutes to hours), the same cadence a `gh`
poll runs at, so webhook latency is not a decision-relevant advantage.
The webhook path also carries real standing cost that gh-polling does not:
a per-repo webhook registration, a Fly-hosted issuepipe Master kept alive,
and a sqlpipe WSS replica — none of which is exercised by anything else
in the fleet today.

**Evidence the webhook path is not fit to rely on:** T33/T35 are marked
achieved from a single 2026-07-26 live scratch-log run. bullseye's own
2026-08-22 entropy audit (`docs/audits/entropy-audit-2026-08-22.md` in the
bullseye repo) flags it explicitly: "Live issuepipe Master | No fixture
server | Residue" and "Unverified residue: live gh/issuepipe HTTP ...".
There is no CI job that even compiles the `github-issues` cargo feature
this path lives behind, and the issuepipe repo's last commit predates this
decision by weeks. **Consciously not relied upon** — this status is
recorded here rather than hardened, because hardening it (a recorded
fixture server, a feature-compile CI job, per-repo webhook registration,
ongoing Fly ops) would cost real engineering for a latency advantage that
changes no PO decision.

**Fitness notes — `bullseye_github_sync` as the PO's mirror tool (T34):**

- *Fit:* proven two-way sync (`src/github.rs`, 1797 lines) already tested
  against a `GhClient` fake; reachable from any seat with the bullseye MCP
  loaded, with no additional infrastructure; uses the caller's own `gh`
  login, so no separate credential to provision or rotate; pull-based
  (caller invokes it against a watermark in `.bullseye/github-sync.json`),
  which composes cleanly with a bounded daemon poll cycle (🎯T1022.2).
- *Gaps for 🎯T1022's use case:* it mirrors *issues* into bullseye targets
  (open issue → `GH<n>` target; achieve/set-aside closes the issue) — it
  has no PR awareness at all, so PR triage/review (🎯T1022.3) is net-new
  surface, not an extension of this tool. It is also a mirror-on-demand
  tool, not a notification source: a PO must already know an issue exists
  (or be told by the 🎯T1022.2 watch cycle) before calling it, and calling
  it unconditionally on every new issue would turn every drive-by issue
  into fleet work before a PO has triaged it — so 🎯T1022.2/.3 compose
  `bullseye_github_sync` as the PO's *accept-as-work* action, not as the
  awareness mechanism itself.



## Design

```
gh issue/pr list (since watermark)        ─┐
  per PO workdir, bounded, capacity-gated  ├─ internal/repowatch cycle (daemon)
state_dir/repowatch/<repo>.json            ─┘
          │ delta (new/updated items)
          ▼
repo-watch brief → PO seat (daemon-delivered, typed `jevons` envelope)
          │
          ▼ PO triages each item against the risk classes below
   ┌──────┴────────────┐
   direct              escalate
   gh comment/label/   jevons_repo_escalate (daemon → ownerquestions
   close / pr review   outbox → blurter), carrying repo, item URL,
   --approve + merge   summary, recommended action, evidence
   (low-risk class only)
```

The daemon owns **awareness**; the PO owns **judgment**; the daemon owns
**escalation transport**. The PO may additionally mirror an issue into its
ledger with `bullseye_github_sync` (pull) when it accepts it as work, which
makes it a frontier leaf and lets 🎯T155 spawn a worker. Mirroring is the
PO's call per item, not automatic: an auto-mirror would turn every drive-by
issue into fleet work before anyone looked at it.

### "Straightforward and low risk" — the direct-action class

Conservative by construction. An item is **direct** only if every line it
touches, and every effect, is on this list; anything else escalates.

Issues:
- Duplicate of an open target or issue → comment with the reference, close
  as duplicate.
- Question answerable from committed docs → answer with a doc link, leave
  open for the asker to close.
- Clear, reproducible, well-scoped bug report → mirror to a target
  (`bullseye_github_sync`), acknowledge on the issue. Fixing it is ordinary
  Build-plane work under the usual oracle rules; the issue closes when the
  target achieves.
- Spam/off-topic → close with a one-line reason.

Pull requests (direct = review, and merge where stated):
- Docs-only, comments-only, typo fixes: approve and squash-merge if CI is
  green and the diff contains no code or config.
- Dependency bumps from a bot (dependabot/renovate) with green CI and a
  minor/patch semver delta: approve and squash-merge.
- CI/config-only changes, any code change, anything from an external author
  that is not in the two classes above, any PR without green CI, any PR
  touching auth, secrets, release, or `.github/`: review with comments if
  useful, **never merge**, escalate with a recommendation.

Merging is Ship-plane. 🎯T1022 is the owner opening Ship for exactly the
two merge classes above and nothing else; the doctrine text names that
grant so it is not read as general PR autopilot (🎯T104 stays in force).

### Escalation payload

One escalation per item (dedup key `repo#number@updated_at`):
repo, item URL and title, author, size (files, +/−), CI state, the PO's
one-paragraph assessment, recommended action, and the answer route (reply
in owner chat or on the GitHub item). Re-notify only when the item changes;
coalesce under the 🎯T1028.3 reminder policy.

## Subdivision

| Child | Scope | Depends on |
|---|---|---|
| 🎯T1022.1 | Decision record: gh polling is the intake; issuepipe path not relied upon. Fitness notes for `bullseye_github_sync` as the PO's mirror tool. Docs only. | — |
| 🎯T1022.2 | `internal/repowatch` daemon cycle + `jevons_repo_watch` tool + `GET /api/repowatch`; typed `repo-watch` brief delivered to the PO. Hermetic with a fake `gh` runner. | — |
| 🎯T1022.3 | Triage doctrine (risk classes above) in persona/guide/brief; PR review procedure; `jevons_repo_escalate` daemon tool over the ownerquestions outbox. Pure classifier `ClassifyRepoItemRisk` with specimens. | — |
| 🎯T1022.4 | Worked example end to end on a real repo: one escalation (mnemo #197) with a blurter event id, one direct action on a synthetic low-risk item with the resulting GitHub URL. | .2, .3 |

.2 and .3 run in parallel; .1 is independent; .4 is the convergence tail.

## Fog map

- **Known:** both bullseye adapters' shape and test status; the PO seat
  inventory and workdirs; the daemon-side blurter transport and its
  permission constraint on seats; the live issue/PR landscape; `gh` scopes.
- **Unknown:** cadence the owner wants (15 min? hourly?) — default to the
  research feed interval (30 min) with the capacity governor degrading it;
  whether POs for non-marcelocantos orgs (arr-ai, squz) should watch at all
  — default yes, opt-out per repo in `state_dir/repowatch/config.json`;
  whether a direct merge should also comment on the PR naming the agent —
  default yes.
- **Blindspot:** none that reshapes the slice. The risk-class list is a
  taste surface the owner may want to tighten or loosen after the worked
  example; that is the 🎯T31.2 spiral, not a reason to withhold .2/.3.
