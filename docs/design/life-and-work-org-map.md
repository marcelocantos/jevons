# Life-and-work org map (🎯T325)

**Status:** design map for owner acceptance (do not treat as full product
implementation). **Mission this note closes:** map + first Build children
filed — not the whole org.

**Audience:** owner, overseer, PO/staff implementers.

**Companions (read first):**

| Note / surface | Role |
|----------------|------|
| [ceo-alter-ego.md](ceo-alter-ego.md) (🎯T98) | Identity: Jevons as owner's alter ego in the CEO seat — not chat-idle butler |
| AGENTS.md CEO identity | Repo-facing thin of T98 |
| `internal/config/persona.md` + fleet standing brief | Live load path for CEO/PO/worker behaviour |
| 🎯T155 / 🎯T254 factory | Unattended frontier kick-off; factory parity parent (**T254 parked**) |
| 🎯T219 sentinel | Always-on observe→repair/file loop (staff shape) |
| 🎯T92 / 🎯T243 / 🎯T130 RSI | **First-class org function** (§4): agent RSI (coach → root) + human RSI domain; filing reflex |
| 🎯T93 / T95 asides | Light idea/target capture without polluting main flow |
| [supergrok-cost-accounting.md](supergrok-cost-accounting.md) (🎯T137) | Subscription vs list-price cost honesty |
| [provider-contract.md](provider-contract.md) | Tool providers (feeds/ui/mcp) — orthogonal to LLM harness providers |
| [attention-threads.md](attention-threads.md) (🎯T65) | Main vs aside attention model |
| §12 Leadership methods | Recurring leader practices + Musk Algorithm as process export (not new hierarchy) |

This note is the **org shape**: who senses, who decides, who acts, how
resources, ideas, and **reflection (agent + human)** flow. It does **not**
implement factory, sentinel, or RSI product code. Whole-org Ship is out of
scope.

---

## 1. North star

**Root Jevons is CEO of Marcelo's life-and-work organisation** — an
always-on sense / decide / act loop with a multi-provider resource
portfolio, product owners, ancillary staff, an idea→priority pipeline,
and **first-class recursive self-improvement (RSI) + self-reflection** for
both the agent org and the human owner.

It is **not**:

- a chat wrapper that goes idle between owner turns
- a single coding agent with a nicer UI
- a permanent monologue of staff agents burning tokens without cycles
- an org that only improves product while treating reflection as garnish
  after "real" work
- Gas Town / Beads reimplemented wholesale (factory parity lives under
  parked 🎯T254; escape-hatch evaluation informs this map, does not unpark it)

**Owner test (extends T98):** if the owner watched a silent hour of root
choices across product *and* life-adjacent capacity (ideas, stall recovery,
resource allocation, **agent RSI filings, human reflection capture**), would
they mostly say "that's what I would have done"? When no — doctrine or
product is wrong.

**Root remains interruptible CEO.** Owner speech always pre-empts ambient
cycles. Root does not go dark implementing product (hierarchy T125/T129).
**Continuous improvement of the org itself is core CEO duty** — not optional
post-product garnish (§4).

---

## 2. Roles

```text
                    Owner (ratifies constitution, Ship, taste, credentials;
                           human RSI / life+work self-reflection — first-class domain)
                                      │
                                      ▼
              ┌─────────────────────────────────────────┐
              │  Root CEO  jevons  (overseer / alter ego) │
              │  sense · decide · act · RSI root · interruptible │
              └─────────────────────────────────────────┘
                     │                    │
         ┌───────────┼──────────┐         │ staff (bounded cycles)
         ▼           ▼          ▼         ▼
      Portfolio   Product     Product   Sentinel / ops staff
      (path)      Owner(s)    Owner(s)  RSI coach (judgment only → root)
         │           │          │         │
         │           ▼          ▼         │
         │        Workers    Workers      └→ judgments / brief PO / escalate
         │        (Build)    (Build)         root files · never mint ledger
         ▼
   Domain POs (personal / minicades / …) — product-scoped only
```

