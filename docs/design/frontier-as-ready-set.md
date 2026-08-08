# Frontier-as-ready-set (🎯T262 decision packet)

**Status:** design evaluation complete for T262.1–T262.3; **T262.4 needs-owner** — do not treat this packet as owner accept.  
**Date:** 2026-08-05  
**Scope:** doctrine + product-boundary map. **Not** a Build swarm. **Not** unpark of 🎯T254.  
**Delivery:** local master only (🎯T104). No Beads dual-write. No PR/ship.

---

## 1. Owner-corrected model (do not re-argue)

| Claim | Meaning |
|-------|---------|
| **Frontier = ready set** | Active leaf targets whose `depends_on` are all terminal. Membership = unblocked and ready. |
| **"What is the next ticket?" is a non-question** | Assumes a hidden total order the graph does not provide. Picking among ready leaves is **indifferent or policy**, not discovery of a true head. |
| **Frontier ⊃ queue** | A queue is frontier size 1 plus an invented serial order (capacity mutex, taste, or ritual). Queues are a special case, not the general work shape. |
| **Multi-agent default** | **Worker per frontier leaf**, refined by **policy-on-set** (capacity, file ownership, design/park filters, churn) — not by Beads-style assignment *as* readiness. |
| **T254 relationship** | Gas Town/Beads factory parity stays **parked**. This eval **informs** T254; mass unpark requires **owner accept on T262.4**, not this packet alone. |

### Anti-patterns

1. Framing bullseye (or `/cv` alone) as the product answer to **"the next ticket"**.
2. Inventing a global queue head so multi-agent work feels ordered.
3. Dual-writing a second intent graph (Beads or otherwise) for continuity gaps.
4. Treating engagement, spawn caps, or design-gates as proof that readiness must be a queue.

### Queue as special case (explicit)

Serial work is still legitimate when:

- **Capacity mutex:** one human attention slice, one merge lane, one fragile shared file without worktree isolation.
- **Hard product dependency:** not yet encoded as `depends_on` (fix by filing edges, not by queue folklore).
- **Owner ritual:** "do this one first" — owner intent overrides the frontier (bullseye already records claims; it does not assign work).

None of these restore "next ticket" as the **default** mental model.

---

## 2. Existing product map (what already implements the model)

| Surface | Target | Status | Role in ready-set model |
|---------|--------|--------|-------------------------|
| Unattended frontier auto-spawn | **T155** | achieved (instructional) | Continuous kick-off of non-design frontier leaves under `parent=jevons-po` — consume the **set**, not a queue head. Residual: no daemon auto-spawn gate. |
| File→spawn same turn | **T193** | achieved (instructional) | New Build leaf → worker in same turn; not ledger-only. Complements T155 for owner/mid-session filing. |
| Frontier engagement overlay | **T198** | achieved (product) | Jevons-only overlay: `target_id` → engaged row sinks, play→stop. Does **not** rewrite bullseye status. Readiness stays in the ledger; engagement is factory state. |
| Exclusive engage + near-dup file gate | **T222** | achieved (product) | Policy-on-set: no second implementer; no duplicate mission id. Play on engaged/closed refuses. |
| RHS live frontier table | **T131** (+ T159 API path) | achieved | Owner-visible ready set (`GET /api/frontier`). |
| Unattended safe batch encapsulation | **T109** | (batch membership; parent lifecycle per ledger) | Example of **filtering the set** (unattended-safe) without inventing order. |
| Worker reap on done / achieve | **T165 / T195** | product | Engagement ends when work claims done or mission target achieves — set membership updates as leaves leave the frontier. |
| Open-mission / stuck recovery | **T236 / T244** family | product path | Continuity without a Beads mail bus (partial; T254.5 deepens). |
| Hierarchy: PO spawns, overseer routes | **T125 / T129** | instructional | Spawn parent for product workers = PO; readiness consumption is PO/factory, not overseer ticket desk. |

### Bullseye already aligned (ledger side)

From bullseye docs (`docs/mcp-triad.md`, agents-guide):

