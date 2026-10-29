# Owner-session handover — 2026-10-24

The owner set an open goal: play with the cockpit as the owner would, fix
anything unnecessary, annoying, incorrect or slow, and stop when Jevons
feels trustworthy for coding work at the UI, UX and semantic levels. Prefer
Cursor for fleet work and watch token burn. Only the owner can sign off;
they have not yet.

Repo: `~/work/github.com/marcelocantos/jevons`, branch
`steer-modes-stop-guards-seat-stops`. It is about 775 commits off `master`,
and which branch is canonical is the owner's call. Development daemon:
supervisord program `jevonsd` on `:13705`. Activate with
`scripts/restart-jevonsd.sh`, then check `supervisorctl status jevonsd` says
RUNNING (a bad `~/.jevons/config.yaml` makes it FATAL).

## Open right now

**Owner-session checkpoint, 2026-10-24 evening:** this section retains the
original handover below; the current evidence is in
[the cleanup verification map](../design/cleanup-oracles-2026-10.md).
Claudia `83ca3cc8` and its matching sidecar are now active after the owner's
explicit restart approval and full clean gate **GREEN `a6ec64f6`**. Fresh
normal broker requests returned Claude and Grok HTTP 200, achieving 🎯T1209.
Jevons stayed on verified `f2657e4f`, with seat identities and startup flags
unchanged. The activation manifest is at
`~/.local/lib/claudia/owner-cleanup-83ca3cc/activation-manifest.json`.
The old Grok credential proposal below is superseded by the broker-owned
credential reader and bounded normal noninteractive renewal now running.
Usage-reader diagnostics remain under 🎯T1212. The overall daily-driver
goal, lifecycle authority 🎯T1074.9 and independent MCP execution 🎯T1074.10
remain open; component gates do not certify those contracts.

1. **Grok usage bar shows "?"**. The owner asked for this, so it comes first.
   The billing reader in claudia (`plan_usage_grok.go`) uses the grok CLI
   token in `~/.grok/auth.json`. That token lasts 6 hours and refreshes only
   when the `grok` CLI starts; nothing starts it any more, and the
   headless-refresh path was removed (`errGrokCLIRefreshRemoved`). Login
   health (`/api/plan-usage/auth`) checks a different credential, the seats'
   `xai-oauth` in claudia's `omp.Store` (broker-held and refreshed), so it
   says "ok" and the renew button never shows.
   - Proposed fix, awaiting owner go-ahead:
     - (a) claudia: have the broker make the billing GET with
       `omp.Store.AccessToken(ctx, "xai-oauth")`, falling back to
       `auth.json`. Whether the endpoint accepts that token is unverified;
       find out in code, never by reading the Keychain by hand.
     - (b) jevons: report a usage-reader 401 as a sign-in fault for that
       provider, so the bar reads as needing attention and the renew action
       appears.
2. **Claude usage bars stick bad until reload** (owner report). Not
   diagnosed. Pushes are wired: `cmd/jevonsd/planusage.go`
   `OnUpdate: srv.FanPlanUsage`. The UI path is `ui/src/components/PlanUsageBar.tsx`;
   with mux present, HTTP polling is off and only pushes update it. A probe
   that records pushes is at
   `/private/tmp/claude-501/.../scratchpad/planwatch.cjs` (Playwright, read-only
   UA). Next step: record frames across a daemon restart and a provider
   failure, and see which frame leaves the bar bad. A 6-minute recording at
   10:45–10:51 (including a daemon restart) showed Claude good in every
   frame, so it did not reproduce; Grok's reason in the same frames read
   "token rotation via a grok one-shot failed: headless CLI turn removed",
   which confirms item 1's cause.