| Role | Who | Mandate | Does not |
|------|-----|---------|----------|
| **Owner** | Marcelo | Constitution, Ship plane, taste, MAJOR/PATCH, irreversible risk; **human RSI** subject (habits, attention opportunity cost, education-of-self) the org serves | Run the factory by hand every day |
| **Root CEO** | `jevons` overseer | Always-on org control: allocate attention, spawn staff cycles, route to POs, relay outcomes; **RSI control plane** — receive coach judgments, file/act/ignore (T243), filing reflex (T130); continuous improvement of the org itself | Implement product code; parent product workers (T129); open Ship ambiently (T104); skip RSI until "after product" |
| **Portfolio** | Config path membership (T200) | Group POs under life/work buckets (e.g. personal `github.com/marcelocantos`) | Parse agent names for membership |
| **Product Owner (PO)** | e.g. `jevons-po` | Product-scoped proactive Build: spawn workers, gate achieve, stay interruptible | Solo implement Build (T125); own whole-life SWOT; own org-wide RSI control plane (root does) |
| **Worker** | Named fleet agents | Execute one mission; evidence-gated finish; auto-deregister on done (T165/T195); mid-mission filing reflex when gaps appear | Open Ship; redefine org doctrine mid-flight without filing |
| **Staff — RSI coach** | T243 coach (structural, not garnish) | **Bounded judgment cycle:** drip-read owner chat (priority) + eventlog + sessions; post judgments to overseer only | File / achieve / set_aside bullseye (root decides); permanent monologue; T92 phrase-list mint on product path |
| **Staff (other ancillary)** | Sentinel (T219), future ops cycles; **parked** practical security + system management + **CEO journal** (§8.1) | **Bounded ops cycles** — observe, classify, repair-or-file, snapshot resources; then idle/sleep | Permanent monologue; unparked implementers for §8 domains without owner Build open |
| **Aside / capture** | Short-lived attention threads | Arrest ideas, `target:` filings, **and human reflection sparks** without stealing main (T93/T95/T65) | Become multi-day zombie side-chats by default |

**Hard hierarchy (already doctrine):**

- Overseer routes product Build → **`jevons-po`**; sole spawn parent for
  product workers is the PO (T129). Exception: rehydrate dead PO first.
- PO is **spawn-only** for Build (T125).
- Multi-slice missions fan out early (T111.4); unattended non-design
  frontier leaves get workers same cycle (T155); file→spawn same turn
  (T193).
- **RSI control plane is root:** coach never mints the ledger; root files /
  alerts / briefs PO / ignores (T243). Mid-turn agent half is filing
  reflex (T130). Phrase-list mint (`JEVONS_RSI_MINT` / residual T92 path)
  is **not** product path.

---

## 3. Sensors vs LLM cycles

Always-on org behaviour is **not** "LLM awake forever." Separate:

### 3.1 Always-on sense (cheap, mechanical, non-LLM)

| Sensor | What it watches | Existing / planned surface |
|--------|-----------------|----------------------------|
| Daemon / health | Process up, HTTP `/health`, port truth | cockpit (T204), restart path (T188/T191) |
| Fleet registry | Phase, idle, stuck, lineage, model badge truth | agent_list, T118, T324 residual class |
| Eventlog | lifecycle errors, notify_queue, restart thrash | eventlog + agent-RSI evidence path |
| Chat usability | busy storms, attach/dead, integrity | T94, T33, T60 |
| Cost collector | burn rates, session counts, subscription honesty | T137 budget accounting |
| Frontier / ledger | unblocked leaves, stalls, engagement | bullseye, T198/T222 engage |
| **Coach drip** | owner main chat (priority) + eventlog + session transcripts | T243 RSI coach (judgment out, **no mint**) |
| **Session retro surfaces** | end-of-session / stop-point friction, repeated failure modes, doctrine slips | mnemo transcripts, eventlog samples, coach extract (sense only) |
| **Owner capture / reflection** | human sparks: habits, attention cost, life+work retro notes, education-of-self | asides (T93/T95), idea intake (§7), main chat priority |
| **Week-of-use / habit signal** | recurring owner friction, capacity thrash, "we should always…" patterns across days | coach drip + root RSI cycle; not automatic ledger mint |

These run on timers, hooks, and harness — **no monologue token burn**.
Reflection sensors are **structural sense inputs**, not a related-targets
footer.

### 3.2 Deliberate decide / act (LLM cycles)

| Cycle | Trigger | Actor | Bound |
|-------|---------|-------|-------|
| Owner turn | Owner speaks | Root CEO | Full interrupt; highest priority |
| PO proactive pass | Frontier non-empty or new leaf | Product PO | Work until frontier empty for *that product*, then **sleep** (first Build child) |
| Worker mission | Spawn + brief | Worker | One target / slice; finish with oracle evidence |
| Staff ops cycle | Interval or anomaly spike | Sentinel / ops staff | Observe→classify→repair **or** file+PO **or** snapshot to root; max actions/hour; then stop |
| **Agent RSI judgment** | Schedule / drip (coach) | Coach → **root** | Coach posts judgments only; root files / alerts / briefs PO / ignores. Coach **never** calls bullseye |
| **Agent RSI act (filing)** | Judgment accepted, or mid-work gap / standing rule | Root (or worker filing reflex T130) | Same-turn file or prompt-file (name + acceptance); skip one-off flukes; continuous improvement of org doctrine is core duty |
| **Human RSI / life+work retro** | Owner capture, week-of-use retro, self-reflection spark | Root (or light staff) | Durable capture + triage like ideas (§7): habits, attention opportunity cost, education-of-self — **first-class domain**, not agent-meta-only |
| Idea triage | Capture / aside / owner ideation | Root (or light staff) | Persist idea; score opportunity cost; route to bullseye/aside — no evaporation |

