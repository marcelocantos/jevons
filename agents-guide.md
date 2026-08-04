# Jevons Agent Guide

Jevons is a remote control system for coding agents — a butler/CEO over
a fleet of agents. It consists of a coordinator daemon (`jevonsd`) and a
browser chat UI (also wrapped by the iOS app).

**CEO identity (🎯T98):** the overseer is the owner's **alter ego** in the
CEO seat (default action/bias/judgment). Doctrine draft for owner review:
`docs/design/ceo-alter-ego.md`. This guide carries the operational
slices workers must obey (fleet spawn, local delivery, PO hierarchy, filing).

Agent backends are **pluggable via claudia** (🎯T148). The default is
Grok Build (`ProviderGrok`: Task mode and Session ACP). Overseer or PO
can choose another claudia-supported backend **per spawn** (e.g. Claude)
without restarting `jevonsd`. Residual: Claude Session re-stitch and
Bedrock are claudia-side (pass-through provider strings are accepted).

## Architecture

```
  browser / iOS  ──WebSocket──►  jevonsd  ──spawns──►  Jevons (default: Grok Session ACP)
                                       ──manages──►  workers / threads (provider-selectable)
                                  MCP ◄─────────────┘ (tool calls)
```

- **jevonsd**: HTTP/WebSocket server. Runs the overseer as a Session ACP
  process (default Grok; pluggable via 🎯T148), exposes an in-process MCP
  server for worker/thread management, collects harness usage (Grok
  sessions plus other providers when configured), and serves the web UI.
- **Primary UI**: browser at `http://localhost:13705/` (`/ws/chat`); the
  iOS app wraps the same UI over a paired QUIC relay.

## Install (multi-step — not done until all succeed)

Canonical second-user path lives in root `README.md` (🎯T47 residual docs).
Same-machine browser use is the supported docs-only path today.

1. **Binary**: `brew install marcelocantos/tap/jevons`
2. **Grok CLI**: install Grok Build and auth (`grok login` or `XAI_API_KEY`);
   ensure `grok` is on `PATH` or at `~/.grok/bin/grok`.
3. **Service** (always-on): `brew services start jevons`
4. **Verify listening** (do **not** use bare `curl` against `/mcp` —
   MCP only answers JSON-RPC POSTs):
   ```bash
   lsof -iTCP:13705 -sTCP:LISTEN
   ```
5. **Optional device pair** — `jevonsd --pair <id> --relay <url>` + Jevon iOS
   app QR scan (source under `ios/`; no App Store binary yet; full
   onboarding is 🎯T14).
6. **MCP attach**: on boot, jevonsd auto-registers its HTTP MCP into the
   overseer's client config when possible. For an external MCP client
   (e.g. Claude Code talking *to* jevons), after restarting that client:
   ```bash
   # Prefer 127.0.0.1 (loopback default, 🎯T6); name matches product default jevonsmcp.
   claude mcp add -s user --transport http jevonsmcp http://127.0.0.1:13705/mcp
   # Grok Build recovery (if auto-ensure did not stick):
   #   grok mcp add --transport http jevonsmcp http://127.0.0.1:13705/mcp
   ```
7. **Confirm tools** via `jevons_thread_list` or `jevons_cost`.

## Running manually

```bash
jevonsd --port 13705 --workdir ~/projects
open http://localhost:13705/
```

## Key concepts

- **Jevons (overseer)**: Session ACP process managed by jevonsd (default
  Grok; other claudia providers via config/`provider=`).
- **Thread**: Durable semantic unit (transcript + metadata + status), not
  tied to a live process. Process = disposable cache.
- **Workers / agents**: Task or Session workers; `provider=` per spawn
  (default Grok).
- **Sessions on disk**: provider-specific roots (e.g.
  `~/.grok/sessions/<encoded-cwd>/<session-id>/` plus
  `~/.grok/active_sessions.json`; Claude inspect discovery is 🎯T213).

## Chat markdown (web UI)

- **Mid-stream (🎯T150):** assistant bubbles paint progressive markdown via
  vendored `streaming-markdown` (`web/scripts/smd.js` +
  `streaming_markdown.js`). Closed emphasis (e.g. `**bold**`) becomes real
  bold as soon as both delimiters arrive — not raw source, and not delayed
  until end of turn.
