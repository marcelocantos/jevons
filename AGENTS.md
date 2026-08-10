# Jevons

Coordinator for a community of coding agents under a single CEO agent.
Go daemon (`jevonsd`) + canonical web UI + iOS thin client. Default agent provider is Grok via the claudia harness; backends are pluggable (🎯T148).

**Architecture:** read [docs/architecture-current.md](docs/architecture-current.md)
— the one honest, current description of the system (components, agent
model, persistence, security posture, glossary). Do not trust older
design docs without checking their supersession banners.

**CEO identity (🎯T98):** Jevons-as-CEO is the owner's **alter ego** —
default action, bias, and judgment match what the owner would do in the
same seat. Draft doctrine (owner review residual):
[docs/design/ceo-alter-ego.md](docs/design/ceo-alter-ego.md). Live
behaviour still loads from `internal/config/persona.md` and the fleet
standing brief; this note maps dimensions → surfaces → targets.

## Build

```bash
make              # Build jevonsd
make init         # Install prerequisites (Go 1.26+, C compiler for CGo/SQLite)
make ios          # Regenerate the iOS Xcode project (xcodegen)
```

## Run

```bash
make run          # Build and run jevonsd (or: brew services start jevons)
open http://localhost:13705/   # Canonical web UI
```

Dev mode serves `web/` from disk with hot reload.

## Test

Two universes for live owner-chat work — keep them distinct:

- **A (daily):** `:13705` / `~/.jevons` / `jevonsmcp` — real owner session.
  Touch only when diagnosis needs that context.
- **B (isolated):** `make test-journey` — throwaway port/state/MCP.
  The **preferred E2E net** for owner-visible chat/fleet behaviour (🎯T101).

A **user journey** maps a real owner interaction and runs **end-to-end**;
it **must interact with an agent** (overseer and/or fleet). Hermetic unit
tests and doc greps are not journeys (see `scripts/docratchet/`,
`scripts/journey-suite/portguard/`). After a successful live run, caching
the agent interaction for replay is allowed (🎯T107).

Hermetic `make test` (Go + Node + Playwright UI with mocks) is the fast
gate and is **distinct from** journeys: it never needs Grok or a daemon.
Journeys are opt-in live Grok against a throwaway isolate (`go run
./scripts/journey-suite` / `make test-journey`).

**Journey-or-exception:** when an owner-visible product failure mode is
fixed, land a journey that covers it, **or** an explicit exception in the
diff/PR notes naming why unit/hermetic coverage is enough. Do not treat
unit green alone as the standing net for chat/fleet regressions that only
show up on the real path.

Universe B never defaults to daily `:13705` — the suite refuses that port.
Attaching to a running daily daemon (`make test-live-suite`, chat-smoke*)
is intentional-only, not routine.

```bash
make test         # All: Go + web hermetic (Node) + Playwright UI (hermetic)
make test-go      # go test ./...
make test-web     # node web/scripts/chat_events_test.js
make test-ui      # Playwright perceptual chat UI (mocked WS)
make test-ui-live # Same, against a running jevonsd
make test-journey # Isolated owner-chat + orchestration journeys (Universe B; needs Grok)
make test-live-suite  # Attaches to running daemon (often A — intentional only)
make bullseye     # Standing invariants: build, test, vet, clean tree
```

## Code conventions

### Go
- **slog** for structured logging.
- `cmd/` for binaries, `internal/` for packages.
- Durable state: atomic write-and-rename; malformed state is a hard
  error, never a silent reset. No silent-fail sends/directs.
- **`//go:embed` inputs stay tracked (🎯T360):** never gitignore a file an
  embed pattern names. A clean checkout of HEAD must `go build ./...` green
  with no prior `make` — a gitignored embed input fails a pristine clone, CI
  runner, or release build with `pattern …: no matching files found` instead
  of naming the missing generator step. `internal/cli/help_agent.md` is a
  **committed** mirror of `agents-guide.md`; the Makefile keeps it in sync,
  and `scripts/docratchet` ratchets both the clean-checkout build and the
  mirror drift.

### Web
- `web/index.html` is self-contained; pure logic lives in
  `web/scripts/*.js` (DOM-free where possible so Node tests can require it).
- **Green in the shared clone is not green on master (🎯T398):** many workers
  share one working copy, so `make test-web` there reads everyone's
  uncommitted edits. A suite held green by WIP is red for a fresh clone, a CI
  runner, and the next worker to check master out — master was red from
  8297ae6 until 🎯T388's gate tripped over it. Before calling a web change
  done, run the suite in a detached `git worktree` of HEAD; `scripts/docratchet`
  ratchets that (`TestT398CleanCheckoutWebTestsPass`), as it does the T360
  build. Same shared-clone family as 🎯T376 and 🎯T377.