**Rule:** sensors fire continuously; **LLM cycles are episodic and bounded.**
Staff that never sleep are a product bug (cost + noise), not ambition.
RSI cycles that never run (deferred until "after real work") are an
**org-shape bug**, not thrift.

---

## 4. RSI + self-reflection (first-class, agent + human)

Reflection is **structural** on this map — not a side mention of T92/T243
in a related-targets table, and not optional garnish after product work.

### 4.1 Two halves (both first-class)

| Half | Subject | What "good" looks like | Product path (landed / residual) |
|------|---------|------------------------|----------------------------------|
| **Agent RSI** | The org / fleet / doctrine | Gaps and standing rules become ledger intent same turn; org improves itself under CEO duty | **T243** coach drip → judgments to overseer; overseer alone files/acts. **T130** filing reflex (mid-turn agent half). **T92** phrase-list mint residual **off product path** (`JEVONS_RSI_MINT` opt-in only — not standing ambient) |
| **Human RSI** | Owner life + work self | Owner self-reflection, habits, attention opportunity cost, education-of-self are captured and triaged like ideas — a **domain the org serves**, not only agent meta | Surfaces: asides, idea pipeline (§7), coach-priority owner chat, week-of-use retro. No ambient auto-implement of life OS (§8 park). Root triages; owner ratifies high-taste moves |

### 4.2 Agent RSI control plane (root decides)

```text
  sense (coach drip / session retro / eventlog / mid-mission gap)
       │
       ▼
  coach judgment (T243)  ──never──▶  bullseye mint
       │
       ▼
  root overseer (alter ego)
       │
       ├── file bullseye (T130 ceremony)
       ├── alert / brief PO
       ├── act on Build plane (spawn under PO hierarchy)
       └── ignore (one-off fluke / noise)
```

- **Coach never files bullseye.** Root is the intent-ledger control plane.
- **Filing reflex (T130)** is the agent half mid-turn: real product gap /
  repeated failure / standing behavioural rule → file or prompt-file same
  turn — not only "standing rule / going forward / from now on / we should
  always…" in chat.
- **Continuous improvement of the org itself is core CEO duty** (T98
  dimension 8 + this map) — not a side quest after product ships.

### 4.3 Human RSI (domain, not garnish)

Owner intent for T325 already named **RSI-for-human-and-agent** alongside
education, finance, health, leisure. On this map that means:

1. Human reflection sparks get the **same no-evaporation pipeline** as
   product ideas (§7) — durable capture → triage → file / park / ask owner.
2. Topics include habits, opportunity cost of attention, education of self,
   life+work retro — not only "how agents should behave."
3. The org **serves** that domain (capture, surface, prioritise); it does
   **not** ambient-automate whole-life OS while §8 domains stay parked.
4. **Reflection is first-class** (this whole section). The **CEO journal**
   (§8.1, parked) is the **durable surface** for structured periodic
   reflection over weeks — sense/decide longitudinal memory for the
   alter-ego org — distinct from chat scrollback and mnemo raw transcript.
   Capture sparks still flow through §7; the journal is the periodic
   written practice, not the spark pipe. **Park implement** until owner
   opens Build (same ceremony as §8 park table).

### 4.4 Explicit non-goals for this section

- Do **not** unpark or implement further RSI product code from this docs
  seat (T243 product path already landed; T92 mint stays residual/off).
- Do **not** promote phrase-list mint back onto the standing product path.
- Do **not** make the coach a second CEO that mints the ledger.
- Do **not** implement CEO journal product code from this docs seat
  (§8.1 inventory only until owner opens Build).

---

## 5. Staff functions (bounded ops, not permanent monologue)

Staff are **functions with a cycle contract**, not eternal co-workers chatting.
**RSI coach is a first-class staff function** (see §4), listed with other
ops — not demoted to a footnote.

| Staff function | Cycle contract | Outputs | Related |
|----------------|----------------|---------|---------|
| **RSI coach** | Drip-read (chat priority + eventlog + sessions); episodic judgment cycle | Judgments to overseer **only**; root files/acts/ignores | T243 (landed product path); T130 filing half; **T92 mint residual off product path** |
| **Health-of-health** | Interval + anomaly interrupt | harness-ok / repair / file+PO / ignore; cooldown on re-file | T219, T90, T204/T207 |
| **Resource snapshot** | Interval (e.g. shift or N min) | Compact brief to root: sessions, providers load, burn, frontier depth, idle PO count | T137, first staff child |
| **Factory kick** | New frontier leaf / empty-but-ready | Spawn under PO (not root parent) | T155; T254 family parked |
| **Idea + human-RSI intake** | On capture (product idea *or* owner self-reflection) | Durable record → prioritise or park — no evaporation | T93/T95; §4.3; Build child (3) |
| **Security / privacy / fraud / practical AI safety** | **Parked** (§8.1) — inventory only | When unparked: alert/file on phishing, money-movement risk, tool exfil, ATO, credential hygiene | T98 (owner ratifies high-stakes); not AGI doom theater |
| **System management** | **Parked** (§8.1) — inventory only | When unparked: inventory + hygiene + escalate anomalies (inbox, Drive, laptop, subs, finances, share-portfolio) | Not permanent monologue; owner opens Build first |
| **CEO journal** | **Parked** (§8.1) — inventory only | When unparked: root writes durable structured periodic reflection (longitudinal CEO memory) | §4 RSI cross-link; not chat scrollback / not mnemo raw transcript |

