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
5. **Optional device pair** — self-host a [pigeon](https://github.com/marcelocantos/pigeon)
   relay (mint your own `PIGEON_TOKEN` / `TERN_TOKEN`; do not message the
   author), then `jevonsd --pair <id> --relay <your-url> --relay-token …`
   + Jevon iOS QR scan (source under `ios/`; no App Store binary yet; full
   onboarding is 🎯T14). See README [Pair a device](README.md#pair-a-device)
   (🎯T156).
6. **MCP attach**: on boot, jevonsd calls Claudia `EnsureMCP` so
   `jevonsmcp` is on Claude, Grok, and Codex native configs (the
   served URL, 🎯T379). Fleet mints also get `LoadMCP` + append on
   `AgentDef.MCPServers`. Do not hand-roll `claude mcp add` /
   `grok mcp add` for fleet seats. An external client talking *to*
   jevons can still add the same URL if it is not a claudia Session.
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
- **Fence hygiene:** 🎯T145 `ensureFenceNewlines` and 🎯T147
  `coalesceAssistantText` keep smushed `prose.```lang` from breaking fences.
- **Never** use plain `textContent` of markdown source as the live stream
  default.

## MCP tools

- **Threads**: `jevons_thread_adopt`, `_list`, `_status`, `_spawn`,
  `_direct`, `_takeover`, `_remove` — durable threads over provider
  sessions (default Grok; other providers via spawn/`provider`).
- **Cost**: `jevons_cost` — burn-rate snapshot (multi-harness when
  configured; Grok session tails remain the historical path).
- **Plan remaining**: `jevons_plan_usage` — the header ticker (per-provider
  session/weekly remaining, 429 as exhausted 0%). Distinct from burn
  (`jevons_cost`) and admission (`jevons_capacity_status`). Use this to
  decide where to put the next job.
- **Workers**: `jwork` (sole ephemeral primitive — one self-contained
  task, runs to completion) and `jevons_agent_*` (named durable agents).
  The legacy `jevons_*_session` tools were removed (🎯T41).
- **MCP resilience (🎯T60)**: `jevons_mcp_reconnect` — from inside the
  overseer chat, re-attach dropped MCP servers (all, or one named
  server) without session rotate or TUI `/mcps`. Cycles
  `grok mcp disable` → `enable` so tools from servers like github/gmail
  work again in the same session.

## No jevons_* tools? Diagnose before you report an outage (🎯T464)

Missing tools look identical from the inside whether the daemon died or
the registration does not reach your working directory. On 2026-08-15 a
product owner restarted outside the jevons repo, found no `jevons_*`
tools, and reported that **the control plane was dead**. It was not:
jevonsd was answering on 127.0.0.1:13705 the whole time, and the fleet
spent a cycle chasing an outage that was not happening.

**The absence of tools never licenses the word "down". Only a probe of the
endpoint does.** You cannot ask an MCP tool why your MCP tools are gone,
so the answer arrives through Bash:

```
bin/mcpscope diagnose        # exit 0 healthy, 3 out of scope (daemon UP), 4 down, 5 undetermined
bin/mcpscope ensure          # register jevonsmcp user-scoped, so it follows the agent
```

`out_of_scope` means **the daemon is up** and your directory has no
registration: fix the registration, do not restart the daemon, and do not
report an outage. The daily daemon now writes that user-scope entry at
boot, so this should be self-healing after a restart; a session that
started before the repair still needs to be restarted to pick it up.

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

### One deliver-by-name path, overseer included (🎯T309.3)

Every message to an agent — `jevons_agent_send`, `POST /api/agents/{name}/send`,
and the daemon's own worker-reply / worker-idle / daemon-restarted
notifications — runs the **same** implementation, addressed by agent name.
**The overseer is just another addressable agent.** It no longer has a
privileged talk wire of its own, so a PO or worker reporting up by name lands
in the owner chat journal with queue-on-busy retry behind it, exactly like any
other delivery.

What this means when you are briefing or reporting:

- **Address by name, not by API.** `jevons_agent_send` with `name="jevons"`
  reaches the overseer; there is no separate overseer tool to hunt for.
- **Name yourself as `actor` (🎯T321).** Pass `actor` = your agent name on
  every `jevons_agent_send` (same idea as `jevons_agent_kill`). Lineage
  authorization runs against that actor; denials log actor + relation. The
  overseer uses the overseer name (usually `jevons`).
- **Hierarchy is lineage, not reachability.** Report up (worker→PO→overseer)
  and direct down (ancestor→descendant) are always allowed; peer messaging
  between siblings is allowed on purpose. What you *cannot* do is speak as the
  **owner** — owner-origin turns paint an owner bubble and only the owner's own
  surface may assert them.
- **No silent drops.** An unregistered peer, an unreachable overseer, and a
  failed delivery are **errors you get back**. A busy peer returns `queued`
  with the message retained (🎯T111.1) — never a discarded send (🎯T61/🎯T62).

`jevons_thread_direct` is **not** a second deliver path: it is the
*synchronous* request/reply op (it waits for the reply and assembles it), which
is why it stays separate from the fire-and-forget family above.

### A send is confirmed, not assumed (🎯T416)

`jevons_agent_send` used to answer **"Message sent"** whenever the keystrokes
left the daemon. That is a statement about the send call. On 2026-08-10 a
multi-KB paste routinely landed in the receiver's composer and stayed there,
unsubmitted, while every sender was told delivery succeeded — including five
overseer↔PO messages, three of which are in no transcript on disk.

The send path now reports what it **observed of the agent**, in four answers:

| Status | What it means | What to do |
|---|---|---|
| `sent` | The payload appeared in the receiver's transcript as a user message. It became a turn. | Nothing. |
| `queued` | A turn was already running. The daemon holds the message itself and delivers it on the next turn boundary — it is **not** pasted into a composer that could merge or destroy it. | Nothing. |
| `delivered_unconfirmed` | Handed over, not seen to land, and the daemon does not know whether a turn was already running (that record does not survive a restart). | Treat as **undelivered** until the agent acts. |
| error: *not submitted* | The agent was known idle and the payload never became a turn. It is sitting in that agent's composer. | Do not re-send — that stacks a second copy. |

**Never** read a `not submitted` error as a provider refusal, a spend limit, or
an agent with nothing to say. Confusing those cost thirteen hours of
misdiagnosis on a worker that was neither broken nor billed out.

#### Checking a delivery by hand — the two instruments that work

Three instruments were consulted on 2026-08-10 and **all three passed while
being wrong**. Do not reach for them:

- ❌ **Transcript growth.** A send's Enter submits whatever the composer already
  held, so the payload that lands is never the one you just sent. Growth
  confirms *somebody else's* message.
- ❌ **A raw grep of the session file.** Agents capture their own panes into
  their transcripts, so an unsubmitted payload appears in the file inside a
  `tool_result`. That is how a real loss was retracted as a phantom.
- ❌ **The receiver's behaviour.** An ack, or the agent doing what it was told,
  proves only that it **saw** the text — and an agent can read its own
  unsubmitted composer. It inverts: an ack to a message that never became a
  turn is evidence the message was lost.

Three instruments are sound, and all three were sitting unused:

- ✅ **Payload-match at user-message level.** Read the receiving session's JSONL
  and look for your payload in a record whose `message.role` is `user`, over
  authored content only (a plain string, or `text` blocks) — never
  `tool_result` blocks. Sound at the instant of reading (🎯T417 bounds it after).
- ✅ **The receiver's own queue records.** In the same JSONL:
  `{"type":"queue-operation","operation":"enqueue|dequeue|remove|popAll","content":…}`
  and `{"type":"attachment","attachment":{"type":"queued_command","prompt":…}}`.
  **enqueue** carrying your payload is a *positive* reading of the queued state;
  a **remove/dequeue** or a **queued_command** attachment means it entered the
  turn. It survives daemon restarts, because it is written by the receiver.
- ✅ **Transcript-file absence.** A session's JSONL is created by its **first
  submit**, so a registry-named session with **no file at all** has never begun
  a turn. Cheap, needs no tmux, and it is a *positive* born-stuck test rather
  than a failure to observe. Use it **with** payload-match, never instead:
  file-exists says nothing about which message landed.

⚠️ **ABSENT AT USER-MESSAGE LEVEL IS NOT UNDELIVERED — READ THE QUEUE RECORDS
FIRST.** A message the receiver accepts behind a live turn is replayed into that
turn as a `queued_command` **attachment** and *never* becomes a user message. So
payload-match alone reports absent for a message already being worked on. It did,
live and twice, and hand-flushing on that reading would have delivered a second
copy of a message that had already landed.

Together they separate the failure shapes: **file absent** ⇒ born-stuck, whole
backlog unsubmitted; **queue record present** ⇒ delivered (queued, or already in
the turn); **file present, no payload and no queue record** ⇒ either a mid-turn
read (🎯T417) or genuinely lost.

The handover dispatcher (`fleet.SeedSuccessor`) is a fourth caller of this path,
and it now uses the same turn-begin evidence as the other three. It used to fail
*closed* on a different predicate — whether the **reply completed** inside ten
minutes — which looks right on a born-stuck agent (a turn that never begins
never completes) and is wrong on a slow one: it condemned a seed at 09:07:04Z
that landed at 09:07:10Z. Its fail-closed line,
`handover hand-off failed; it stays pending for the next launch`, is still the
one to read, and now says which transcript finding produced it. Its recovery —
a seed parked until a launch that may never come — is 🎯T418.

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

Digit-squash makes `🎯T27.2` vs `🎯T272` (or `🎯T47.1` vs `🎯T471`) ambiguous in
the RHS fleet list. Residual: flat ids (no sub-target segment) stay as
today (`jv-t159-seal`). Optional suffix (`-config`, `-docs`) is free-form.

### Multi-slice fan-out (🎯T111.4)

PO/boss agents on **multi-slice** missions must spawn `jevons_agent_start`
children (with `actor`/`parent` lineage) rather than unbounded solo
exploration. Single-agent tasks remain fine. Zero children after planning
on a multi-slice brief is a failure mode (`jevons_agent_list` fan-out
check). Prefer agents over threads for named long-lived workers.

### Frontier = ready set (🎯T262.1)

**Frontier = ready set.** Every unblocked leaf is legitimate work. There is
no privileged "next ticket." A queue is frontier size ≤1 with invented
order. Multi-agent default: one work agent per ready leaf, subject to
engagement policy (capacity, ownership, design/park filters, churn).
Bullseye records intent and computes readiness; Jevons engages implementers.
Neither product answers "the next ticket" as a total order.

- **Anti-pattern:** framing bullseye (or `/cv` alone) as answering "what is
  the next ticket?"
- **Queue is special case:** capacity mutex, hard product dependency not yet
  in `depends_on`, or owner ritual — not the default. Pick among ready
  leaves is **indifferent or policy**, not discovery of a true head.
- **Related:** 🎯T155 / 🎯T193 consume the set; 🎯T198 / 🎯T222 engagement.
  Design: `docs/design/frontier-as-ready-set.md`.
- **Residual:** instructional doctrine + brief inject. Does **not** unpark
  🎯T254 or claim 🎯T262.4 owner accept.

### Unattended frontier auto-spawn (🎯T155)

When a **new frontier leaf** is filed that is **not** design-gated /
needs-owner / design-discussion / parked-for-design, **`jevons-po` spawns a
fleet worker** under **`parent=jevons-po`** in the **same operational cycle**
— do not wait for the owner to request a frontier review.

- **Standing rule:** kick off all non-design frontier work **continuously**;
  new unattended leaves get a worker **immediately**.
- Overseer routes to PO (🎯T129); PO spawns, workers execute (🎯T125).
- **Skip:** design-gated (🎯T112 / 🎯T67 / 🎯T29-class) and blocked targets stay
  unspawned until unblocked or owner opens design. **Host saturation
  (🎯T460)** is also a blocking condition: do not spawn when
  `jevons_capacity_status` reports pressure critical (or
  `jevons_agent_start` refuses with `host_saturated`). "Frontier is not
  empty" does not mean keep spawning on a host that cannot run what is
  already spawned.
- **Related:** 🎯T193 file→spawn same turn (owner-filed and mid-session Build).
- **Residual:** instructional; no daemon auto-spawn unless later enforced.

### File→spawn same turn (🎯T193)

**🎯T130** files the target; **🎯T193** spawns the worker. Do **not** leave
Build filings **ledger-only**.

When a **Build-plane** target is filed — owner via `target:` aside /
`jevons_target_file`, or mid-session by overseer/PO — **`jevons-po` spawns a
named worker** under **`parent=jevons-po`** in the **same turn** as filing
unless the target is design-gated or parked.

- **Same turn:** `jevons_agent_start` (or route to PO) before the turn ends.
- Overseer routes to PO (🎯T129); PO spawns, workers execute (🎯T125).
- **Skip (file without spawn):** design-gated (e.g. OAuth app pins, 🎯T112 /
  🎯T67 / 🎯T29-class), blocked-on-human / needs-owner / parked-for-design,
  pure documentation / docs-only, and host saturation (🎯T460).
- **Related:** 🎯T155 continuous unattended frontier kick-off.
- **Residual:** instructional; no daemon auto-spawn unless later enforced.

### PO proactive-until-empty-then-sleep (🎯T325.1)

Product owners run **proactive-until-empty-then-sleep**: keep kicking Build
while the product frontier has ready leaves; sleep/idle without open-mission
thrash when empty; stay interruptible.

- **Kick while ready:** unblocked ready leaves (not design-gated /
  needs-owner / design-discussion / parked-for-design / blocked /
  already-engaged / host-saturated 🎯T460) ⇒ continue spawn/brief
  until empty or blocked — not a one-shot pass that strands work.
  Complements 🎯T155. Host saturation is blocked: wait it out.
- **Sleep when empty:** empty frontier, or only gated/blocked/parked/
  already-engaged leaves ⇒ sleep/idle without perpetual create thrash or
  zombie open-mission re-spawn noise (compose 🎯T244).
- **Interruptible:** PO remains registered for owner/overseer directs while
  sleeping or mid-pass.
- **Pure helpers:** `ClassifyPOProactive` / `ClassifyFrontierLeaf` /
  `POOpenMissionForProactive`. Design:
  `docs/design/life-and-work-org-map.md` §8 child (1).
- **Residual:** instructional doctrine + pure classifier; hard daemon sleep
  gate may follow.

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
| **`jevons-po` (sole spawn parent)** | Yes — bosses/workers under 🎯T125 |

**Exception:** PO dead/unregistered → rehydrate or start PO first, then
PO spawns. **Residual:** instructional until a later target adds registry
enforcement (reject wrong parent).

### Domain portfolios default (🎯T200)

RHS fleet tree groups product owners under named **portfolios** via
declarative path membership in `~/.jevons/config.yaml` — **not**
agent-name parsing. Portfolio nodes sit under the root overseer
(`jevons`); POs with matching workdirs nest under their portfolio;
unassigned POs hang directly under the overseer root.

**Default for marcelocantos POs:** workdirs under
`github.com/marcelocantos/…` belong in the **personal** portfolio.
Live config uses the org path fragment so one member entry covers the
whole org:

```yaml
portfolios:
  - id: personal
    name: Personal
    members:
      - github.com/marcelocantos
  - id: minicades
    name: Minicades
    members:
      - github.com/squz/yourworld2   # example non-default assignment
```

| When spawning… | Nest under |
|---|---|
| New PO in `github.com/marcelocantos/…` | **Personal** (default — ensure config path match) |
| Owner assigns another domain (e.g. squz / minicades) | That portfolio’s members list |
| No matching `members` path | Overseer root (unassigned) — **avoid** for marcelocantos POs |

**Standing rule:** when spawning a new marcelocantos PO, they nest under
Personal — do **not** leave them unassigned under the overseer root
unless the owner assigns a different portfolio. Membership is config path
match only; no GM agent required (🎯T201 set aside). Residual:
instructional spawn hygiene; display reparent is config/registry, not
kill lineage.

### Idea capture (🎯T325.3) — durable intake, not scrollback

Owner sparks via `idea:`, `capture:`, aside, or mid-chat must land in a
**listable** destination within one ceremony:

| Path | Tool / surface |
|------|----------------|
| Capture spark | `jevons_idea_capture` or owner `idea:` / dual-write `capture:` |
| List inbox | `jevons_idea_list` / `GET /api/ideas` |
| Triage | `jevons_idea_triage`: **file** → then `jevons_target_file` (+ 🎯T193 if Build); **park** needs-owner/design; **hold** life-domain parked; **drop** rare |

Do not leave product-shaped sparks as main-chat-only prose. Ceremony:
`docs/design/idea-capture.md`. Residual: opportunity-cost optimiser parked.

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

**Retrospective coach mine (🎯T353):** the coach does not only wait for new
appends. On a slow cadence (default 6h, 7d window) it makes a **bounded pass
over history** — git repair churn and reverts, the eventlog tail, owner chat,
session transcripts — and posts sparse judgments marked `Mode: retrospective`
with commit SHAs / session ids as evidence. *Fine sensors, coarse conclusions:*
extraction stays sensitive, but a value bar drops one-off git noise and bare
phrase-friction before anything reaches the overseer. On demand:
`jevons_rsi_coach_cycle mode=retro|both`; dials (`retro_lookback_hours`,
`retro_interval_sec`, `retro_rate_cap`, `retro_min_count`, `retro_workdir`) via
`jevons_rsi_coach_configure`; last pass visible in `jevons_rsi_coach_status`.
Retro never advances the drip cursor and never calls bullseye.

**Capacity-aware background (🎯T359):** ambient cycles (research, audits,
coach, sentinel) are **admitted, degraded, or deferred** against one holistic
read of remaining budget and concurrent load — not each loop's own soft cap.
Priority is explicit: **owner turns and open Build missions outrank all ambient
background**; among background, control-plane repair is load-bearing and stands
down last. Pressure ladder: *elevated* → ambient runs a reduced pass; *tight* →
load-bearing only; *critical* → owner and Build work only, plus **one sticky
owner notice** (not one per tick). Composes 🎯T36 (the clamp stays the safety
net, capacity acts before it), 🎯T137 (under subscription accounting USD is an
estimate and never denies work — tokens and load do), and 🎯T325.2 provider
soft caps. Ask `jevons_capacity_status` — or `GET /api/capacity` — when a
background cycle looks quiet: it reports pressure, headroom by dimension, and
what each class would be granted right now. Dials live in
`~/.jevons/capacity.json` (`daily_token_budget` is **unset by default**: an
unknown ceiling is reported as unknown, never invented). Design:
`docs/design/capacity-aware-background.md`.

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

## Cited SHA must stay reachable (🎯T427)

A commit SHA cited as attestation evidence must still be reachable when
the overseer checks it. Prove it **before** you send the report:

```bash
git merge-base --is-ancestor <sha> HEAD
```

The worker runs that check; the overseer runs it at review. The instruction
and its proof obligation are never separated.

**Amend-vulnerable is not file count.** A tip is amend-vulnerable when it is
unpushed **and** the write touches only `bullseye.yaml` (bullseye's
auto-commit amends that shape). A single-file **code** commit is safe. Do
**not** teach or follow "cite a multi-file commit" — that would flag honest
single-file landings and teach padding (Goodhart).

Do not rest attestation on a yaml-only / ledger-only commit alone; cite a
stable code or docs SHA as well. `bin/gate check` flags finish reports that
cite an unreachable SHA. A standing ledger walk reports rewritten (object
present, not an ancestor) vs missing citations in `bullseye.yaml` —
historical unreachable citations are **reported**, never silently rewritten
in place. Local master only (🎯T104).

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
rich 🎯T29 surface and owner process-fidelity validation remain
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

## Typed fleet envelopes (🎯T509)

Load-bearing agent-to-agent messages (spawn-brief, finish-report,
status-ping, escalation, ack, target-file-request, scout-report) open
at line 1 with a fenced `jevons` block. Schema and enums live in
`internal/envelope` — this file does not restate them. Worker terminal
**product-done** reports **MUST** be a `finish-report` envelope:

```jevons
jevons: kind finish-report
jevons: target T509
jevons: oracle sha=… gate-id=…
jevons: verdict GREEN
jevons: status in-progress
jevons: silent-ledger none
```

Or, when the brief was silent on material choices, a ranked ledger
(least-confident first) — 🎯T536.1:

```jevons
jevons: kind finish-report
jevons: target T536.1
jevons: oracle sha=… gate-id=…
jevons: verdict GREEN
jevons: silent-ledger ranked
jevons: silent-decision confidence=0.2 choice="optimistic concurrency" why="spec silent on locking"
jevons: silent-decision confidence=0.5 choice=shared-sqlite-table why="no isolation guidance"
```

A green oracle with a **missing** silent-ledger (and no explicit `none`)
is flagged, not treated as complete. Quality of the decisions is judgment;
this rule is that the artifact exists and the independent gate can read it
(`envelope.ReadSilentLedger`). Worker/boss doctrine requires the ledger on
terminal reports (role files under `internal/roles/builtin/` when spawned
with a role; otherwise agents-guide + fleet standing brief).

## Auditor role (🎯T536.2)

Spawn a read-only challenger of the silent-decision ledger:

```
jevons_agent_start name=… workdir=… role=auditor parent=jevons-po …
```

Built-in role files live in `internal/roles/` (`builtin/auditor.md` and
siblings). Owner overrides: `~/.jevons/roles/` (or `$JEVONS_ROLES_DIR`).
Override wins; built-ins cannot be deleted. The registry records
`role=` (AgentDef + `state_dir/agent_roles.json`); `jevons_agent_list`
surfaces it. Instruction assembly is universal fleet brief + role body +
mission — auditor doctrine is in the role file, not as if-you-are-X in the
shared brief. Residual: hard daemon git-write block may follow; first slice
is role + doctrine + a different seat from the implementer.


### Fog-of-war scout (🎯T536.3)

Non-trivial Build work is scouted before implement. A scout spawn-brief
carries `phase scout`; the scout terminal is a `scout-report` (not a
product-done `finish-report`) so T165 does not reap a seat that only
mapped territory:

```jevons
jevons: kind scout-report
jevons: target T536.3
jevons: phase scout
jevons: silent-ledger ranked
jevons: silent-decision confidence=0.4 choice="scout-report kind" why="not product-done"
jevons: fog-known "T509 envelopes"
jevons: fog-unknown "auto-spawn always scouts?"
jevons: fog-blindspot "non-trivial threshold"
```

The implementer brief uses `phase implement` and may inherit the scout
ledger (`envelope.InheritLedger`). A non-empty `fog-blindspot` sets
`FogMap.NeedsReslice` — re-slice (another scout pass / narrower leaves)
before implement; do not guess through hidden map. Design-gated /
parked-for-design / T31.2 fuzzy / host saturation (T460) still block
advancing into implementation — scout does not punch through those gates.

English rationale follows the closing fence. Unenveloped messages still
fall back to prose heuristics. YAML front matter (`---`) is not this
format. The cockpit paints a compact header, not a raw dump.

**Finished work agents leave the fleet without hand-pruning (🎯T165 / 🎯T195):**
when a work agent's terminal report claims done — including imperfect bare
done without oracle markers — the product stop+Removes them from the
registry (RHS / `agent_list` omit the name). When a mission target is
achieved on the bullseye ledger, work agents engaged on that TargetID are
also reaped. Residual: POs and overseer stay; multi-target agents without
a matching TargetID stay; deliberate `jevons_agent_stop` without kill
still leaves registration for resume; 🎯T90 deep anomaly supervisor is separate.

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

The script: re-exec into its own session (🎯T405) → `make` →
`brew services stop jevons` → kill `:13705` → `nohup`/`setsid` start
`$REPO/bin/jevonsd` with workdir → wait `/health` + `/api/frontier`
non-404 → exit 0 only when serving. Pure static web-only changes may
hard-reload only. Residual: session drop until 🎯T40/🎯T171.

**Supervision (🎯T405).** On 2026-08-10 a worker's restart killed the
daemon, the daemon's shutdown stopped that worker, and the script died
with it five seconds before the step that starts the replacement — the
fleet stayed down until the owner noticed. Two things now stop that. The
script **re-execs itself through `bin/detach` into its own session**
before doing anything, so being invoked wrongly cannot cause an outage:
the hazard was documented here as an instruction to callers from the
first version, and a correctness property that depends on every caller
remembering a convention is not a property. And the launchd job
**`com.marcelocantos.jevons-watchdog`** probes the port every 30s from
outside every process tree a restart tears down, restarting through this
same script when it stays dead past a grace window, pacing its attempts
and never giving up. `make watchdog-install` / `make watchdog-status`.
A recovered outage reaches the owner twice: out of band while it is
down, and in owner chat once the daemon is back to report it. The
blessed invoke above is still preferred — it stops the caller *blocking*
on the bounce — it is just no longer what makes the bounce survive.

## Run gates so the status survives (🎯T386 / 🎯T396)

A pipeline's exit status is the **last** command's. `go test ./... |
tail -20` therefore reports tail's status, which is always zero — that is
how a suite that died on a timeout panic was reported as green, twice in
one session. Two siblings of the same defect: bash's `PIPESTATUS` does not
exist in the zsh this harness runs (`${PIPESTATUS[0]}` expands to nothing,
and zsh's own `pipestatus` indexes from 1, so `${pipestatus[0]}` is empty
too), and the harness has relayed a background gate as "exit code 0" for a
`go test` that exited 1.

Run every gate through `bin/gate` and cite the line it prints:

```bash
bin/gate -- make test-go
GATE make-test-go exit=0 GREEN id=9f13c0a2 out=6b1d9e4f2a01 dur=42.1s
```

`gate` runs the command as a process — no shell, no pipeline — exits with
the command's own status (so it drops into a Makefile recipe or an `&&`
chain unchanged), and writes a record under `~/.jevons/gates`.

| Rule | Why |
|---|---|
| Never pipe a gate and cite the result | Nothing after the command owns its status |
| `exit=unknown` is not a pass | A status that could not be established never renders as zero |
| Only `GREEN` may be cited as a pass | `SUSPECT` = zero exit over panic/timeout/race/FAIL output |
| Read background gates back with `bin/gate last` | In band, off disk, independent of what the harness claimed |
| A refused invocation is not a gate | An argument `gate` cannot honour is named and rejected, never performed in part (🎯T453) |

`bin/gate check <report-path>` — or `bin/gate check < report` — reads a
finish report and flags a green its own evidence does not support; the
daemon runs the same check on the notify
path and prepends a **FALSE-GREEN banner** in front of the report to the
overseer. A fabricated `GATE` line is caught too — the id is looked up,
and an id with no record behind it says so. Pure helpers:
`gate.FlagFalseGreen` / `gate.Banner` (`internal/gate`).

**Residual:** the banner marks a report, it does not block delivery.

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

## Visual cockpit finish is a prose look, not a green metric (🎯T493.1)

After any change that can affect what the owner sees in `#messages` / the
React transcript pane in **`ui/`** (pin, virtualize, replay, fold, slot
mint, spacing) — the product cockpit (🎯T540) — take a viewport screenshot
and write a short visual verdict **before** claiming done or achieving:

1. What ink is on screen.
2. How much of the pane is empty.
3. Whether Latest is showing.
4. The sentence yes or no to "does this look like a normal chat transcript after a hard reload?"

**A metric that is already green cannot be that verdict.**
`visibleInScroller ≥ 1`, `modelRows = N`, and a screenshot-tool caption
are not answers. One leftover bubble in a tall pane, Latest on a hard
reload, or more empty canvas than bubbles is an **automatic no**.

If the prose says **no** and a journey is green, the journey is a **false
green** — fix the oracle in the same turn; daily is not a universe the
test cannot see.

Pure helpers: `HasVisualProseVerdict` / `LooksLikeMissingVisualVerdict`
(`internal/mcpserver`). **Residual:** instructional + pure classifier;
not a hard daemon block of bullseye achieve.

## Cockpit UI path (🎯T540) — hard doctrine

Product owner-visible UI work lands in **`ui/`** (Vite + React). **`web/`
is deprecated reference-only** — use it to judge parity, not to ship new
behaviour. Daily `:13705` may still serve vanilla until cutover (🎯T505 /
🎯T540.2); that is not licence to edit vanilla for features. Run React with
`make ui-dev` (`:5173`).

## Configuration

| Path | Purpose |
|---|---|
| `~/.jevons/` | Data directory |
| `~/.jevons/config.yaml` | Daemon config incl. `portfolios` (🎯T200 path membership) |
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
clobbered to Grok). New agents without an override use the daemon default
(`config.yaml` `provider`, then `JEVONS_PROVIDER`, then grok). 🎯T476:
a leftover `~/.jevons/llm-portfolio.json` or the compiled T325.2 seed
(which still prefers Claude for `code_implement` / `design_prose`) must
not silently win. The start result cites which knob selected the provider
(`provider_knob: config` | `explicit` | `resume`) and names a disagreeing
file or compiled seed as the loser.
Provider strings pass through to claudia (no allow-list) so future ids
(e.g. Bedrock) are not blocked at the Jevons selection surface.

### Running the whole fleet on Claude (🎯T282)

One setting moves everything — overseer, POs, workers, asides, jwork
tasks — onto Claude:

```yaml
# ~/.jevons/config.yaml
provider: claude
```

(or `JEVONS_PROVIDER=claude`, or `jevonsd --provider claude`). Restart the
daemon; already-registered agents keep the backend stored on their
registry row, so an existing fleet stays on Grok until each agent is
re-created or started with `provider="claude"`.

Evidence: `make test-journey PROVIDER=claude` runs the isolated
Universe-B suite — owner chat, cancel, MCP tool surface, worker spawn,
direct, shell tool, transcript inspect — end to end on Claude.

What changes under Claude:

- **Overseer MCP** is the same `EnsureMCP` write as Grok and Codex
  (claudia 🎯T40). Restart jevonsd to refresh the served URL.
- **Transcripts** are discovered under `~/.claude/projects` (🎯T213);
  `claude_projects:` in config points elsewhere if needed.
- **`jevons_mcp_reconnect` does not apply** — `grok mcp disable/enable`
  is a Grok control plane. With Claude selected the tool says so rather
  than cycling a config the overseer never reads. Re-attach with `/mcp`
  in-session, or restart jevonsd to re-run the user-scoped install.
- **Agents launch as tmux sessions.** jevonsd therefore drops the
  enclosing agent session's identity from its own environment at boot
  and reconciles claudia's long-lived tmux server, which otherwise hands
  each new agent the environment of whatever started it — possibly a test
  run from days earlier. Starting jevonsd from inside a Claude Code
  session is safe because of this; without it, spawned workers rejoin the
  parent session and never submit their turns.

Residual: Claude Session readiness is a pane pattern match owned by
claudia, and Claude Code's startup splash can satisfy it while the TUI is
still mounting. jevons pays a short settle after launch
(`internal/fleet.claudeReadySettle`) to keep the first turn from being
swallowed; the real fix belongs in claudia's readiness detection.

## Gotchas

- Default remains Grok for empty config/env; set `provider` / `JEVONS_PROVIDER`
  or pass `provider=` on spawn to use another backend.
- Cost events from Grok may not carry Claude-style `costUSD` yet; the
  collector still tails session files for activity; pricing tables will
  improve as Grok usage telemetry is understood.
- Do not diagnose MCP readiness with bare `curl` — use `lsof` + a tool call.