- Server↔client chat events go through the normalization layer
  (`internal/server/chat_wire.go` + `web/scripts/chat_events.js`) — keep
  both sides in sync and covered by the hermetic tests.

### iOS
- Thin client only: logic belongs server-side; the app wraps the web UI
  in a WKWebView over the pigeon relay. Do not grow native UI.

### General
- Default branch is `master`.
- Keep concerns modular and orthogonal; platform-specific code in
  separate files.
- Convergence targets live in `bullseye.yaml` (🎯Tn); target lifecycle
  rides the PR that changes it.
- Voice targets (anything under 🎯T21/T22/T28) are gated on the 🎯T37
  decision — don't resume voice work without it.
- The `jevons` overseer prompt is embedded in `cmd/jevonsd/main.go` until
  🎯T44 externalizes it; treat it as config-in-code when editing.
- **Fleet spawn (🎯T78):** child implementation work uses Jevons fleet
  agents (`jevons_agent_start` / durable threads), **not** Grok
  `spawn_subagent` / worktree children that die with the parent and never
  show in the RHS fleet panel. Full doctrine: `internal/config/persona.md`
  and `agents-guide.md`.
- **Unified fleet (🎯T114):** aside is a kind of agent (purpose field);
  one deliver/send/push path by name for workers and asides; dual-write
  threads into the agent registry. Docs: persona + agents-guide.
- **Multi-slice fan-out (🎯T111.4):** PO/boss multi-slice missions
  `jevons_agent_start` children early (with parent lineage); solo is fine
  for single-agent tasks. Zero-children failure surfaces in agent_list.
- **PO never implements (🎯T125):** Stratum-1 product owners are
  **spawn-only** for Build work — no solo code/docs/oracle commits, even
  "small" patches. They stay interruptible for overseer/owner directs;
  workers/bosses execute. Instructional doctrine (not a hard spawn-gate)
  unless a later target enforces it. Full text: `internal/config/persona.md`,
  `agents-guide.md`, fleet standing brief.
- **Overseer never parents product workers (🎯T129):** for jevons-repo
  Build work, overseer (`jevons`) routes owner intent to **`jevons-po`**
  and does **not** `jevons_agent_start` product workers with
  `parent=jevons`. Sole spawn parent for product workers = **`jevons-po`**
  (T125). Exception: PO dead/unregistered → rehydrate PO first, then PO
  spawns. Instructional until registry enforcement. Persona + agents-guide
  + fleet brief.
- **Domain portfolios default (🎯T200):** product owners whose workdir is
  under `github.com/marcelocantos/…` belong in the **personal** portfolio
  by default. Membership is declarative path match in
  `~/.jevons/config.yaml` (org fragment `github.com/marcelocantos` covers
  the org). When spawning a new marcelocantos PO, they nest under Personal
  — do **not** leave them unassigned under the overseer root unless the
  owner assigns a different portfolio (e.g. minicades for squz). RHS tree
  uses path membership, not agent-name parsing. Agents-guide + fleet brief.
- **Filing reflex (🎯T130):** when a real product gap, repeated failure
  mode, or standing behavioural rule appears mid-work → **file or
  prompt-file a bullseye target** (name + acceptance) in the **same turn**
  — not only "standing rule" / "going forward" / "from now on" /
  "we should always…" in chat. Ceremony: `jevons_target_file` and/or
  bullseye MCP (`bullseye_commit` track). Related: 🎯T92 ambient RSI,
  🎯T129 hierarchy. Residual: one-off flukes may skip filing.
- **Ambient RSI coach (🎯T243 / T92):** harness coach drip-reads owner main
  chat (priority), eventlog, and session transcripts; posts **judgments** to
  the overseer (`jevons_rsi_coach_cycle` / configure / status). Overseer alone
  files / alerts / briefs PO / ignores. Coach never calls bullseye. Residual
  phrase-list mint (`JEVONS_RSI_MINT` / `jevons_rsi_cycle`) is not product
  path. Filing reflex is the mid-turn agent half of the same mission.
- **Retrospective coach mine (🎯T353):** *fine sensors, coarse conclusions.*
  The drip cursor starts at EOF, so the coach also makes a **bounded backward
  pass** over history — git commits (repair churn, reverts), the eventlog
  tail, owner chat, and session transcripts — on its own slow cadence
  (`retro_interval_sec`, default 6h; `retro_lookback_hours`, default 7d).
  Judgments carry commit SHAs / session ids as evidence and are marked
  `Mode: retrospective`. Delivery stays sparse: retro rate cap + the retro
  value bar (one-off git noise and bare phrase-friction never reach the
  overseer) + T333 disposition suppressions. Run by hand with
  `jevons_rsi_coach_cycle mode=retro|both`; dials via
  `jevons_rsi_coach_configure`. Retro never advances the drip cursor and
  never calls bullseye.