**Anti-patterns (explicit):**

- Staff that re-explain the same healthy state every loop.
- Staff that implement product code "while they're looking."
- Staff that file bullseye without root when their job is judgment-only (coach).
- Treating RSI / self-reflection as optional garnish after product work.
- Root that stays in unbounded solo plan loops on multi-slice work instead of fanning out.

---

## 6. Multi-provider resource management

Two different "provider" words must stay distinct:

| Word | Meaning here |
|------|----------------|
| **LLM harness provider** | Grok / Claude / GPT (and models under them) — agent runtime |
| **Tool provider** | mnemo, bullseye, … per [provider-contract.md](provider-contract.md) |

This section is **LLM harness portfolio** (owner: grok / claude / gpt
pro-max, load spread, task-type cost).

### 6.1 Goals

1. **Load spread** — do not pin every mission to one subscription until
   throttle; spread concurrent sessions by capacity and task fit.
2. **Task-type portfolio** — pick provider/model by *job class*, not habit.
3. **Honest cost** — subscription plans use T137 `accounting=subscription`
   (API-equivalent visibility, no false pause/kill on list $); paid APIs use
   list_price ladders.
4. **Truth-bound badges** — UI model label matches session truth (T324 class).

### 6.2 Task-type cost table — seed (not ratified prices)

Seed only for the multi-provider Build child. Numbers are **relative
preference / estimated burn**, not live billing APIs. Owner ratifies
before hard policy.

| Task type | Prefer | Avoid / secondary | Rationale (seed) |
|-----------|--------|-------------------|------------------|
| Fleet CEO / interruptible root | Default daily harness (today: Grok) | Swap mid-flight without reattach story | Continuity + owner chat path |
| Large multi-file code implement | Strong code model (Claude / GPT pro-max class as available) | Weak/cheap model on hard refactors | Quality bar T31 |
| Hermetic oracle / greps / mechanical | Cheapest capable | Flagship models | Cost hygiene |
| Design map / doctrine prose | Mid or flagship with long context | — | Coherence over thrash |
| Sentinel / ops classify | Small/fast if policy pure; else mid | Flagship monologue | Bounded cycle |
| Journeys / live Grok path | Provider under test | Cross-provider flakiness | Oracle honesty |
| Ideation / opportunity cost | Mid + tools (mnemo, calendar later) | Permanent ideation agent | Episodic triage |

**Load policy seed:**

- Cap concurrent sessions per provider (budget `max_sessions` + per-provider
  soft caps when multi-harness lands).
- Prefer under-utilised provider for *eligible* task types before queueing.
- Never silently demote an in-flight worker's provider mid-mission without
  rehydrate story (badge/session truth).

**Product seed (🎯T325.2):** pure table + routing live in
`internal/cost/portfolio.go` (`DefaultPortfolio`, `Route`, session
`soft_caps`). Mint path: `jevons_agent_start` with empty `provider` uses
`task_type` (or purpose-derived class) + registry load counts — prefers
fit, then under-utilised capacity; never mid-flight reassign. Optional
`budget.json` `provider_soft_caps` overlays caps. Residual: live vendor
quotas / billing APIs; owner-ratified prices; full marketplace OS.

---

## 7. Ideation, prioritization, opportunity cost

Owner intent: ideas across education, finance, health, leisure,
entertainment, **RSI-for-human-and-agent** (§4 — first-class, not
garnish), stalled hardware (e.g. cat-flap camera/prism), time management —
**must not evaporate**.

Human self-reflection sparks (habits, attention opportunity cost,
education-of-self, life+work retro) use the **same pipeline** as product
ideas — durable capture + triage. That is the human half of RSI on this
map (§4.3), not a separate afterthought channel.

### 7.1 Pipeline

```text
  spark (chat / aside / capture: / ambient / week-of-use retro / self-reflection)
       │
       ▼
  durable capture (aside, idea record, or bullseye draft)
       │
       ▼
  triage (root or light staff cycle)
       │
       ├── product-shaped → file bullseye (+ T193 spawn if Build-plane)
       ├── agent-RSI doctrine gap → root files (T130 / T243 control plane)
       ├── human-RSI / life reflection → capture + prioritise (domain serve;
       │     no ambient whole-life OS while §8 domains parked)
       ├── needs-owner / design → park-for-design or design-discussion
       ├── life-domain parked (see §8) → hold queue, no implementer
       └── drop / ignore (one-off noise) — rare; prefer park with reason
```

### 7.2 Opportunity cost (noisy dynamic optimization)

There is **no total order** on the frontier (Beads evaluation residual:
membership = ready; pick is engagement policy, not a single queue head).
Opportunity cost is therefore:

