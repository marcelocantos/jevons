# Grok CLI capability audit → conversational embodiment (🎯T96)

**Status:** research note for product decisions (2026-08-02).  
**Constraint:** voice-first. Jevons acts from ordinary conversation and ambient
mission — **not** slash-command parity or a growing command bar.  
**Sources:** Grok Build user-guide (`~/.grok/docs/user-guide/`, esp. slash
commands, subagents, sessions, plan mode, background tasks, dashboard),
in-session tool surface (goals/loops/workflows/monitor/spawn), and current
Jevons surfaces (persona, fleet MCP, chat chrome, bullseye).

## Thesis

Grok CLI is a power-user **harness** for one interactive agent (and its
children). Jevons is a **CEO/butler** over a durable fleet, with the owner
mostly talking (or dictating) to one overseer. The right transfer is
**behavioural absorption**, not UI cloning:

| Grok shape | Jevons default |
|------------|----------------|
| Slash / palette literacy | Ordinary talk + rare speakable prefixes (`target:`, `aside:`) |
| In-session subagents | Fleet agents (`jevons_agent_start` / durable threads) — 🎯T78 |
| `/goal` evidence loop | Bullseye acceptance + oracle-first (🎯T31) + overseer judgment |
| `/loop` / scheduler | Ambient supervision + event push (🎯T34/T89/T90), not owner-facing crons |
| Agent dashboard | RHS fleet tree + chat progress chrome (🎯T68/T72/T81/T89) |
| Skill slash zoo | Skills run *inside* Grok sessions; owner never types `/commit` at Jevons |

**Anti-goal:** a Jevons command menu that mirrors Grok’s `/` surface.

## Classification key

| Class | Meaning |
|-------|---------|
| **Absorb** | Overseer / harness does this from ordinary owner talk or ambient mission — no ceremony. |
| **Light ceremony** | Speakable, minimal prefix/intent (same family as `target:` / `aside:`). |
| **Leave** | Stays in the Grok harness for the agent that is already a Grok session; Jevons does not re-expose it to the owner. |
| **Reject / defer** | Skip for owner-facing product, or park until a named dependency (reason required). |

---

## Inventory (material capabilities)

One line each: what it does for the **Grok user**, then Jevons class + note.

### Goals, loops, workflows (orchestration)

| Capability | What it does (Grok) | Jevons class | Embodiment |
|------------|---------------------|--------------|------------|
| **`/goal`** | Autonomous multi-round objective; completion only after independent evidence review; pause/resume/clear/status; optional token budget. | **Absorb** | Owner states outcomes in prose; overseer treats active bullseye + mission as the goal graph. “Done” = acceptance/oracle evidence (T31/T104), not self-attestation. Do **not** ship `/goal` in chat. |
| **`/loop` / `scheduler_*`** | Recurring interval prompt (min 60s, auto-expire 7d); tool API for create/list/delete; optional durable. | **Absorb** (product) / **Leave** (in-worker) | Owner: “keep an eye on CI / that long job” → overseer watches via fleet signals + `jevons_event_push` / active-work, not owner-managed crons. Workers may use harness scheduler *inside* a Grok session when useful. |
| **`monitor` tool** | Stream filtered stdout lines into the conversation as wake events. | **Leave** (+ absorb *effect*) | Leave stream plumbing to Grok. Absorb the *outcome*: progress/anomaly chips and overseer wake (T71/T89/T90). |
| **`/workflow` + Rhai workflows** | Named multi-agent pipelines (parallel panels, agent budget, pause/resume/stop); project/user `.rhai` scripts. | **Leave** / **Reject** (owner slash) | Multi-slice work is **fleet fan-out** (T111.4), not owner-launched Rhai. Optional later: PO-internal workflow scripts — still not owner slash soup. |
| **`/workflows` dashboard** | Live run roster (phase, agents, progress). | **Reject** (owner UI clone) | RHS fleet + activity strip + active-work is the product surface (T72/T81). |
| **`/deep-research`** | Background research workflow with claim verification and partial reports. | **Absorb** | Owner: “research X and only keep solid claims” → overseer/PO runs research agents; present verified summary in chat. |

### Subagents, sessions, multi-agent surfaces

