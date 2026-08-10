# 🎯T372 — One chat widget + one agent contract: fork inventory

**Status:** deliverable 1 of the T372 mission (inventory + exception
**candidates**). Nothing here is a locked exception. Section 3 is raised
**for owner review only** — per the locked doctrine, no implementer may
assume or lock an exception without an owner-signed list.

**Locked principle (owner, 2026-08-09):**

1. Main view = ONE widget: transcript history + message box + send.
2. That EXACT widget (same module, not a cousin) for main AND every agent
   transcript surface (sidebar / aside / inspect).
3. Same for APIs + backend harness — one participant model; root `jevons`
   is not a special technical class.
4. Role may differ *presentation*; differences are exceptions only, never
   default forks.
5. No exception assumed or merged without an owner-raised documented
   exception list first.

Supersedes 🎯T309.1's "two contracts are fine" residual. 🎯T371 (aside send
vanish) is a **symptom** of the forks below, not an independent bug.

---

## 1. Summary judgment

`ConversationWidget` is **nominally** shared and **materially** forked.
Both surfaces call `ConversationWidget.mount()`, but main mounts it with
`wireComposer: false` and keeps its own send, its own key handling, its own
pending/retain machinery, and its own transport. The widget is therefore a
*renderer* for main and a *renderer + composer + sender* for the sidebar.

The result is one module with two contracts running through it — which is
the exact shape the owner has rejected. Nine forks are enumerated below
across UI, API, and harness.

---

## 2. Fork inventory (file:line)

### 2.1 UI layer

**F-UI-1 — The widget is mounted with opposite wiring authority.**
- `web/index.html:8737` — main mount: `agentId: 'jevons'`,
  `density: 'comfortable'`, **`wireComposer: false`**, with the inline
  comment *"main keeps ComposerKeys + send_queue wiring"*.
- `web/index.html:8749` — inspect mount: `density: 'compact'`,
  **`wireComposer: true`**, *"widget owns key/draft/optimistic/send"*.

This single flag is the root of the fork: main opts **out** of the widget's
composer and send path. Everything in F-UI-2..4 follows from it.

**F-UI-2 — Two send implementations.**
- `web/index.html:7481` — `function send(opts)`: the main path (~200 lines).
  Owns the send queue, Wispr seed stripping, image markers, attention
  prefixes (`aside:` / `target:` / `park:` / `capture:` / `main:` /
  `pursue:`), and decision logging.
- `web/scripts/conversation_widget.js:768` — `function send()`: the sidebar
  path. Owns none of the above.

**F-UI-3 — Two pending-owner-turn implementations (same concept, different
durability).** — **CLOSED at `29e69e8`** (see §6).
- Main: `web/scripts/composer_persist.js:270` `stagePending()` +
  `:252` `savePending()` + `:231` `loadPending()`, replayed by
  `web/index.html:7333` `retainPendingOwnerTurnsVisible()`. Backed by
  **localStorage — survives reload**.
- Sidebar: `web/scripts/conversation_widget.js:296`
  `stagePendingOwnerTurn()` + `:360` `applyPendingOwnerTurns()`.
  **In-memory only — dies on reload.**

Note: this second implementation is the **in-flight 🎯T371 work**
(uncommitted at the time of writing). It moves in the right direction
(into the shared module) but main does not adopt it, so as landed it is a
*second* implementation of one contract. See §4.

**F-UI-4 — Two render/paint paths.**
- Main: `#messages` + `VirtualList` + `buildMsg`, declared a
  "ledger-allowed residual" at `web/scripts/conversation_widget.js:12`.
- Inspect: `lineSpec` / `paintBody` / `workingLabel` overrides at
  `web/index.html:8758-8773`, rendering via `renderAgentInspect`.

**F-UI-5 — Two Enter-chord contracts.**
- `web/scripts/conversation_widget.js:118` `classifyComposerKey()`:
  compact density hard-returns `send` / `newline` only (`:128-131`).
  Comfortable delegates to `ComposerKeys` for the richer chord set
  (`interrupt`, `force_send`, `send_queue_now`).

### 2.2 API layer

**F-API-1 — Two transports for the same act (owner sends a message).**
- Main: WebSocket `/ws/chat` — `internal/server/server.go:424` →
  `internal/server/chat.go:1084` `handleChat`.
- Sidebar: HTTP `POST /api/agents/{name}/send` —
  `internal/server/server.go:434` → `internal/server/agent_send.go:129`
  `handleAgentSend`.