- Within a repo, agents can work **every frontier target in parallel**; ranking is a **guide**, not a serialisation constraint.
- Repo frontier order = unblocking fanout, then ID — optional policy signal, **not permission**.
- Product identity: intent ledger + claim lifecycle — **not a task assigner**.
- `assign` / `unassign` = ownership exclusion (someone else driving); dependents stay blocked. Distinct from `set_aside`.

### Tension to resolve (doctrine / UX language)

| Surface | Issue | Disposition (recommend) |
|---------|-------|-------------------------|
| `bullseye_convergence` tool description | "what's the **next most-valuable** thing…" + `**Execute now**: Work on 🎯T…` | **Ship-bullseye (docs/UX language):** rephrase as **focus recommendation within the ready set** for single-agent / human-attention slices; never imply exclusive readiness. Parallel wording already exists for multi-top-frontier. |
| Jevons fleet brief / persona | Strong on T155/T193 spawn; weak on explicit "ready set ≠ next ticket" | **Ship-Jevons (doctrine inject):** short doctrine block after owner accept on T262.4 (or with T262.1 close). |
| Agent prose / overseer habit | "What's next on the frontier?" as queue language | **Doctrine + brief** — not a new subsystem. |

---

## 3. T262.1 — Written doctrine (readiness set)

### Canonical wording (load into AGENTS / persona / fleet brief when owner opens doctrine land)

> **Frontier = ready set.** Every unblocked leaf is legitimate work. There is no privileged "next ticket." A queue is frontier size ≤1 with invented order. Multi-agent default: one work agent per ready leaf, subject to engagement policy (capacity, ownership, design/park filters, churn). Bullseye records intent and computes readiness; Jevons engages implementers. Neither product answers "the next ticket" as a total order.

### Checkable acceptance (T262.1)

- [x] This design doc states frontier = unblocked ready leaves; pick is indifferent/policy.
- [x] Explicit anti-pattern: bullseye as "next ticket" oracle.
- [x] Queue treated as special case, not general shape.
- [x] AGENTS / persona / FleetStandingBrief inject updated (land **after** owner accept or as thin T262.1 follow-up commit — **do not** claim doctrine inject done until greps green).

**T262.1 close path:** thin docs+grep commit (persona + AGENTS + agents-guide + FleetStandingBrief markers); hermetic: `internal/config` + `fleet_brief` doctrine greps for ready-set marker strings. Does **not** achieve T262.4 or unpark T254.

---

## 4. T262.2 — Multi-agent: worker-per-leaf vs engagement policy

### Model

```
ready_set = bullseye frontier (minus design/park/needs-owner filters)
for leaf in ready_set:
  if engaged(leaf): skip  # T198/T222
  elif policy_allows(leaf): spawn worker(target_id=leaf)  # T155/T193
  else: record explicit skip reason (cap / ownership / park)
```

**Naïve-and-correct:** one worker per leaf.  
**Policy-on-set (not a queue):**

| Policy | Existing | Gap |
|--------|----------|-----|
| Design / needs-owner / parked skip | T155, T193 doctrine | Instructional residual → **T254.1** hardens daemon enforcement |
| Exclusive engagement | T222, T198 | Sufficient for single implementer; multi-agent same target residual noted on T198 |
| Capacity / max parallel | Informal | **T254.6** factory posture + optional spawn caps |
| Same-repo file thrash | Ad hoc worktrees | **T254.2** worktree + integrator |
| Frontier churn (new leaves mid-flight) | T155 continuous kick-off | Mostly covered; churn storm → factory posture rate limits under T254.6 |
| Step-ordered work inside one target | Weak | **T254.3** ordered steps (not a global queue) |
| Structured finish → parent | Partial (event push / reap) | **T254.4** fleet ops inbox |
| Stuck/idle recovery | T236/T244 | **T254.5** standing product path |

**Classification (explicit):** capacity, shared-file ownership, design/parked/needs-owner filters, and frontier churn are **policy-on-set** — filters and rate limits applied to the ready set — **not reasons to restore a global queue**. Each constrains *which* leaves get an implementer *now*; none reintroduces a total order or a privileged "next ticket."

### Residual recommendations (each residual)

