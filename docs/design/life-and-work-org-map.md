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
| 🎯T92 / 🎯T243 RSI | Ambient self-improvement: coach judges → overseer decides/files |
| 🎯T93 / T95 asides | Light idea/target capture without polluting main flow |
| [supergrok-cost-accounting.md](supergrok-cost-accounting.md) (🎯T137) | Subscription vs list-price cost honesty |
| [provider-contract.md](provider-contract.md) | Tool providers (feeds/ui/mcp) — orthogonal to LLM harness providers |
| [attention-threads.md](attention-threads.md) (🎯T65) | Main vs aside attention model |

This note is the **org shape**: who senses, who decides, who acts, how
resources and ideas flow. It does **not** implement factory, sentinel, or
RSI code. Whole-org Ship is out of scope.

---

## 1. North star

**Root Jevons is CEO of Marcelo's life-and-work organisation** — an
always-on sense / decide / act loop with a multi-provider resource
portfolio, product owners, ancillary staff, and an idea→priority pipeline.

It is **not**:

- a chat wrapper that goes idle between owner turns
- a single coding agent with a nicer UI
- a permanent monologue of staff agents burning tokens without cycles
- Gas Town / Beads reimplemented wholesale (factory parity lives under
  parked 🎯T254; escape-hatch evaluation informs this map, does not unpark it)

**Owner test (extends T98):** if the owner watched a silent hour of root
choices across product *and* life-adjacent capacity (ideas, stall recovery,
resource allocation), would they mostly say "that's what I would have
done"? When no — doctrine or product is wrong.

**Root remains interruptible CEO.** Owner speech always pre-empts ambient
cycles. Root does not go dark implementing product (hierarchy T125/T129).

---

## 2. Roles

```text
                    Owner (ratifies constitution, Ship, taste, credentials)
                                      │
                                      ▼
              ┌─────────────────────────────────────────┐
              │  Root CEO  jevons  (overseer / alter ego) │
              │  sense · decide · act · interruptible     │
              └─────────────────────────────────────────┘
                     │                    │
         ┌───────────┼──────────┐         │ staff (bounded cycles)
         ▼           ▼          ▼         ▼
      Portfolio   Product     Product   Sentinel / ops staff
      (path)      Owner(s)    Owner(s)  RSI coach (judgment only)
         │           │          │         │
         │           ▼          ▼         │
         │        Workers    Workers      └→ brief PO / escalate root
         │        (Build)    (Build)
         ▼
   Domain POs (personal / minicades / …) — product-scoped only
```

| Role | Who | Mandate | Does not |
|------|-----|---------|----------|
| **Owner** | Marcelo | Constitution, Ship plane, taste, MAJOR/PATCH, irreversible risk | Run the factory by hand every day |
| **Root CEO** | `jevons` overseer | Always-on org control: allocate attention, spawn staff cycles, route to POs, relay outcomes, file when doctrine demands | Implement product code; parent product workers (T129); open Ship ambiently (T104) |
| **Portfolio** | Config path membership (T200) | Group POs under life/work buckets (e.g. personal `github.com/marcelocantos`) | Parse agent names for membership |
| **Product Owner (PO)** | e.g. `jevons-po` | Product-scoped proactive Build: spawn workers, gate achieve, stay interruptible | Solo implement Build (T125); own whole-life SWOT |
| **Worker** | Named fleet agents | Execute one mission; evidence-gated finish; auto-deregister on done (T165/T195) | Open Ship; redefine org doctrine mid-flight without filing |
| **Staff (ancillary)** | Sentinel (T219), RSI coach (T243), future ops cycles; **parked** practical security + system management (§7.1) | **Bounded ops cycles** — observe, classify, repair-or-file, snapshot resources; then idle/sleep | Permanent monologue; mint ledger without root (RSI coach never files bullseye); unparked implementers for §7 domains without owner Build open |
| **Aside / capture** | Short-lived attention threads | Arrest ideas and `target:` filings without stealing main (T93/T95/T65) | Become multi-day zombie side-chats by default |

**Hard hierarchy (already doctrine):**

- Overseer routes product Build → **`jevons-po`**; sole spawn parent for
  product workers is the PO (T129). Exception: rehydrate dead PO first.
- PO is **spawn-only** for Build (T125).
- Multi-slice missions fan out early (T111.4); unattended non-design
  frontier leaves get workers same cycle (T155); file→spawn same turn
  (T193).

---

## 3. Sensors vs LLM cycles