1. **Capacity** — free PO/worker slots vs open sessions/budget.
2. **Urgency / stall** — stuck missions, owner-blocking, health-of-health.
3. **Option value** — small pin of a large idea vs full Build.
4. **Decay** — ideas without durable capture die; capture without triage
   becomes sludge (must surface top-N to root on staff cycle).
5. **Attention (human RSI)** — owner focus is a scarce resource the org
   must protect and help the owner reallocate consciously (T98 attention
   dimension + §4.3).

**Not this map's job:** solve optimal control for life goals. **Is this
map's job:** ensure every spark has a **bucket** and every bucket has a
**next ceremony** (file / park / ask owner).

Surfaces: T93/T95 `target:` asides, T65 capture/aside, bullseye track,
filing reflex T130, coach drip + root RSI cycle (T243). Build child
**(3)** hardens "no evaporation" with an explicit idea→ledger path and
oracle (covers product ideas and human-RSI sparks alike) — see
[idea-capture.md](idea-capture.md) (🎯T325.3: `idea:` / dual-write
`capture:`, `POST/GET /api/ideas`, `jevons_idea_*`, triage dispositions).

---

## 8. Explicit park (owner must open Build)

Do **not** spawn implementers for these domains until the owner unparks.
Same ceremony as SWOT / life-domains / device life-app: **map inventory
only** until owner opens Build — not ambient T325.n Build children, not
unattended T155 workers.

**Human RSI capture is not parked** — reflection sparks still flow through
§7 (capture+triage). What is parked is **automated life-domain portfolios**
and whole-life OS implementers. Agent RSI product path (T243) is already
landed; this docs seat does not unpark further RSI product code.

| Parked domain | Why parked | Unpark signal |
|---------------|------------|---------------|
| **SWOT automation** | Strategic life analysis; high taste / class-3; easy to Goodhart | Owner opens Build or files a non-design leaf |
| **Life-domains automation** (education, finance, health, leisure, entertainment as automated portfolios) | Crosses product boundary into life OS; needs owner scope + oracles | Owner names first domain + acceptance |
| **Device life-app** (parking → life management; Jevons-on-device) | Mobile/thin-client residual; product not design-ready as org staff | Owner opens mobile life-app Build |
| **T254 factory parity children** | Already parked at parent | Owner opens factory Build |
| **Stalled hardware pursuits** (e.g. cat-flap camera/prism) | Physical + multi-repo; capture as ideas only until owner prioritises | Owner prioritises specific hardware leaf |
| **Security / privacy / fraud / AI safety (practical)** | Staff/domain function for phishing, money-movement risk, tool exfiltration, account takeover, credential hygiene — **not** existential AGI doom theater. High-stakes money/auth moves need owner ratify (T98 alter-ego still defers irreversible risk). Shape when unparked: bounded sense→alert/file cycles, not permanent monologue | Owner opens Build for practical security staff cycle + acceptance |
| **System management** | Staff/domain function for inbox, Google Drive, laptop(s), paid subscriptions, finances, share-portfolio cleanup/hygiene. Life-ops surface; easy to thrash or over-automate without owner scope | Owner opens Build for system-management hygiene cycles + acceptance |
| **CEO journal** | Root regularly writes a **durable journal** for longitudinal memory — famous leaders keep journals. Distinct from chat scrollback and mnemo raw transcript; structured periodic reflection for the alter-ego org (sense/decide over weeks, not one turn). Links human+agent RSI §4 (reflection as first-class; journal is the durable surface for that practice) | Owner opens Build for CEO journal practice + acceptance |

### 8.1 Parked staff inventory (shape only — no implementers)

These are **staff/domain functions** on the org map (same family as §5
bounded cycles and T219/T243 staff shape). They are **not** new T325.5 /
T325.6 Build slices unless the owner later files explicit
`set_aside` / parked-for-design children. Identity cross-link: 🎯T98
(CEO alter ego) — root may eventually allocate these cycles; owner still
ratifies high-stakes money/auth/Ship moves.

| Staff function (parked) | Cycle contract (when unparked) | Does not |
|-------------------------|--------------------------------|----------|
| **Security / privacy / fraud / practical AI safety** | Bounded sense → classify risk → alert root and/or file → stop; cooldown on re-alert; owner ratifies money moves, credential rotation, account recovery, tool-exfil policy changes | Permanent monologue; existential AGI doom theater; auto money/auth without owner; implement product code "while scanning" |
| **System management** | Inventory + hygiene cycles (inbox/Drive/laptop/subs/finances/share-portfolio clutter) → compact snapshot or escalate anomalies → idle | Permanent monologue agent; silent bulk delete/spend; whole-life OS without owner scope |
| **CEO journal** | Bounded periodic write cycle: structured reflection entry (decisions, sense of org health, open tensions, opportunity cost) → durable store → optional root re-read on next cycle / week-of-use; longitudinal memory for alter-ego CEO (weeks, not one turn) | Replace chat scrollback or mnemo raw transcript; permanent monologue "journal spam"; unattended implementers while parked; treat as idea-capture spark pipe (§7 is capture; journal is practice) |