- Client URL construction: `web/scripts/conversation_widget.js:154`
  `agentSendPath()`.

**F-API-2 — Two history/rehydrate sources.**
- Main: WS chat history frames on `/ws/chat` (journal-backed).
- Sidebar: WS `agent_transcript` frames — `web/index.html:2486` dispatch →
  `web/index.html:9094` `handleAgentTranscriptWire()`; plus HTTP residual
  `GET /api/agents/{name}/transcript` — `web/index.html:9252`
  `loadAgentTranscript()` → `internal/server/server.go:433` →
  `internal/server/agent_transcript.go:52`.

**F-API-3 — Root `jevons` is a special technical class, enforced in code.**
- `web/scripts/conversation_widget.js:180` — `buildSendRequest()` returns
  `{ ok: false, reason: 'overseer-main-only' }` when the addressee is the
  overseer.
- `web/scripts/conversation_widget.js:205` — owner-facing copy:
  *"Overseer uses main chat, not the RHS Transcript composer."*

This is the most direct contradiction of locked principle 3. The overseer
is refused by the shared widget purely for being the overseer.

### 2.3 Harness layer

**F-HARNESS-1 — Two ingress paths into the agent participant model.**
- Named agents: `internal/server/agent_send.go:66` `sendToNamedAgentAs()`.
- Overseer: `internal/server/chat.go:1084` `handleChat` — its own session,
  journal, and seal semantics.

There is no single "deliver owner turn to participant X" call that both
surfaces bottom out in; root `jevons` and worker `jv-*` are different
technical classes all the way down.

---

## 3. EXCEPTION **CANDIDATES** — owner review only (NOT locked)

Raised per locked principle 5. Each is a place where main and agent
surfaces currently differ; the question for the owner is whether the
difference is a legitimate **role/presentation** exception or a fork to be
removed. **Default assumption while unanswered: remove the fork.**

| # | Candidate | Current state | Question for owner |
|---|---|---|---|
| EC-1 | Density (compact vs comfortable) | Param + CSS only (`conversation_widget.js:31`) | Likely *not* an exception — confirm param-only is acceptable. |
| EC-2 | Enter-chord richness | Main has interrupt / force_send / send_queue_now; compact has Enter + Shift+Enter (`:128-131`) | Should agent composers get the full chord set? |
| EC-3 | Composer command prefixes (`aside:`, `target:`, `park:`, …) | Main only (`index.html:7517-7561`) | Should an agent composer accept prefixes, or is routing inherently a main-chat act? |
| EC-4 | Send queue + image attachments | Main only (`index.html:7485-7516`) | Extend to all surfaces, or main-only by role? |
| EC-5 | Draft/pending durability across reload | Main localStorage; sidebar in-memory | Owner turns to an agent — must they survive reload like main's? (Recommend: yes.) |
| EC-6 | Overseer addressable from a non-main surface | Refused (`conversation_widget.js:180`) | Principle 3 says root is not special. Confirm the refusal is deleted, and whether the overseer appears as an inspectable transcript. |
| EC-7 | VirtualList / history scale | Main-only residual (`conversation_widget.js:12`) | Fold into the widget for all surfaces, or keep as a main host param? |
| EC-8 | Role chrome (portfolio labels, 💡 aside, 🎯 target) | Per-surface | Presentation-only exception? Expected to be the *legitimate* case. |

**Recommended owner action:** rule EC-6 and EC-5 first — they are the two
that keep root `jevons` a special class and keep agent owner-turns less
durable than main's. EC-1 and EC-8 are the likely-legitimate residuals.

---

## 4. Coordination with 🎯T371 (F1–F5)

`jv-t371-aside-send-parity` is working the vanish symptom in the same
files. Its in-flight diff:

- **Right direction:** puts pending-owner-turn helpers *inside*
  `ConversationWidget` (`:296`, `:360`) and paints the owner bubble on
  **accept** rather than on transport success — which is main's 🎯T279
  contract.
- **Risk:** main still stages through `ComposerPersist`
  (`composer_persist.js:270`). Landing as-is yields **two** implementations
  of one contract — deepening the T309.1 dual-contract residual the owner
  just superseded.

**Ask for T371 F1–F5:** converge on a single pending/stage/ack/retain
contract that **main also adopts**, rather than a sidebar-local mirror of
main's behaviour. The unification test in §5 should fail if the two drift.

**Resolved.** T371's fix landed at `12c3c73`; T372 then collapsed the two
implementations at `29e69e8` (§6). The ask above is discharged — T371 keeps
its cure, and main now runs the same code rather than its own copy.

