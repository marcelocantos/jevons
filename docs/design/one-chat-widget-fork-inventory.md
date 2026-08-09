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
durability).**
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

## 6. Provenance

Inventory built by reading the tree at `master` (working tree carried
uncommitted T371 edits, noted inline). Every claim above is a verified
`file:line` at the time of writing; no line number is inferred.