- **Capacity-aware background (🎯T359):** ambient cycles (research T356,
  audits T357, coach T243/T353, sentinel extras) are admitted / degraded /
  deferred by one holistic read of remaining budget and concurrent load
  (`internal/capacity` pure policy + daemon governor), not by each loop's own
  soft cap. **Owner turns and open Build missions outrank all ambient
  background**; control-plane repair is the load-bearing background class and
  stands down last. Ladder: elevated → reduced pass; tight → load-bearing
  only; critical → owner/Build only + one **sticky** owner notice. Composes
  🎯T36 (clamp remains the safety net), 🎯T137 (subscription USD is an
  estimate and never denies work; tokens and load do), 🎯T325.2 provider soft
  caps. Surfaces: `jevons_capacity_status`, `GET /api/capacity`,
  `~/.jevons/capacity.json` (`daily_token_budget` unset = unknown, never
  invented). Residual: exact vendor quotas class-3; preemption advisory (defer,
  not mid-pass kill); not optimal scheduling. Design:
  `docs/design/capacity-aware-background.md`.
- **Oracle-first completion (🎯T31 / 🎯T31.1):** bare "done" / complete /
  finished without **oracle evidence** (named test + green, and/or
  commit SHA) or **explicit accepted-risk / class-3** language is **not
  accepted**. Overseer is the independent gate (attestation ≠ execution).
  Instructional residual + pure classifier. Persona + agents-guide +
  fleet standing brief.
- **Finished work auto-deregister (🎯T165 / 🎯T195):** when a **work**
  agent’s terminal report claims done — including imperfect bare done
  without oracle markers — the product **stop+Removes** it from the live
  fleet (not persona-only hygiene). Ledger achieve of a mission TargetID
  also reaps engaged implementers. Hermetic: spawn fixture → done/achieve
  path → `agent_list` omits name. Residual: POs and overseer stay;
  multi-target agents without matching TargetID stay; deliberate stop
  without kill still resume-friendly; T90 anomaly supervisor separate.
- **Greenfield oracle elicitation (🎯T31.2):** for new software (no
  external reference), co-develop an **oracle-coverage map** alongside
  design — **pinned** / **fuzzy** / load-bearing **when X expect Y**
  examples (plus taste / spike). **SPIRAL** (design → thin slice → owner
  reacts → intent sharpens → new oracle); refuse production on still-
  fuzzy parts. **DECIDABLE-FROM-TASTE** sort; **PROPORTIONALITY +
  GOODHART** (spikes OK un-oracled; pin only with load-bearing examples).
  Pure `CoverageMap` / `ClassifyDesignClause` helpers. Residual:
  instructional + pure model; not a hard daemon block; T29 UI + owner
  process-fidelity class-3. Design: `docs/design/greenfield-oracle-elicitation.md`.
- **Frontier = ready set (🎯T262.1):** frontier = unblocked ready leaves;
  pick among them is indifferent/policy, not discovery of a hidden "next
  ticket." A queue is frontier size ≤1 with invented order. Multi-agent
  default: worker per ready leaf subject to engagement policy. Anti-pattern:
  framing bullseye as "the next ticket" oracle. Design:
  `docs/design/frontier-as-ready-set.md`. Does not unpark T254 or claim
  T262.4 owner accept. Persona + agents-guide + fleet standing brief.
- **Unattended frontier auto-spawn (🎯T155):** when a new frontier leaf is
  filed that is not design-gated / needs-owner / design-discussion /
  parked-for-design, **`jevons-po` spawns a fleet worker** under
  **`parent=jevons-po`** in the same operational cycle — kick off all
  non-design frontier work continuously; do not wait for the owner.
  Skip design-gated (T112 / T67 / T29-class) and blocked targets until
  unblocked or owner opens design. Instructional residual. Persona +
  agents-guide + fleet standing brief.
- **File→spawn same turn (🎯T193):** when a **Build-plane** target is filed
  (owner `target:` aside / mid-session), **PO spawns a named worker** under
  **`parent=jevons-po`** in the **same turn** as filing — not ledger-only.
  T130 files; T193 spawns. Skip design-gated / blocked-on-human /
  parked-for-design / pure documentation. Related: 🎯T155 continuous
  frontier kick-off. Instructional residual. Persona + agents-guide +
  fleet standing brief.