**Allowed while parked:** idea capture, **human RSI / self-reflection
capture**, opportunity-cost notes, design discussion targets, owner chat.
**Forbidden while parked:** unattended T155 spawn of implementers for
these domains.

---

## 9. First Build slices (children of 🎯T325)

Filed as ledger children (same turn as this map). Implementers may be
spawned later under `parent=jevons-po` when leaves are non-design and
unblocked — **not** as part of this design-map seat's ship.

| # | Child (intent) | Acceptance seed |
|---|----------------|-----------------|
| **(1)** | **PO proactive-until-empty-then-sleep** | When product frontier has ready leaves, PO keeps spawning/briefing until empty or blocked; when empty, PO enters sleep/idle without zombie open-mission thrash; interruptible for owner/overseer directs. Hermetic and/or journey: empty frontier ⇒ no perpetual create thrash; non-empty ⇒ progress. |
| **(2)** | **Multi-provider load/token portfolio + task-type cost table seed** | Config or design-backed table maps task types → preferred harness provider/model; load/session soft caps; T137 accounting respected; seed table present and wired enough for one routing decision path (even if thin). Residual: live billing APIs. |
| **(3)** | **Idea capture → bullseye/aside without evaporation** | Owner spark via capture/aside/chat has durable destination within one ceremony; triage path documented + thin product path so ideas do not only exist in scrollback; oracle for "captured ⇒ listed or filed." Covers product ideas **and** human-RSI / self-reflection sparks (§4.3). |
| **(4)** | **One staff ops cycle** | Bounded cycle: health-of-health sample + resource snapshot delivered to root (not permanent monologue); classify harness-ok/repair/file/ignore; cooldown; hermetic pure policy tests. Aligns with T219 shape; may be thin vertical not full sentinel. |

Parent 🎯T325 **not self-achieved** by the map author — owner/overseer gates
acceptance of map + children existence (class-3 residual on full org).
RSI elevation in this note is **doctrine map only** — not a new Build
child and not further RSI product implementation.

---

## 10. Relation to existing targets (do not re-litigate)

| Target | Relationship to this map |
|--------|---------------------------|
| 🎯T98 | Identity doctrine; this note is **org structure** for that CEO; RSI/self-improvement is a first-class dimension (§4) + parked security/system-management staff still defer high-stakes money/auth/Ship to owner |
| 🎯T104 | Local Build vs Ship unchanged at org scale |
| 🎯T125 / T129 | PO spawn-only; overseer never parents product workers |
| 🎯T155 / T193 | Factory kick under PO; file→spawn |
| 🎯T254.* | Factory physics parent — **parked** |
| 🎯T219 | Full sentinel; child (4) may thin-slice toward it |
| 🎯T243 | **Agent RSI product path:** coach drip → judgments to overseer; coach **never** files bullseye; root decides (structural §4.2) |
| 🎯T130 | Filing reflex — mid-turn agent half of RSI (same-turn file, not chat-only "standing rule") |
| 🎯T92 | Ambient RSI parent; **phrase-list mint residual off product path** (not standing ambient) |
| 🎯T93 / T95 / T65 | Capture surfaces for ideation child **and** human-RSI sparks |
| 🎯T137 | Cost honesty for portfolio child |
| 🎯T200 | Portfolio path membership for domain POs |
| 🎯T31 / T31.1 / T194 | Done = evidence; daemon path needs daily probe |
| 🎯T326 / T327 | Simplify surfaces (shared 🎯 hotspot card; main progress clears on aside) — Algorithm step 3 examples |
| §12 | Leadership methods export (leaders + Algorithm); does not reopen park table |

---

## 11. Residual & non-goals

**Residual (class-3 / later):**

- Full life SWOT and multi-domain automation (§8 park).
- Device life-app.
- Practical security / privacy / fraud / AI-safety staff cycles (§8.1) —
  parked until owner opens Build; not AGI doom theater.
- System-management staff cycles (inbox, Drive, laptop, subs, finances,
  share-portfolio hygiene) — parked until owner opens Build (§8.1).
- **CEO journal** (durable periodic reflection / longitudinal CEO memory)
  — parked staff concept until owner opens Build (§8.1); §4 RSI
  cross-link only in this note.
- Optimal control / true dynamic optimisation of owner time.
- Hard multi-provider scheduler with live vendor quotas.
- Owner ratification of T98 checklist and this org map before harder
  persona law expansion.
- Richer human-RSI product surfaces (week-of-use retro chrome, habit
  trackers) — capture+triage doctrine is map-first; Build only when owner
  opens.

**Non-goals for this note:**