Always-on org behaviour is **not** "LLM awake forever." Separate:

### 3.1 Always-on sense (cheap, mechanical, non-LLM)

| Sensor | What it watches | Existing / planned surface |
|--------|-----------------|----------------------------|
| Daemon / health | Process up, HTTP `/health`, port truth | cockpit (T204), restart path (T188/T191) |
| Fleet registry | Phase, idle, stuck, lineage, model badge truth | agent_list, T118, T324 residual class |
| Eventlog | lifecycle errors, notify_queue, restart thrash | eventlog + RSI evidence path |
| Chat usability | busy storms, attach/dead, integrity | T94, T33, T60 |
| Cost collector | burn rates, session counts, subscription honesty | T137 budget accounting |
| Frontier / ledger | unblocked leaves, stalls, engagement | bullseye, T198/T222 engage |
| Coach drip | owner chat priority + eventlog + sessions | T243 RSI coach (judgment out, no mint) |

These run on timers, hooks, and harness — **no monologue token burn**.

### 3.2 Deliberate decide / act (LLM cycles)

| Cycle | Trigger | Actor | Bound |
|-------|---------|-------|-------|
| Owner turn | Owner speaks | Root CEO | Full interrupt; highest priority |
| PO proactive pass | Frontier non-empty or new leaf | Product PO | Work until frontier empty for *that product*, then **sleep** (first Build child) |
| Worker mission | Spawn + brief | Worker | One target / slice; finish with oracle evidence |
| Staff ops cycle | Interval or anomaly spike | Sentinel / ops staff | Observe→classify→repair **or** file+PO **or** snapshot to root; max actions/hour; then stop |
| RSI judgment cycle | Schedule / drip | Coach → root | Coach posts judgments only; root files / alerts / ignores |
| Idea triage | Capture / aside / owner ideation | Root (or light staff) | Persist idea; score opportunity cost; route to bullseye/aside — no evaporation |

**Rule:** sensors fire continuously; **LLM cycles are episodic and bounded.**
Staff that never sleep are a product bug (cost + noise), not ambition.

---

## 4. Staff functions (bounded ops, not permanent monologue)

Staff are **functions with a cycle contract**, not eternal co-workers chatting.

| Staff function | Cycle contract | Outputs | Related |
|----------------|----------------|---------|---------|
| **Health-of-health** | Interval + anomaly interrupt | harness-ok / repair / file+PO / ignore; cooldown on re-file | T219, T90, T204/T207 |
| **Resource snapshot** | Interval (e.g. shift or N min) | Compact brief to root: sessions, providers load, burn, frontier depth, idle PO count | T137, first staff child |
| **RSI coach** | Drip-read | Judgments to overseer only | T243 (landed); T92 mint residual off product path |
| **Factory kick** | New frontier leaf / empty-but-ready | Spawn under PO (not root parent) | T155; T254 family parked |
| **Idea intake** | On capture | Durable idea record → prioritise or park | T93/T95; Build child (3) |
| **Security / privacy / fraud / practical AI safety** | **Parked** (§7.1) — inventory only | When unparked: alert/file on phishing, money-movement risk, tool exfil, ATO, credential hygiene | T98 (owner ratifies high-stakes); not AGI doom theater |
| **System management** | **Parked** (§7.1) — inventory only | When unparked: inventory + hygiene + escalate anomalies (inbox, Drive, laptop, subs, finances, share-portfolio) | Not permanent monologue; owner opens Build first |

**Anti-patterns (explicit):**

- Staff that re-explain the same healthy state every loop.
- Staff that implement product code "while they're looking."
- Staff that file bullseye without root when their job is judgment-only (coach).
- Root that stays in unbounded solo plan loops on multi-slice work instead of fanning out.

---

## 5. Multi-provider resource management

Two different "provider" words must stay distinct:

| Word | Meaning here |
|------|----------------|
| **LLM harness provider** | Grok / Claude / GPT (and models under them) — agent runtime |
| **Tool provider** | mnemo, bullseye, … per [provider-contract.md](provider-contract.md) |

This section is **LLM harness portfolio** (owner: grok / claude / gpt
pro-max, load spread, task-type cost).

### 5.1 Goals

1. **Load spread** — do not pin every mission to one subscription until
   throttle; spread concurrent sessions by capacity and task fit.
2. **Task-type portfolio** — pick provider/model by *job class*, not habit.
3. **Honest cost** — subscription plans use T137 `accounting=subscription`
   (API-equivalent visibility, no false pause/kill on list $); paid APIs use
   list_price ladders.