- **PO proactive-until-empty-then-sleep (🎯T325.1):** when the
  product-scoped frontier has unblocked ready leaves, the PO continues
  spawn/brief until empty or blocked — not a one-shot pass that strands
  work. When empty (or only design-gated / blocked / parked /
  already-engaged leaves remain), the PO sleeps/idles without open-mission
  thrash; stays interruptible for owner/overseer directs. Pure helpers:
  `ClassifyPOProactive` / `ClassifyFrontierLeaf` /
  `POOpenMissionForProactive`. Design:
  `docs/design/life-and-work-org-map.md` §8. Complements T155 / T193 /
  T244. Residual: instructional + pure classifier; hard daemon sleep gate
  may follow. Persona + agents-guide + fleet standing brief.
- **Worker names literal dots (🎯T197):** hierarchical target ids in fleet
  worker names keep **literal dots** — never digit-squash
  (`jv-t27.2-config` not `jv-t272-config`). Flat ids unchanged
  (`jv-t159-seal`). Names free-form otherwise. Persona + agents-guide +
  fleet standing brief.
- **Shared hot files are compare-and-swap (🎯T376):** concurrent workers share
  one working tree, so a full-file `Write` derived from an older `Read` silently
  reverts whatever another worker landed in between — it happened three times
  while landing 🎯T370, on `web/index.html`. `internal/treeguard` + the
  `PreToolUse` hook in `.claude/settings.json` refuse such a write and **name
  the lines that would be lost**; recover by re-reading and re-applying on top
  of current content, never by disabling the guard. Guarded set:
  `treeguard.DefaultGuardedPaths` (cockpit HTML, Makefile, embed.go, the
  instruction files, `bullseye.yaml`). `make all` builds `bin/treeguard`; a
  missing binary reports a visible non-blocking error rather than degrading to
  no guard. Sibling: 🎯T377 (shared `.git` index — stage and commit with
  explicit paths, never `git add -A`).
- **Commit only your own paths (🎯T377):** the clone has one index, so `git add`
  writes shared state and a bare `git commit` turns whatever every worker has
  staged into one tree under one worker's message. That is how `29e69e8`
  ("refactor(web): T372 …") came to contain the whole of 🎯T370 —
  `git log --diff-filter=A -- web/scripts/fleet_cycle.js` still answers 29e69e8,
  and reverting T372 would silently revert T370. Commit as
  `git commit --only <your paths>`, then confirm with `git show --stat HEAD`;
  a worker-owned `GIT_INDEX_FILE` is equally sound. The `pre-commit` hook
  (`scripts/hooks/pre-commit` → `internal/commitscope`) refuses the sweeping
  forms — bare `git commit`, `-a`, `-i` — by reading which index git is
  committing from, and names the paths that would have gone in. Deliberate
  whole-index commits are `JEVONS_COMMIT_SCOPE=off git commit …`, never
  `--no-verify` reflexively. `make` installs the hook (`bin/commitscope
  --install`), since git never populates `.git/hooks` from the tree and a guard
  waiting to be copied by hand is absent in the fresh clone that needs it; a
  pre-commit hook this repo did not write is left alone and reported as
  leaving the clone unguarded. Do **not** redirect `core.hooksPath` — that
  would disable git-lfs's own hooks. Sibling: 🎯T376 (same root cause, working
  tree rather than index).
- **Run gates so the status survives (🎯T386 / 🎯T396):** a pipeline's exit
  status is the **last** command's, so `go test ./... | tail -20` reports
  tail's success — which is unconditional. That is how a suite that died on a
  timeout panic was cited as a green, twice in one session; a fabricated green
  is worse than no test, because it retires a target. Two siblings: bash's
  `PIPESTATUS` does not exist in the zsh this harness runs (and zsh's own
  `pipestatus` indexes from 1, so `${pipestatus[0]}` is empty too — an empty
  status is not zero), and the harness has itself relayed a background gate as
  "exit code 0" for a `go test` that exited 1. Run every gate as
  `bin/gate -- make test-go` and cite the `GATE … exit=0 GREEN id=…` line it
  prints: it runs the command as a process (no shell, no pipeline), exits with
  the command's own status, and records that status under `~/.jevons/gates`
  where `bin/gate last` reads it back **in band** — independent of what the
  harness claimed about a background run. `exit=unknown` is never a pass, and
  `GREEN` is the only verdict citable as one (`SUSPECT` = zero exit over
  panic/timeout/race/FAIL output). `make bullseye`'s test step runs under the
  gate. Report time closes the loop: `bin/gate check` reads a finish report and
  the daemon runs the same check on the notify path, prepending a FALSE-GREEN
  banner ahead of a report whose own cited evidence — piped gate, empty status,
  quoted failure, or a `GATE` id with no record behind it — contradicts the
  pass it claims. Ratcheted by `scripts/docratchet`. Residual: the banner marks
  a report, it does not block delivery.