| Residual | Recommendation |
|----------|----------------|
| Instructional-only auto-spawn (T155) | **Implement under T254.1** when factory Build opens — daemon enforces worker-or-park-reason |
| No hard spawn cap | **T254.6** (factory posture) + optional cap config; **defer** until owner wants thrash feel |
| Worktree isolation default | **T254.2** — do not invent Beads refinery |
| Molecules / step discipline | **T254.3** — ordered **children of one target**, still ready-set at leaf level |
| Town mail | **T254.4** — Jevons structured notices, not Beads mail |
| Patrol / GUPP | **T254.5** — deepen existing recovery |
| Ranking as "the answer" | **Already sufficient** as optional focus; fix **language** (bullseye-po) |
| Beads dual-write for continuity | **Out of scope** — map gaps to T254 family only |

Recommendation categories used: **implement under T254.*** / **new leaf** / **already sufficient** / **defer**. No residual requires a **new leaf**: every gap maps to an existing T254.1–T254.6 child, is already sufficient, or is deferred/out of scope — filing new Build leaves under T262 stays owner-gated (§7).

---

## 5. T262.3 — Cross-product ownership table

| Concern (Beads-ish or queue-ish) | Bullseye ledger | Jevons factory | Neither |
|----------------------------------|-----------------|----------------|---------|
| Desired state, depends_on, frontier membership | **Own** | Read via open/frontier API | — |
| "Next ticket" total order | **Must not claim** | Must not invent global queue | — |
| Focus ranking (fanout, portfolio WSJF) | Optional policy signal | May display; must not serialize spawn by default | — |
| Assign / owned-elsewhere | **Own** (`assign`/`unassign`) | Honor exclusion when spawning | — |
| set_aside / park / design-gated tags | **Own** status + tags/context | Filter spawn (T155) | — |
| Worker-per-leaf spawn | — | **Own** (T155/T193/T254.1) | — |
| Engagement overlay, stop, exclusive engage | — | **Own** (T198/T222) | — |
| Worktrees + integrator | — | **Own** (T254.2) | — |
| Ordered plan steps on a target | Optional child graph | Claim/resume (T254.3) | — |
| Structured finish/block/needs-design inbox | — | **Own** (T254.4) | — |
| Stuck/idle patrol | — | **Own** (T254.5, T236) | — |
| Factory posture / max parallel consume | — | **Own** (T254.6) | — |
| Dual-write Beads ledger | — | — | **Out of scope** |
| Gas Town formula TOML day-one | — | — | **Out of scope** (T254 residual) |
| Owner taste / design accept | Records gate tags | Does not auto-spawn design-gated | Owner |

### Bullseye-side candidates (brief **bullseye-po**; do not parent under jevons-po)

1. **Docs/tool copy:** reword `bullseye_convergence` and agents-guide "next most-valuable" → **ready-set focus recommendation** (single-agent / human bottleneck). Keep parallel-frontier recommendation path first-class.
2. **UX copy:** any CLI/MCP summary that says "the next ticket" → "recommended focus among ready leaves" / list the set.
3. **assign/unassign:** keep as ownership exclusion (already correct); docs should say exclusion ≠ queue head.
4. **No** Beads dual-write; **no** factory spawn inside bullseye.

### Jevons-side candidates

1. **Doctrine inject** (T262.1 land): ready-set language in persona, AGENTS, fleet brief.
2. **T254.1–T254.6** remain the factory Build leaves — **parked until owner opens Build after T262.4**.
3. Spawn caps / factory posture / worktrees / inbox = T254.*, not new T262 Build children unless owner wants a thinner slice.

### Out of scope

- Beads or Gas Town as a second ledger.
- Unparking T254 mass-implementation from this packet alone.
- Claiming infinite tokens or removing design/needs-owner gates.
- Changing bullseye graph math (frontier definition stays dependency-complete leaves).

---

## 6. Disposition table (oracle-friendly)

Legend: **ship-J** = Jevons Build; **ship-B** = bullseye product/docs; **covered** = cite existing; **defer** = later; **oos** = out of scope.