3. **Ledger written by hand.** jevons-po rewrites `bullseye.yaml` with
   `python3` heredocs from Bash (T546 violation). On 2026-10-23 09:05 that
   left the ledger unparseable and the Frontier tab unavailable; repaired by
   revert plus `bullseye apply`. The guard against this exists only as a
   Claude Code hook and the sidecar's file-tool rule; Bash interpreter
   writes from sidecar seats are not blocked.
   - Next steps: a pre-commit check in `internal/commitscope` that refuses
     a staged `bullseye.yaml` that does not parse; release bullseye with
     T104 (`context_append`, landed in the bullseye repo, not released;
     installed is 0.57.0) so the PO has no reason to hand-edit.
4. **T1198 (filed): red trunk is not surfaced.** On 2026-10-23 21:39
   `TestT742…` went red (a one-line const the ratchet's regex missed; fixed
   `0e290e56`, gate `45a733c5`). Every landing then failed post-merge
   (T1197 runs docratchet), and the PO wrote ledger notes for 13 hours (82
   of 91 commits) without telling anyone. Needs a typed `trunk_red` outcome
   from integrate, an owner fault, and a fixer spawn.
5. **23 parked seats hold unlanded worktree commits** (pause-era). The
   owner said cleanup is not this session's job. The product gap is that
   parked seats with nothing external to wait on are never aged out. File
   or fix that; don't hand-clean.

## Landed and live this session

All committed; verify with `git log`. Highlights:

- **Ledger:** all 110 landed-but-open targets dispositioned.
- **Envelopes:** a trailing envelope counts (`c8e6f28d`, `ced96c83`), and
  finished workers now reap.
- **Token burn:**
  - Overseer no longer gets a copy of every worker report (`e1c00802`).
  - Placement-correction notice limited to one per author per hour.
  - Claudia v0.64.0 released: restart nudges go only to seats that were
    mid-turn (T214).
- **Fleet liveness:**
  - Broker restart re-adopts seats (T1189).
  - PO/auditor stall detection (T1192); unacted child reports wake the PO
    (T1193); owner missions (T1195).
  - Sweep ceiling counts only sweep-minted seats (`06783ff8`). Frontier
    sweep re-enabled (`max_live_workers: 4`).
- **Integrate:** post-merge gate tests touched packages plus docratchet
  (T1191, T1197).
- **Cockpit:**
  - Show earlier pages until real messages appear (`9a3cb25a`,
    `1364793a`, `a2ed9cfb`).
  - Parked and idle rows fold; the mass-stop alarm is capped; the Coach tab
    says when the coach is off; Questions loads in about 0.4s; the "ready"
    badge shows only on Frontier.
- **Owner origin:** an unnamed non-browser HTTP send is agent origin, not
  owner (`server/agent_send.go`). jevons-po's curl report had painted as
  the owner's message.
- **Set-aside reap:** a worker is reaped when its target is set aside.
- **Routing:** `~/.jevons/llm-portfolio.json` prefers cursor (backup
  `.bak-pre-cursor-20261023`).

## Rules that bit this session

- Never Read/Write/Edit `bullseye.yaml`. Use bullseye MCP or CLI. The
  treeguard hook refuses Bash that mentions the file.
- Commit with `git commit --only <paths>`. For `ui/src` changes, rebuild
  `ui/bundle.zip` (`make ui-build`) in the same commit.
- Cite gates as `bin/gate -clean -- …` and quote the `GATE … GREEN id=` line.
  zsh does not word-split `$var`; use `${=var}`.
- Don't read Keychain or credential items, don't run `claudia broker
  auth-recover` yourself, and don't type into the owner's composer.
- `bullseye_commit achieve` requires an `id=` gate whose measured commit
  contains every cited SHA.
- Before claiming a fix, observe it live: a probe, a screenshot, or a log
  line on `:13705`. Hermetic green has twice been false here (T1186).

## Token posture (2026-10-24 morning)

Claude weekly 6% (hot); Codex weekly 73%; Cursor monthly 75%. Grok's reading
is unknown because of item 1, but its last value was about 18% weekly. Fleet
runs mostly on Cursor.