4. **Truth-bound badges** — UI model label matches session truth (T324 class).

### 5.2 Task-type cost table — seed (not ratified prices)

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

## 6. Ideation, prioritization, opportunity cost

Owner intent: ideas across education, finance, health, leisure,
entertainment, RSI-for-human-and-agent, stalled hardware (e.g. cat-flap
camera/prism), time management — **must not evaporate**.

### 6.1 Pipeline

```text
  spark (chat / aside / capture: / ambient)
       │
       ▼
  durable capture (aside, idea record, or bullseye draft)
       │
       ▼
  triage (root or light staff cycle)
       │
       ├── product-shaped → file bullseye (+ T193 spawn if Build-plane)
       ├── needs-owner / design → park-for-design or design-discussion
       ├── life-domain parked (see §7) → hold queue, no implementer
       └── drop / ignore (one-off noise) — rare; prefer park with reason
```

### 6.2 Opportunity cost (noisy dynamic optimization)

There is **no total order** on the frontier (Beads evaluation residual:
membership = ready; pick is engagement policy, not a single queue head).
Opportunity cost is therefore:

1. **Capacity** — free PO/worker slots vs open sessions/budget.
2. **Urgency / stall** — stuck missions, owner-blocking, health-of-health.
3. **Option value** — small pin of a large idea vs full Build.
4. **Decay** — ideas without durable capture die; capture without triage
   becomes sludge (must surface top-N to root on staff cycle).

**Not this map's job:** solve optimal control for life goals. **Is this
map's job:** ensure every spark has a **bucket** and every bucket has a
**next ceremony** (file / park / ask owner).

Surfaces: T93/T95 `target:` asides, T65 capture/aside, bullseye track,
filing reflex T130. Build child **(3)** hardens "no evaporation" with an
explicit idea→ledger path and oracle.

---

## 7. Explicit park (owner must open Build)

Do **not** spawn implementers for these domains until the owner unparks.
Same ceremony as SWOT / life-domains / device life-app: **map inventory
only** until owner opens Build — not ambient T325.n Build children, not
unattended T155 workers.

| Parked domain | Why parked | Unpark signal |
|---------------|------------|---------------|
| **SWOT automation** | Strategic life analysis; high taste / class-3; easy to Goodhart | Owner opens Build or files a non-design leaf |
| **Life-domains automation** (education, finance, health, leisure, entertainment as automated portfolios) | Crosses product boundary into life OS; needs owner scope + oracles | Owner names first domain + acceptance |
| **Device life-app** (parking → life management; Jevons-on-device) | Mobile/thin-client residual; product not design-ready as org staff | Owner opens mobile life-app Build |
| **T254 factory parity children** | Already parked at parent | Owner opens factory Build |
| **Stalled hardware pursuits** (e.g. cat-flap camera/prism) | Physical + multi-repo; capture as ideas only until owner prioritises | Owner prioritises specific hardware leaf |
| **Security / privacy / fraud / AI safety (practical)** | Staff/domain function for phishing, money-movement risk, tool exfiltration, account takeover, credential hygiene — **not** existential AGI doom theater. High-stakes money/auth moves need owner ratify (T98 alter-ego still defers irreversible risk). Shape when unparked: bounded sense→alert/file cycles, not permanent monologue | Owner opens Build for practical security staff cycle + acceptance |
| **System management** | Staff/domain function for inbox, Google Drive, laptop(s), paid subscriptions, finances, share-portfolio cleanup/hygiene. Life-ops surface; easy to thrash or over-automate without owner scope | Owner opens Build for system-management hygiene cycles + acceptance |

### 7.1 Parked staff inventory (shape only — no implementers)

These are **staff/domain functions** on the org map (same family as §4
bounded cycles and T219/T243 staff shape). They are **not** new T325.5 /
T325.6 Build slices unless the owner later files explicit
`set_aside` / parked-for-design children. Identity cross-link: 🎯T98
(CEO alter ego) — root may eventually allocate these cycles; owner still
ratifies high-stakes money/auth/Ship moves.

| Staff function (parked) | Cycle contract (when unparked) | Does not |
|-------------------------|--------------------------------|----------|
| **Security / privacy / fraud / practical AI safety** | Bounded sense → classify risk → alert root and/or file → stop; cooldown on re-alert; owner ratifies money moves, credential rotation, account recovery, tool-exfil policy changes | Permanent monologue; existential AGI doom theater; auto money/auth without owner; implement product code "while scanning" |
| **System management** | Inventory + hygiene cycles (inbox/Drive/laptop/subs/finances/share-portfolio clutter) → compact snapshot or escalate anomalies → idle | Permanent monologue agent; silent bulk delete/spend; whole-life OS without owner scope |