| Capability | What it does (Grok) | Jevons class | Embodiment |
|------------|---------------------|--------------|------------|
| **`spawn_subagent`** | Child session with own context; explore/plan/general types; optional worktree isolation. | **Reject** as ambient path | **T78 doctrine:** children are Jevons fleet agents so they appear in the RHS, survive parent interrupt, and report via Deliver. Hard suppress optional; convention + tools already standing. |
| **Agent types / personas (Grok)** | Session definitions and subagent behavioural overlays. | **Leave** | Map to Jevons fleet roles (overseer, PO, worker, aside purpose) + `persona.md` / standing briefs — not Grok persona modal in owner chat. |
| **Agent Dashboard (`/dashboard`)** | Live top-level sessions: peek, reply, dispatch, pin, stop. | **Absorb** (fleet panel) | RHS tree + send-by-name + interrupt/queue (T68/T72/T111.1/T114). No second dashboard chrome. |
| **Session resume / load** | Persist and reload conversations by id/title; daemon attach. | **Leave** (+ harness absorb) | Claudia ACP session/load; overseer resume on daemon restart (T58). Owner never picks session IDs. |
| **`/new` / `/clear`** | Fresh conversation. | **Defer / rare absorb** | Self-heal (T94) mints sessions when rotten; owner-facing “start over” only if productized later as talk (“forget this digression”) — not a slash. |
| **`/fork`** | Branch session history into a new agent. | **Leave** / **map** | Fleet spawn with lineage (T111.3), not history-fork UX. |
| **`/compact` + auto-compact** | Compress context; optional keep-note. | **Leave** | Harness concern per agent session; overseer should not dump compaction UI on owner. |
| **`/rewind` / undo** | Roll conversation and file snapshots back. | **Light ceremony or absorb** | Owner: “undo that” / rewind last turn → `jevons_transcript_rewind` (T52). Prefer prose over a command chip zoo. |
| **`/session-info` / `/context` / `/usage`** | Status, token breakdown, billing. | **Absorb** (selective) | Cost ticker / overseer relay when relevant (T36/T117); never force context pie charts on the owner. |

### Skills, MCP, plan, permissions

| Capability | What it does (Grok) | Jevons class | Embodiment |
|------------|---------------------|--------------|------------|
| **Skills (`/skill`, user-invocable)** | Packaged workflows as slash commands (`/commit`, `/cv`, …). | **Leave** (worker) / **Absorb** (intent) | Workers invoke skills inside Grok. Owner says “commit this” / “what’s next” → overseer routes; no skill menu in owner chat. |
| **MCP servers (`/mcps`)** | Attach external tools; live enable/disable. | **Leave** + **Absorb control** | Grok owns client attach. Overseer reconnect via `jevons_mcp_reconnect` (T60) from conversation when tools die. |
| **Plan mode (`/plan`, enter/exit tools)** | Read-only design phase; approve before code. | **Absorb** | Ambiguous work → overseer/PO proposes plan in chat (or files a design note); owner approves in prose. No mode-toggle slash for owner. |
| **Permission modes (`/always-approve`, `/auto`, sandbox)** | Ask vs classifier vs always-approve; OS sandbox. | **Leave** (config) | Fleet defaults for unattended work (T97); not owner session mode cycling in web chat. |
| **Hooks / plugins / marketplace** | Lifecycle scripts and extension packs. | **Leave** / **Reject** (owner) | Power-user Grok install surface; Jevons product does not rehost the marketplace. |

### Status, cancel, correct, high-use interaction