| Item | Disposition | Cite / note |
|------|-------------|-------------|
| Ready-set doctrine prose | **ship-J** (docs) | This file + post-accept inject (T262.1 residual) |
| Anti "next ticket" in agent load path | **ship-J** (docs) | persona / AGENTS / FleetStandingBrief |
| Convergence "next most-valuable" wording | **ship-B** | bullseye-po: tools.rs, agents-guide, /cv relay copy |
| Worker per frontier leaf (instructional) | **covered** | T155, T193 |
| Worker per leaf (daemon enforce) | **defer** → **T254.1** | Park until T262.4 + owner opens factory Build |
| Engagement overlay + exclusive engage | **covered** | T198, T222 |
| Frontier UI table | **covered** | T131 |
| Capacity / factory thrash posture | **defer** → **T254.6** | |
| Worktree multi-worker | **defer** → **T254.2** | |
| Ordered steps / molecules | **defer** → **T254.3** | |
| Structured fleet inbox | **defer** → **T254.4** | |
| Stuck recovery productization | **defer** → **T254.5** | builds on T236/T244 |
| Beads dual-write | **oos** | Continuity → T254 family only |
| Mass unpark T254 from this doc | **oos** until T262.4 owner accept | |
| Portfolio WSJF across repos | **covered** (bullseye) | Human attention allocator; not repo queue |

---

## 7. T262.4 draft — owner decision packet

### What we ask the owner to accept or reject

1. **Doctrine:** Frontier-as-ready-set wording in §1–§3 becomes standing product language (Jevons inject after accept).
2. **Boundary:** Bullseye owns readiness membership + optional focus ranking language fix; Jevons owns engagement/factory; **no Beads**.
3. **T254:** Stay **parked**. Prefer ordered unpark **only if** owner opens factory Build; suggested order if/when opened:

| Priority | Leaf | Why first |
|----------|------|-----------|
| 1 | **T254.1** | Closes instructional gap on ready-set consumption (daemon worker-or-park) |
| 2 | **T254.5** | Continuity / stuck path already half-built (T236) |
| 3 | **T254.4** | Structured finish visibility for set-wide fan-out |
| 4 | **T254.2** | Needed before high parallel same-repo (else thrash) |
| 5 | **T254.6** | Factory posture once 1–4 make thrash safe-ish |
| 6 | **T254.3** | Step discipline — valuable but not required for ready-set model |

4. **bullseye-po brief (non-blocking for T262.4):** language-only change to convergence/docs; no factory features in bullseye.
5. **Do not** file new Build leaves under T262 unless owner wants thinner slices than T254.*; T262 remains design/doctrine gate.

### Owner responses we will record

- **Accept all** → land T262.1 doctrine inject; achieve T262.1–T262.3 with this packet + inject SHA; T262.4 achieve only after owner chat/attestation; T254 remains parked until explicit Build open.
- **Accept doctrine only, keep T254 parked hard** → same inject; no T254 ordering commitment.
- **Reject / amend model** → revise this doc; do not inject doctrine; do not unpark T254.
- **Open factory Build on subset** → file/unpark only named T254.* children with owner list.

### Explicit non-claims

- This packet does **not** achieve T262.4.
- This packet does **not** unpark T254.
- Hermetic greps of this markdown are **necessary** for design leaf close; owner accept is **required** for T262.4 and for factory mass work.

---

## 8. Coordination notes

- **bullseye-po:** language/docs candidates in §5 — coordinate via overseer/event; **do not** parent bullseye workers under `jevons-po`.
- **Overseer:** independent gate for achieve of T262.1–T262.3 after packet lands; T262.4 waits on owner.
- **Oracle for this design deliverable:**
  - File present: `docs/design/frontier-as-ready-set.md`
  - Sections cover T262.1 doctrine, T262.2 multi-agent map, T262.3 ownership table, T262.4 draft decisions
  - Disposition table uses ship-J / ship-B / covered / defer / oos
  - Maps T155, T193, T198, T222, T254.1–T254.6
  - States no Beads dual-write; T254 stays parked pending owner

---

## 9. Finish criteria for evaluation workers (this assignment)

| Check | Evidence |
|-------|----------|
| Packet path | `docs/design/frontier-as-ready-set.md` |
| Local commit | SHA of design-only commit on local master |
| T262.4 | Draft only — **needs-owner**; no self-attested owner accept |
| T254 | No unpark; ordered recommend only |
| Ship plane | No PR, no push (T104) |