- **Seal:** full `marked` parse (plus mermaid 🎯T59 and highlight.js 🎯T74).
- **Fence hygiene:** T145 `ensureFenceNewlines` and T147
  `coalesceAssistantText` keep smushed `prose.```lang` from breaking fences.
- **Never** use plain `textContent` of markdown source as the live stream
  default.

## MCP tools

- **Threads**: `jevons_thread_adopt`, `_list`, `_status`, `_spawn`,
  `_direct`, `_takeover`, `_remove` — durable threads over provider
  sessions (default Grok; other providers via spawn/`provider`).
- **Cost**: `jevons_cost` — burn-rate snapshot (multi-harness when
  configured; Grok session tails remain the historical path).
- **Workers**: `jwork` (sole ephemeral primitive — one self-contained
  task, runs to completion) and `jevons_agent_*` (named durable agents).
  The legacy `jevons_*_session` tools were removed (🎯T41).
- **MCP resilience (🎯T60)**: `jevons_mcp_reconnect` — from inside the
  overseer chat, re-attach dropped MCP servers (all, or one named
  server) without session rotate or TUI `/mcps`. Cycles
  `grok mcp disable` → `enable` so tools from servers like github/gmail
  work again in the same session.

## Fleet spawn path (🎯T78)

**Default for child implementation work:** create a Jevons fleet agent or
durable thread so the child **outlives the spawner** and can show in the
RHS fleet panel (🎯T72 family).

| Need | Tool |
|---|---|
| Named long-lived PO/boss/worker | `jevons_agent_start` → `jevons_agent_send` |
| Durable owned conversation / aside | `jevons_thread_spawn` → `jevons_thread_direct` (or unified push/send by name) |
| One-shot task, no ongoing ownership | `jwork` |

### Unified participant model (🎯T114)

An **aside is a kind of agent** (purpose=`aside`). Work agents use
purpose=`work`. One registry id space and one deliver path:
`jevons_event_push` / Deliver resolves **thread or agent by name** — no
"no thread X" when the agent exists. UI: work agents **and** asides on the
RHS fleet tree (asides use 💡 chrome; 🎯T136) — not a top attention chip
bar. Same underlying registry records.

**Do not default to** Grok `spawn_subagent` (or worktree subagents that
die with the parent). Those children are not first-class fleet entries,
vanish on parent interrupt, and break multi-agent observability.

Hard suppress of harness subagent spawn is optional where the Grok CLI
allows it; until then this convention plus jevons MCP tools is the
enforced path. Brief every new agent with target IDs and ownership —
never bare "go".

### Worker names: literal dots for hierarchical target ids (🎯T197)

Agent names are free-form. When a name encodes a **hierarchical** bullseye
target id, **keep the literal dots** — never digit-squash.

| Target | Correct worker name | Wrong (digit-squash) |
|---|---|---|
| 🎯T27.2 | `jv-t27.2-config` | `jv-t272-config` |
| 🎯T47.1 | `jv-t47.1-docs` | `jv-t471-docs` |
| 🎯T159 (flat) | `jv-t159-seal` | unchanged — flat ids stay flat |

Digit-squash makes `T27.2` vs `T272` (or `T47.1` vs `T471`) ambiguous in
the RHS fleet list. Residual: flat ids (no sub-target segment) stay as
today (`jv-t159-seal`). Optional suffix (`-config`, `-docs`) is free-form.

### Multi-slice fan-out (🎯T111.4)

PO/boss agents on **multi-slice** missions must spawn `jevons_agent_start`
children (with `actor`/`parent` lineage) rather than unbounded solo
exploration. Single-agent tasks remain fine. Zero children after planning
on a multi-slice brief is a failure mode (`jevons_agent_list` fan-out
check). Prefer agents over threads for named long-lived workers.

### Unattended frontier auto-spawn (🎯T155)

When a **new frontier leaf** is filed that is **not** design-gated /
needs-owner / design-discussion / parked-for-design, **`jevons-po` spawns a
fleet worker** under **`parent=jevons-po`** in the **same operational cycle**
— do not wait for the owner to request a frontier review.

- **Standing rule:** kick off all non-design frontier work **continuously**;
  new unattended leaves get a worker **immediately**.
- Overseer routes to PO (🎯T129); PO spawns, workers execute (🎯T125).
- **Skip:** design-gated (T112 / T67 / T29-class) and blocked targets stay
  unspawned until unblocked or owner opens design.
- **Related:** 🎯T193 file→spawn same turn (owner-filed and mid-session Build).
- **Residual:** instructional; no daemon auto-spawn unless later enforced.

### File→spawn same turn (🎯T193)

**T130** files the target; **T193** spawns the worker. Do **not** leave
Build filings **ledger-only**.

When a **Build-plane** target is filed — owner via `target:` aside /
`jevons_target_file`, or mid-session by overseer/PO — **`jevons-po` spawns a
named worker** under **`parent=jevons-po`** in the **same turn** as filing
unless the target is design-gated or parked.

- **Same turn:** `jevons_agent_start` (or route to PO) before the turn ends.
- Overseer routes to PO (🎯T129); PO spawns, workers execute (🎯T125).
- **Skip (file without spawn):** design-gated (e.g. OAuth app pins, T112 /
  T67 / T29-class), blocked-on-human / needs-owner / parked-for-design, and
  pure documentation / docs-only.
- **Related:** 🎯T155 continuous unattended frontier kick-off.
- **Residual:** instructional; no daemon auto-spawn unless later enforced.

### PO never implements (🎯T125)

**Stratum-1 product owners never implement themselves** — including small
patches, oracle/tests, and docs commits. Mirror rule: **spawn-only for Build work**;
no solo code/docs commits by the PO.

| Role | Does |
|---|---|
| **PO (Stratum 1)** | Plan, brief, spawn workers/bosses, collect evidence, stay free for overseer/owner directs |
| **Boss / worker** | Execute (edit, test, commit) under the brief |

POs stay **interruptible** so redirects from above are not blocked by a
solo coding session. **Residual:** instructional doctrine, not a hard
daemon spawn-gate, unless a later target adds enforcement.

### Overseer never parents product workers (🎯T129)

For **jevons-repo Build work**, the overseer (`jevons`) routes owner
intent to **`jevons-po`** and does **not** `jevons_agent_start` product
workers with `parent=jevons` (or actor=jevons as parent).

| Role | Spawns product workers with parent= |
|---|---|
| **Overseer (`jevons`)** | Does **not** — routes to PO only |
| **`jevons-po` (sole spawn parent)** | Yes — bosses/workers under T125 |

**Exception:** PO dead/unregistered → rehydrate or start PO first, then
PO spawns. **Residual:** instructional until a later target adds registry
enforcement (reject wrong parent).

### Filing reflex (🎯T130) — doctrine first, narrative second

When a **real product gap**, **repeated failure mode**, or **standing
behavioural rule** appears mid-work → **file or prompt-file a bullseye
target** (name + acceptance) in the **same turn** — not only chat promises.

**Trigger phrases** that require filing (not "I'll remember"):
- "standing rule"
- "going forward"
- "from now on"
- "we should always…"
- plus: repeated failure, hierarchy slip, logging gap, UX pain, fleet doctrine

**Ceremony:** `jevons_target_file` and/or bullseye MCP (`bullseye_commit`
op=track / file tools). Related: ambient RSI coach **🎯T243** (judgments →
overseer; not direct mint), residual **🎯T92**, hierarchy **🎯T129**.
**Residual:** one-off flukes may skip filing; judgment allowed.

## Oracle-first completion (🎯T31 / 🎯T31.1)

**Bare "done" is not accepted.** Finish reports must carry either:

1. **Executable oracle evidence** — named test command + green result,
   and/or commit SHA that lands the oracle; or
2. **Explicit accepted-risk / isolated class-3** language for residual
   that stays human-gated.

**Attestation ≠ execution** (oracle-first rule 9): the overseer, who did
not do the work, is the independent gate. Self-attested "complete /
finished / achieved" prose without evidence is refused for production
or retire claims. Do not substitute adjacent greens ("it compiles",
"agent replied") for the product property under test.

**Residual:** instructional + pure `ClassifyCompletionReport` heuristic;
not a hard daemon block.

## Greenfield oracle elicitation (🎯T31.2)

For **new software** (no external reference), co-develop an
**oracle-coverage map** alongside design:

| Status | Meaning |
|--------|---------|
| **pinned** | Executable checks seeded from load-bearing examples |
| **fuzzy** | Still open — production refused until pinned enough to test |
| **taste** | Class-3 perceptual residue — single owner accept/reject |
| **spike** | Exploratory on purpose; intentionally un-oracled |

**SPIRAL:** design → thin slice → owner reacts → intent sharpens → new
oracle (not waterfall). **Unit of intent:** *when X, expect Y*.

**DECIDABLE-FROM-TASTE:** separate decidable criteria from irreducible
taste before production work. **PROPORTIONALITY + GOODHART:** spikes may
stay un-oracled; pin only with load-bearing examples (rule 6).

Pure helpers: `CoverageMap` / `ClassifyDesignClause` /
`ParseLoadBearingExample` (`internal/mcpserver`). Design note:
`docs/design/greenfield-oracle-elicitation.md`.

**Residual:** instructional + pure map model; not a hard daemon block;
rich T29 surface and owner process-fidelity validation remain
class-3 / follow-ups.

## Status language: in progress vs live (🎯T176)

Hard vocabulary when reporting fleet / worker status to the owner (overseer
voice; workers use the same words in finish reports):

| Say | When |
|---|---|
| **in progress** | Worker is registered or running, but product is **not yet owner-visible** |
| **live** / **landed** / **shipped** | Only with product evidence: commit SHA + hard-reloadable UI, or proven API on the daily path |

Never call a registered or running worker **"live"** — that implies product
on the wire. Residual: journey-suite / `test-ui-live` / daemon-attach uses of
"live" stay lab jargon, not status language about workers.

## Delivery: local by default (🎯T104)

Owner vocabulary is **literal**:

| Said | Means |
|---|---|
| **master** | Local `master` branch |
| **locally** / **local only** | No `git push`, no GitHub PR, no CI merge |
| **merge to master locally** | Cherry-pick/merge onto local `master` only |

**Done** for fleet work = commits + evidence + notify overseer — **not**
"opened a PR" / "merged to origin/master". Bare done without oracle or
accepted-risk is also refused (🎯T31.1).

**Finished work agents leave the fleet without hand-pruning (🎯T165 / 🎯T195):**
when a work agent's terminal report claims done — including imperfect bare
done without oracle markers — the product stop+Removes them from the
registry (RHS / `agent_list` omit the name). When a mission target is
achieved on the bullseye ledger, work agents engaged on that TargetID are
also reaped. Residual: POs and overseer stay; multi-target agents without
a matching TargetID stay; deliberate `jevons_agent_stop` without kill
still leaves registration for resume; T90 deep anomaly supervisor is separate.

Do **not** re-expand a local merge order into continuous origin/PR
shipping because a PO already opened remotes. Remote delivery only when
the owner **explicitly** asks to ship/push/PR.

## Daemon rebuild + restart (🎯T188 / 🎯T191)

After any **daemon-path** Build (binary or server-side behaviour), rebuild
and restart daily `jevonsd` without asking the owner. Owner never restarts
by hand. Do not report the fix done until the restart succeeds.

**BLESSED INVOKE** (survive agent/overseer death — 🎯T191):

```bash
nohup scripts/restart-daily-jevonsd.sh >>"$HOME/.jevons/restart-daily.log" 2>&1 &
```

Never run `scripts/restart-daily-jevonsd.sh` as a foreground child of a
fleet agent without detach. The script: `make` → `brew services stop jevons`
→ kill `:13705` → `nohup`/`setsid` start `$REPO/bin/jevonsd` with workdir →
wait `/health` + `/api/frontier` non-404 → exit 0 only when serving.
Pure static web-only changes may hard-reload only. Residual: session drop
until T40/T171.

## Achieve reports need activated daily path (🎯T194)

Daemon/API product is **not achieved on hermetics alone**. When the
product path is served by daily `jevonsd` (HTTP API, compiled server,
non-static):

1. Detached `scripts/restart-daily-jevonsd.sh` must succeed (or proven
   zero-downtime upgrade), **and**
2. A **live probe** of the product path must be green (e.g. `curl`
   non-404 / expected body on the daily port).

**Hermetic unit green is necessary not sufficient.** Do not retire or
claim fixed while a stale binary may still serve. Finish reports must
cite **daily-path evidence** (restart-daily success and/or live probe),
not only `go test` / hermetic greps. Pure static web may hard-reload only
(🎯T188). Pure helper: `HasDailyPathEvidence` (`internal/mcpserver`).

**Residual:** instructional + pure classifier; not a hard daemon block of
bullseye achieve.

## Configuration

| Path | Purpose |
|---|---|
| `~/.jevons/` | Data directory |
| `~/.jevons/threads.json` | Durable thread registry |
| `~/.jevons/usage.db` | Token-spend accounting |
| `~/.jevons/budget.json` | Spend budgets / thresholds (optional). `disabled` opt-out; `accounting` = `list_price` (default, billable $) or `subscription` (SuperGrok: API-eq $ never enforces — 🎯T137) |
| `~/.jevons/agents.json` | Agent registry |
| `~/.grok/sessions/` | Grok session store |
| `~/.jevons/jevons/AGENTS.md` | Generated overseer instructions |

## Agent provider (🎯T148)

**Default** (daemon-wide), in order:

1. `provider:` in `~/.jevons/config.yaml`
2. env `JEVONS_PROVIDER`
3. flag `--provider` (overrides file when set)
4. `grok` (back-compat)

**Ad hoc** (per spawn — overseer/PO):

```text
jevons_agent_start(name=…, workdir=…, provider="claude", model=…?)
jevons_thread_spawn(id=…, workdir=…, provider="claude", model=…?)
jwork(text=…, provider="claude", model=…?)
```

Empty `provider` on resume keeps the **registry-stored** backend (not
clobbered to Grok). New agents without an override use the daemon default.
Provider strings pass through to claudia (no allow-list) so future ids
(e.g. Bedrock) are not blocked at the Jevons selection surface.

## Gotchas

- Default remains Grok for empty config/env; set `provider` / `JEVONS_PROVIDER`
  or pass `provider=` on spawn to use another backend.
- Cost events from Grok may not carry Claude-style `costUSD` yet; the
  collector still tails session files for activity; pricing tables will
  improve as Grok usage telemetry is understood.
- Do not diagnose MCP readiness with bare `curl` — use `lsof` + a tool call.