| Capability | What it does (Grok) | Jevons class | Embodiment |
|------------|---------------------|--------------|------------|
| **Still-running status line** | Counts bg commands, monitors, loops, subagents while idle-looking. | **Absorb** | In-flight progress chrome (T71/T89); RHS brief status (T118); never silent “working…”. |
| **Tasks pane / prompt queue** | List bg work; queue follow-ups. | **Absorb** | Owner chat queue (T113); fleet busy queue + interrupt (T111.1); activity strip (T63). |
| **Cancel / kill / interrupt** | Stop turn, kill bg task/subagent, Ctrl+C class behaviour. | **Absorb** | Prose “stop that worker”, `interrupt=true`, stop/kill tools, cross-tree kill (T100). |
| **Correct / redirect mid-flight** | New message interrupts wait; queued follow-ups. | **Absorb** | Owner impatience (T87): queue + interrupt, not “wait for me to learn a chord”. |
| **`/btw` aside** | Side question without hijacking main turn. | **Light ceremony (done)** | Attention threads: `aside:`, `capture:`, `main:`, `pursue:` (T65); asides as purpose=aside agents (T114/T115). |
| **`/history` / up-arrow recall** | Prompt history search and redo. | **Absorb** (partially done) | History nav / redo intent (T88); keep shell-like feel without Grok-only chords. |
| **File refs, paste images, export/copy** | Attach context; export transcript. | **Leave** / **Absorb** | Image paste (T76); export only if owner asks — low priority vs fleet control. |
| **`/model`, `/effort`, theme, vim, minimal UI** | Session aesthetics and model switch. | **Leave** / **Reject** | Operator config of Grok installs; not CEO conversation surface. |
| **Todo list tool (`todo_write`)** | In-session structured task list. | **Leave** | Per-agent scratch. Product intent ledger is **bullseye**, not ephemeral todos. |
| **Memory (`/remember`, flush, dream)** | Cross-session knowledge. | **Leave** + **Absorb** | Grok memory + mnemo for history; overseer files durable intent as bullseye targets, not “hope memory stuck”. |
| **Worktree isolation** | Subagent edits in isolated git worktree. | **Leave** | Optional for single Grok workers; fleet default is durable shared workdir policy per agent (T86 isolation of *sessions*, not always worktrees). |

### High-use owner intents (not Grok commands — mapping check)

| Owner says (voice/type) | Grok analogue | Jevons surface |
|-------------------------|---------------|----------------|
| “Get X done” / “work on T96” | `/goal` | Bullseye + fleet spawn + report SHA/oracle |
| “Keep watching CI / that job” | `/loop` + monitor | Active supervision + event push; reopen depth under T90 |
| “Spin up helpers for these slices” | subagents / workflow parallel | `jevons_agent_start` + parent lineage (T78/T111.4) |
| “Where are we?” | dashboard + still-running | `jevons_active_work`, RHS tree, progress strip |
| “Stop / nudge / take over” | kill, interrupt, attach | agent_send interrupt, stop/kill, direct/takeover |
| “File this as a target” | (none — skill/manual) | `target:` aside (T95) + `jevons_target_file` |
| “Undo that reply” | `/rewind` | transcript rewind |
| “Tools are dead” | `/mcps` refresh | `jevons_mcp_reconnect` |
| “Ship it” | `/push`-class skill | **Ship plane only when owner opens it** (T104) |

---

## Voice-first recommendations (not slash soup)

1. **Default verb is conversation.** If the owner would say it to a human CEO, Jevons should do it without a special token — spawn, watch, report, stop, replan.
2. **Ceremony budget is tiny.** Only short, speakable prefixes that beat ambiguity under dictation:
   - Existing: `aside:`, `capture:`, `park:`, `main:`, `pursue:`, `target:`.
   - Prefer **not** adding `loop:`, `goal:`, `workflow:` unless a real speech failure appears. Prefer “watch the deploy every few minutes” as free text.
3. **Absorb progress and supervision.** Impatience (T87) fails if the owner must open a tasks pane. Progress and still-running counts belong in chat chrome + RHS, not in a Grok-clone status line the owner never sees.
4. **Fleet is the multi-agent product.** Anything that would be a Grok subagent/dashboard/workflow run for multi-hour coding work should land as named fleet agents with lineage and Deliver — even when the *implementation* inside a worker still uses Grok tools.
5. **Evidence over vibes for “done”.** Steal `/goal`’s *idea* (independent verification), implement via bullseye acceptance + journeys/oracles (T31/T101/T104), not a parallel goal subsystem.
6. **Skills stay behind the curtain.** Owner literacy is product English (“commit”, “review”, “what’s next”), not `/skill` names.

---

## Concrete Jevons surfaces (where absorption lands)