- Implement factory/sentinel/RSI product code in the same change as the map.
- Unpark T92 phrase-list mint onto the standing product path.
- Whole-org GitHub PR or Ship plane.
- Claiming T325 achieved before owner accepts map + children.
- Implementing §12 product bets (journal-cycle, priority-triage, Algorithm
  ritual) from this docs seat — inventory + cross-link only until owner
  opens Build.

---

## 12. Leadership methods (leaders + Algorithm)

**Status:** curated doctrine export from overseer research (owner session
`019fdf9d`) — **not** a new hierarchy and **not** an unpark of §8 domains.
Maps external leadership patterns onto roles already in §§2–8 and T98/T325.
Docs only; no product code from this seat.

**Bottom line:** externalize memory, protect think time, run a few
**rhythmic** forums, delegate execution with ownership, treat reflection
as work — then apply process first principles in **order** (question
requirements → delete thrash → simplify → accelerate cycle → automate
last). Climb **L1/L2** (hierarchy, sensors, delete thrash, local delivery)
before **L3** life ambitions and automate-everything staff.

### 12.1 Leaders in general (recurring practices)

Recurring patterns from historical leaders (da Vinci notebooks, Franklin,
Eisenhower-class diaries; notebook culture) and modern CEO habit research
(priority-few, meeting cadence, journaling, delegation) — curated for
Jevons, not biography.

| Practice | Core idea | Jevons analogue (existing map) |
|----------|-----------|--------------------------------|
| **Externalize thought** | Writing stops thrash; later navigability | CEO journal parked (§4.3, §8.1) — durable synthesis, **not** chatlog/mnemo raw; asides + idea capture (T93/T95, 🎯T325.3) = pocket notebook |
| **Few priorities** | Ruthless high-impact few; rest is staff or later | Idea pipeline + opportunity cost (§7): capture freely, **promote few** to Build; weekly attention portfolio (taste + capacity), not infinite frontier guilt |
| **Meeting / cadence rhythm** | Regular review beats ad-hoc crisis only | Episodic LLM cycles (§3.2): staff ops (🎯T325.4 / T219 shape), RSI coach (T243), PO proactive sleep (🎯T325.1), owner digest — **not** permanent monologue |
| **Delegation with ownership** | Clear who owns what; not abdication | Root routes; **PO spawn-only** (T125); sole product-worker parent = PO (T129); workers execute + evidence finish; owner gates Ship/taste/credentials (T98/T104) |
| **Protect alone / think time** | Strategy needs uninterrupted blocks | Sensors continuous, LLM episodic (§3); drip when dirty / scheduled strategy — no thrash remints |
| **Reflection as duty** | What happened / learned / change | Agent RSI + human RSI first-class (§4); coach → root files (T243/T130); not garnish after product |
| **Character under ego load** | Orchestration, know what you don't know | Structure over shouting: lineage, exclusive engage, fleet tree; hard domains (money, security, Ship) → **owner gate** (§8 park + T98) |

**Five CEO meetings → five product rhythms** (cheap pilot language only;
aligns existing cycles — does not mint new staff):

| Rhythm | Product surface already on the map |
|--------|--------------------------------------|
| Status | Staff ops cycle (🎯T325.4 seed → T219) |
| All-hands | Optional owner digest (what the fleet did) — residual chrome |
| 1:1 | PO direct when product stuck |
| Retro | RSI coach + human reflection (§4) |
| Wins | Explicit positive capture (morale + learning) — residual |

**Ownership on the mountain (L1 / L2 / L3):**

| Layer | Who | Owns |
|-------|-----|------|
| **L1** | Hierarchy + mechanical truth | Roles (T125/T129), oracles (T31), local vs Ship (T104), sensors that do not burn tokens (§3.1) |
| **L2** | Root + staff cycles + POs | Attention portfolio, staff briefs, RSI control plane, idea triage, journal **when unparked**, multi-provider portfolio seed (🎯T325.2) |
| **L3** | Owner-gated / parked | Life-domain automation, SWOT OS, device life-app, practical security & system-management implementers, full journal product — **§8 park** until owner opens Build |

Root is **navigator and integrator** (alter ego T98), not permanent
implementer. Machines sense; agents judge in bounded cycles; hierarchy
acts. Notebook always at hand (asides/ideas); journal is the bound book
(periodic synthesis) — same distinction as §4.3 / §8.1.

**What not to copy (leaders lore):** wake-at-4am cosplay; meeting overload
as virtue (agent failure mode is token thrash on continues, not calendar
spam); cult of the lone genius (contradicts PO/worker design); existential
AI-safety theater (practical safety only when unparked).

**Thin product bets** (inventory only — **do not** implement from this
seat; cross-link, do not fork parallel essays):

1. **Journal-cycle** — already parked as CEO journal (§8.1): first-
   principles / decision log (requirements questioned, deletes, lessons),
   not diary cosplay. Unpark when owner opens Build.
2. **Priority-triage** — harden “capture freely, promote few” on §7 +
   opportunity cost; optional weekly priority-few habit. Complements
   🎯T325.3 idea capture without infinite Build.
3. Wins + retro staff outputs ride RSI (§4) / staff ops — not a third
   monologue path.