- **Status language in progress vs live (🎯T176):** always say **in progress**
  for a registered/running worker whose product is not yet owner-visible;
  never call a running worker **live**. Reserve **live** / **landed** /
  **shipped** for product evidence only (commit SHA + hard-reloadable UI, or
  proven API on the daily path). Lab/test uses of "live" (journeys,
  `test-ui-live`) stay technical jargon. Persona Communication Style +
  agents-guide + fleet standing brief.
- **Daemon rebuild + restart (🎯T188 / 🎯T191):** after any Build that
  changes the running `jevonsd` binary or server-side behaviour, rebuild
  and restart the daily daemon without asking the owner. Owner never
  restarts by hand. Do not claim a daemon-path fix done until restart
  succeeds. Invoke **detached**:
  `nohup scripts/restart-daily-jevonsd.sh >>"$HOME/.jevons/restart-daily.log" 2>&1 &`
  Detached is still the blessed invoke because it stops the caller
  blocking on the bounce — since 🎯T405 it is no longer what makes the
  bounce survive. Script path: `scripts/restart-daily-jevonsd.sh`. Pure
  static web-only may hard-reload only. Residual: session drop until
  T40/T171. Persona + agents-guide.
- **The restart is supervised (🎯T405):** on 2026-08-10 a worker's restart
  killed the daemon, the daemon's shutdown stopped that worker, and the
  script died with it five seconds before the step that starts the
  replacement — the fleet stayed down until the owner opened the cockpit
  and found it dead. The script had documented that exact hazard since its
  first version, as an instruction to callers (*invoke me detached*), and a
  correctness property that depends on every caller remembering a
  convention is not a property. So the script now **re-execs itself into
  its own session** through `bin/detach` before doing anything: being
  invoked wrongly cannot cause an outage. The deeper finding was that
  nothing watched the result at all — thrash policy (T218), lock
  serialisation (T392.5) and HEAD build snapshots (T254.2) are elaborate
  about restarting and silent about whether anything is serving
  afterwards. The launchd job **`com.marcelocantos.jevons-watchdog`**
  (`make watchdog-install`, `make watchdog-status`) probes the port every
  30s from outside every process tree a restart tears down, restarts
  through the same script once an outage outlives the grace window, paces
  its attempts and never gives up. An outage the owner hears about twice:
  out of band while it is down, and in owner chat once the daemon is back
  to report it — the watchdog cannot write a chat line into a daemon that
  is not running, so the recovery is recorded to disk and the daemon
  reports it on the way up (`internal/supervise`). Oracles kill a real
  restart mid-bounce and SIGKILL a real foreground caller's process group
  (`cmd/jevons-watchdog/oracle_test.go`, with a control that shows the
  test still detects the regression). Persona + agents-guide.
- **Achieve reports need activated daily path (🎯T194):** a target whose
  product path is served by daily jevonsd (HTTP API, compiled server,
  non-static) is **not achieved** until detached `restart-daily-jevonsd`
  succeeds (or proven zero-downtime upgrade) **and** a live probe of the
  product path is green (e.g. curl non-404). Hermetic unit green is
  **necessary not sufficient** — hermetics alone do not close daemon/API
  work while a stale binary may still serve. Finish reports must cite
  daily-path evidence (restart success and/or live probe). Pure web
  static may hard-reload only (T188). Pure helper: `HasDailyPathEvidence`.
  Residual: instructional + pure classifier; not a hard achieve block.
  Persona + agents-guide + fleet standing brief.

## Project structure

```
jevons/
├── Makefile              # Build orchestration (Go-only since 🎯T43)
├── cmd/jevonsd/          # Coordinator daemon entry point
├── internal/             # server, butler, thread, fleet, mcpserver,
│                         # cost, auth, discovery, transcript, cli
├── web/                  # Canonical web UI (served by jevonsd)
├── ios/Jevon/            # iOS thin client (WKWebView + pigeon)
├── scripts/              # journey-suite, chat-smoke, chat-ui-test, …
├── docs/                 # charter, architecture-current, design docs
└── bullseye.yaml         # Intent ledger
```

## Delivery

Merged to `master` via squash-only PRs; releases via the `/release`
flow (Homebrew tap `marcelocantos/tap/jevons`, brew-services daemon).