**Allowed while parked:** idea capture, opportunity-cost notes, design
discussion targets, owner chat. **Forbidden while parked:** unattended
T155 spawn of implementers for these domains.

---

## 8. First Build slices (children of 🎯T325)

Filed as ledger children (same turn as this map). Implementers may be
spawned later under `parent=jevons-po` when leaves are non-design and
unblocked — **not** as part of this design-map seat's ship.

| # | Child (intent) | Acceptance seed |
|---|----------------|-----------------|
| **(1)** | **PO proactive-until-empty-then-sleep** | When product frontier has ready leaves, PO keeps spawning/briefing until empty or blocked; when empty, PO enters sleep/idle without zombie open-mission thrash; interruptible for owner/overseer directs. Hermetic and/or journey: empty frontier ⇒ no perpetual create thrash; non-empty ⇒ progress. |
| **(2)** | **Multi-provider load/token portfolio + task-type cost table seed** | Config or design-backed table maps task types → preferred harness provider/model; load/session soft caps; T137 accounting respected; seed table present and wired enough for one routing decision path (even if thin). Residual: live billing APIs. |
| **(3)** | **Idea capture → bullseye/aside without evaporation** | Owner spark via capture/aside/chat has durable destination within one ceremony; triage path documented + thin product path so ideas do not only exist in scrollback; oracle for "captured ⇒ listed or filed." |
| **(4)** | **One staff ops cycle** | Bounded cycle: health-of-health sample + resource snapshot delivered to root (not permanent monologue); classify harness-ok/repair/file/ignore; cooldown; hermetic pure policy tests. Aligns with T219 shape; may be thin vertical not full sentinel. |

Parent 🎯T325 **not self-achieved** by the map author — owner/overseer gates
acceptance of map + children existence (class-3 residual on full org).

---

## 9. Relation to existing targets (do not re-litigate)

| Target | Relationship to this map |
|--------|---------------------------|
| 🎯T98 | Identity doctrine; this note is **org structure** for that CEO; parked security/system-management staff still defer high-stakes money/auth/Ship to owner |
| 🎯T104 | Local Build vs Ship unchanged at org scale |
| 🎯T125 / T129 | PO spawn-only; overseer never parents product workers |
| 🎯T155 / T193 | Factory kick under PO; file→spawn |
| 🎯T254.* | Factory physics parent — **parked** |
| 🎯T219 | Full sentinel; child (4) may thin-slice toward it |
| 🎯T92 / T243 | RSI coach → root; filing reflex remains agent half |
| 🎯T93 / T95 / T65 | Capture surfaces for ideation child |
| 🎯T137 | Cost honesty for portfolio child |
| 🎯T200 | Portfolio path membership for domain POs |
| 🎯T31 / T31.1 / T194 | Done = evidence; daemon path needs daily probe |

---

## 10. Residual & non-goals

**Residual (class-3 / later):**

- Full life SWOT and multi-domain automation (§7 park).
- Device life-app.
- Practical security / privacy / fraud / AI-safety staff cycles (§7.1) —
  parked until owner opens Build; not AGI doom theater.
- System-management staff cycles (inbox, Drive, laptop, subs, finances,
  share-portfolio hygiene) — parked until owner opens Build (§7.1).
- Optimal control / true dynamic optimisation of owner time.
- Hard multi-provider scheduler with live vendor quotas.
- Owner ratification of T98 checklist and this org map before harder
  persona law expansion.

**Non-goals for this note:**

- Implement factory/sentinel/RSI code in the same PR as the map.
- Whole-org GitHub PR or Ship plane.
- Claiming T325 achieved before owner accepts map + children.

---

## 11. History

- **2026-08-08** — Owner filed 🎯T325: root as CEO of life-and-work org
  (always-on sense/decide/act; multi-provider portfolio; staff; idea
  pipeline). Design-map mission: this note + four first Build children;
  park SWOT / life-domains / device life-app until owner opens Build.
- **2026-08-08** — Staff inventory residual: park **practical security /
  privacy / fraud / AI safety** and **system management** as staff/domain
  functions (§7 table + §7.1); no T325.5/6 Build slices; T98 cross-link;
  implement only when owner opens Build (T325 residual / owner capture).