---

## 5. Oracle direction (hermetics fail if main vs agent diverge)

Per the mission: parity is the oracle, not prose. Planned hermetic shape —

1. **Same-module assertion:** both surfaces resolve to the same
   `ConversationWidget` send entry (no `wireComposer:false` escape hatch
   that routes main around it).
2. **Send/display/rehydrate parity table:** for each surface, drive
   `stage → send → history frame without the turn → assert bubble
   survives`. Main and agent must produce identical outcomes.
3. **No-special-root:** `buildSendRequest('jevons', …)` must succeed once
   EC-6 is ruled, and the parity table must include the overseer as an
   ordinary participant.
4. Any surviving difference must name an owner-signed EC number, else the
   test fails.

---

## 6. Landed so far

**`29e69e8` — F-UI-3 closed: one pending-owner-turn contract.**

`web/scripts/pending_turns.js` is now the single implementation, keyed by
**agent**, with main as simply the agent `PendingTurns.MAIN_AGENT`. Locked
principle 3 is expressed in the data model rather than in prose: the algorithm
cannot distinguish root `jevons` from a worker pane.

- `ConversationWidget`'s five pending helpers are direct **bindings** to it
  (identity, not lookalikes); ~150 lines of duplicate deleted.
- `ComposerPersist` keeps what is genuinely main's — localStorage durability
  and send-queue restore planning — and delegates stage/ack/apply. Its
  agent-free public signatures are unchanged, so `index.html` and its own
  suite are untouched and green.
- Legacy main pending (`{id, text, stagedAt}`) migrates on read.

Oracle: `web/scripts/pending_turns_test.js` (in `make test-web`). §3 is a
**parity table** — six send/display/rehydrate scenarios driven through *both*
public surfaces, deep-equalled on the owner-visible outcome. §4 asserts the
stronger invariant that the surfaces are the **same code**, greps both
adopters for re-grown local definitions, and pins script load order. It has
already caught a concurrent edit that dropped the `index.html` script tag.

**No exception was locked.** EC-5 is deliberately reduced from "two
implementations with different durability" to a **one-line choice of store per
surface** — so the owner's ruling is now a parameter, not another rewrite.

**`1244b44` — the sidebar's shadow copies deleted: alias, not lookalike.**

Every sidebar composer entry point in `agent_transcript.js` and `index.html`
carried a local *"if the widget is missing, do it myself"* fallback. Nominally
shared, materially forked — and the copies only ran when the widget failed to
load, i.e. exactly when nobody could see them. They had already drifted:

- `AgentTranscript.linesFingerprint` omitted `when`, so the host could call a
  re-timestamped line set unchanged while the widget repainted it;
- the last composer-visibility rung showed the composer **for the overseer** —
  silently deciding EC-6 in the opposite direction to the refusal at
  `conversation_widget.js:180`;
- `index.html`'s draft-empty chain named `sidebarDraftIsEmpty`, which does not
  exist, so it always fell through to a local `trim()`.

Each entry point is now a binding to the `ConversationWidget` function of the
same concept. A missing widget is a **loud throw naming the export**, never a
quiet second implementation.

Oracle: five tests in `web/scripts/agent_transcript_test.js` (in `make test`).
Three assert identity — the alias returns the widget's result verbatim, density
is a *parameter* to the one widget, absence throws. Two are **ratchets**: no
fallback branch may re-grow in `agent_transcript.js`, and `index.html` may hold
zero `typeof ConversationWidget !== 'undefined' &&` ternary guards — that guard
shape *is* the fork's tell.

No exception was locked. The density assertion pins only that the sidebar
*asks* the widget for compact (EC-1's "param-only" reading); what compact then
means for the chord set is EC-2, still open and untouched.

### What is still blocked on owner EC rulings

The remaining forks all bottom out in `wireComposer: false` (F-UI-1), and
removing that escape hatch means main adopting the widget's composer — which
is precisely EC-2 (Enter-chord richness), EC-3 (command prefixes) and EC-4
(send queue + images). Those cannot be collapsed without inventing exceptions,
so they are **parked**, per the standing default rather than resolved by
implementer judgment. EC-6 (overseer refused by the shared widget) and EC-5
remain the two highest-value rulings: they are what still keep root `jevons` a
special class and agent owner-turns less durable than main's.

---

## 7. Provenance

Inventory built by reading the tree at `master` (working tree carried
uncommitted T371 edits, noted inline). Every claim above is a verified
`file:line` at the time of writing; no line number is inferred.