| Surface | Role in embodiment |
|---------|-------------------|
| **`internal/config/persona.md` + fleet standing brief** | Doctrine: no spawn_subagent ambient, local≠PR done, multi-slice fan-out, impatience, relay worker results. Primary absorb path for CEO behaviour (feeds T98). |
| **Harness (claudia ACP)** | Session durability, permissions auto-approve (T97), interrupt/queue, process health (T85), isolation (T86). Leave raw Grok tools here. |
| **Fleet MCP** | `jevons_agent_*`, `jevons_thread_*`, `jevons_event_push`, `jevons_active_work`, `jevons_target_file`, `jevons_mcp_reconnect`, cost — the control plane that *is* Grok’s dashboard+loop+spawn for the owner. |
| **Bullseye** | Goal graph and acceptance; ambient RSI filing (T92); light `target:` ceremony (T95). Not Grok `/goal` clone. |
| **Chat chrome** | Attention prefixes (T65), queue (T113), progress (T71/T89), activity strip (T63), RHS tree/status (T68/T72/T115/T118), self-heal (T94). Owner-visible still-running. |
| **Journeys / hermetic oracles** | Prove absorb paths (fleet spawn, reconnect, fan-out, progress) without requiring owner slash literacy (T101/T105). |

---

## Gaps that matter for impatience & multi-agent

Called out for product follow-through; prefer **link existing targets** over a new zoo.

| Gap | Why it hurts | Status / link |
|-----|--------------|---------------|
| **Active long-work supervision** | Grok `/loop`+monitor wakes the *session*; Jevons must wake the *overseer* on stall/anomaly without owner ping. | **T90** set aside with partial cover from T85/T89/T94 — reopen when passive progress is not enough. |
| **Still-running multi-agent counts in owner UI** | Grok’s `◎ N monitors · M loops` is excellent impatience UX; Jevons RHS/progress must not regress to empty “working…”. | **T71/T89** done baseline; **T118** brief automated status on fleet rows. |
| **Evidence-gated completion culture** | `/goal` refuses self-attested done; fleet workers still risk “done” prose without oracles. | **T31** oracle-first; standing brief + journey-or-exception (T101/T107). |
| **Orchestration depth** | Rhai workflows encode multi-phase verification; fleet fan-out is spawn+direct, weaker for staged research panels. | **Leave** complex panels to Grok *inside* a worker/PO when needed; product path remains fleet. No owner `/workflow`. Revisit only if PO missions systematically fail without scripted stages. |
| **Speakable watch intent without T90** | Owner may say “ping me when green” and get a one-shot promise instead of a standing watch. | Absorb via event_push + persona; if speech fails in week-of-use, consider one light prefix (e.g. `watch:`) under T90 — **do not pre-ship**. |
| **CEO doctrine completeness** | Absorb list is behaviour; full alter-ego policy is broader than CLI mapping. | **T98** (consumes this note). |

**No new bullseye targets filed by this audit.** Reopen/drive **T90** and write **T98** against this map; T118 closes a concrete chrome gap already on the frontier.

---

## Explicit non-goals

- Owner-facing slash command palette or “Jevons Build” command parity.
- Replacing Grok’s internal tools for workers (they remain Grok agents).
- Cloning Agent Dashboard, workflows run UI, or tasks pane pixel-for-pixel.
- Teaching the owner Grok skill names.
- Ambient `spawn_subagent` as a “faster” child path (forbids T78).

---

## Decision summary (for T98 / implementers)

| Absorb aggressively | Light ceremony only | Leave to Grok harness | Reject / defer |
|---------------------|---------------------|----------------------|----------------|
| Goal-seeking from talk + bullseye | Attention + `target:` (existing) | Skills, plan tools, monitor, scheduler *in* workers | Owner `/loop` `/goal` `/workflow` zoo |
| Fleet spawn/direct/watch/report | Transcript rewind as prose | Session compact, model, theme, hooks | Subagent-as-default children |
| Progress + queue + interrupt | Future `watch:` **only if** speech fails | MCP client attach (except reconnect tool) | Marketplace / plugins UI |
| Evidence-based done | — | Worktree isolation option | Silent waits |

**North star:** the owner talks to a CEO who already knows how to use Grok’s power tools through the fleet — they never need to learn Grok’s remote control to get work done.