### 12.2 Musk Algorithm (process first principles)

Sibling export: first principles applied to **process**, not factory
cosplay. **Order is sacred** — most orgs (and agent systems) jump to
automate (5) or speed (4) first.

| # | Step | Essence |
|---|------|---------|
| **1** | **Question requirements** | Every requirement has a **named owner** (human or named target), not a department. Question even smart people's requirements. |
| **2** | **Delete** | Delete more than is comfortable. **If you never add ~10% back, you didn't delete enough.** |
| **3** | **Simplify / optimize** | Only *after* delete. Optimizing something that should not exist is the trap. |
| **4** | **Accelerate cycle time** | Speed *after* 1–3. Accelerating a process you later delete is wasted motion. |
| **5** | **Automate last** | Automating a bad process makes a faster bad process. |

Underneath: reason from fundamentals, not “what others do” or “what we
inherited.”

**Decision table → Jevons** (reference targets; do not re-litigate):

| Step | Jevons application |
|------|--------------------|
| **1 Requirements** | Named ownership of doctrine, acceptance, long-lived process rules (idle pressure, RSI residual, staff contracts). No “the system requires endless continues.” |
| **2 Delete** | Delete thrash: open-mission zombie heuristics (🎯T325.1 sleep), bare-status acks, dual progress chrome (🎯T327), dual conversation paths (done class). Prefer fewer staff monologues (§5 anti-patterns). |
| **3 Simplify** | Org map (this note / 🎯T325) before new staff agents; shared frontier-card for 🎯 hotspots (🎯T326), not a second card. |
| **4 Cycle time** | Local master (🎯T104) + thin slices; brief→work lands (🎯T305 class) so spawn is one cycle, not paste purgatory. |
| **5 Automate** | Idle pressure, portfolio routing, idea API, sentinel — **after** delete/simplify. Automate last checklist before new daemon loops. |

**Journal under Algorithm:** periodic write-up of requirements questioned,
things deleted, cycles accelerated — same first-principles log as §12.1 /
§8.1, not diary cosplay. **RSI spine matches:** observe failure → change
the process (coach → root files → product), not only work harder (§4).

**What not to copy:**

- Permanent crisis as culture (burnout / chaos).
- CEO implements forever (root/PO already spawn-only for product Build —
  T125/T129).
- Growth without oracles (T31 / T31.1 / T194 anti-that).
- Manufacturing-everything when taste / class-3 applies (life domains
  parked §8).

**Thin product bets** (optional later; not automatic children of T325):

1. **Staff Algorithm ritual** — when stuck: short staff note forced through
   order 1→5.
2. **10% restore metric** — a stretch with zero “deleted then re-added”
   means deletion was too timid.
3. **Named requirement owner** on every long-lived process rule.
4. **Automate-last checklist** before new always-on daemon loops.

### 12.3 One ladder (mountain)

**L1/L2 before L3:** delete thrash and fix hierarchy/sensors/local
delivery (requirements → delete → simplify → cycle time) **before**
automating staff forever or growing parked L3 life ambitions. Algorithm
order and leadership cadence are the same climb: **few owned priorities,
bounded rhythms, subtraction before automation.**

---

## 13. History

- **2026-08-08** — Owner filed 🎯T325: root as CEO of life-and-work org
  (always-on sense/decide/act; multi-provider portfolio; staff; idea
  pipeline). Design-map mission: this note + four first Build children;
  park SWOT / life-domains / device life-app until owner opens Build.
- **2026-08-08** — Staff inventory residual: park **practical security /
  privacy / fraud / AI safety** and **system management** as staff/domain
  functions (§8 table + §8.1); no T325.5/6 Build slices; T98 cross-link;
  implement only when owner opens Build (T325 residual / owner capture).
- **2026-08-08** — **RSI + self-reflection first-class (agent + human):**
  elevated from related-targets garnish to structural §4 (control plane,
  dual halves), sense/decide tables (§3), staff table, ideation pipeline
  (§7). T243 coach never files; T92 mint residual off product path. Docs
  only — no RSI product code this seat. Companion: T98 dimension 8.
- **2026-08-08** — **CEO journal** parked staff concept (§8 park table +
  §8.1 inventory + §5 staff row + §4.3 RSI cross-link): durable structured
  periodic reflection for alter-ego longitudinal memory; distinct from
  chat scrollback and mnemo raw transcript; park implement until owner
  opens Build. Docs only — no journal product code this seat.
- **2026-08-08** — **§12 Leadership methods (leaders + Algorithm):**
  dual export from overseer research (session `019fdf9d`) — recurring
  leader practices (externalize, few priorities, cadence, delegation)
  mapped to L1/L2/L3 + T98/T325; Musk Algorithm 1→5 as process first
  principles with Jevons decision table, what-not-to-copy, thin bets.
  Cross-links journal §8.1, RSI §4, idea §7, T326/T327, T31/T104,
  T125/T129. Docs only — no product unpark.
