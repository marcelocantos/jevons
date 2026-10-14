# Composer dictation and seed handling (🎯T562.4)

Status: **accepted design / oracle map**. Spec pass only.
Implementation is a follow-on under 🎯T562 once this document is achieved as
T562.4. Do not treat helper presence in `ui/src/composer/wispr.ts` as product
completion — `UserRequest.tsx` does not write `EMPTY_SEED`, does not mount
`#wispr-context`, and does not call `tidyDictationInsert` on bulk inserts.

Slice S5 of 🎯T562. Related census: 🎯T21, 🎯T80, 🎯T133, 🎯T149, 🎯T183,
🎯T192 (and the T241 / T562.7 force-send supersession of Alt+Enter pop-last).
Greenfield map shape: 🎯T31.2 (pinned when-X-expect-Y before build).

Main and sidebar share `UserRequest` / `AgentInteraction`. Every example below
applies to **both** densities unless a density is named. A main-only fix is a
fail.

---

## 1. Census of the vanilla behaviour (what the React product must absorb)

| Id | Status in ledger | Census (react-fidelity map) | Vanilla intent (owner-facing) | React today (2026-10-14 read) |
|---|---|---|---|---|
| **T21** | achieved (vanilla) | composer / **missing** | Wispr Flow into an empty or non-empty Jevons composer yields full sentences (capital + terminal punctuation), matching Claude CLI. Preferred path: live `#wispr-context` via `aria-describedby` with sentence-shaped recent-transcript context; optional empty-seed fallback stripped before wire. | No `#wispr-context`. Hint is a CSS overlay (not a placeholder) so a11y does not treat "Message…" as draft. No live STT context region. |
| **T80** | achieved (vanilla) | composer / **missing** | First bulk paste/insert that looks like an unpunctuated utterance is lightly tidied: trim, leading capital, trailing `.` if no `.?!`. Mid-edit keystrokes and already-tidy multi-sentence text are left alone. | `tidyDictationInsert` exists and has a pure oracle; **no insert path calls it**. |
| **T133** | achieved (vanilla) | composer / missing (map row "Empty-composer Wispr seed…") | `EMPTY_SEED` (`\u200B.\u200B`) stays in an empty field so Wispr document-mode still fires; seed is invisible (`composer-seed-only` → transparent text + caret colour); real draft is fully visible; wire strip removes seed; backspace on seed-only clears the seed in one action. | Helpers `EMPTY_SEED`, `isSeedOnly`, `needsSeedOnlyClass`, `stripSeed`, `seedPrefixLen` exist. **Nothing writes the seed into the textarea.** No `composer-seed-only` class toggle. Draft store starts as `''`. |
| **T149** | achieved (vanilla) | composer / code_supported | Home/End (and equivalent field-end chords) move the caret on **effective** content: seed-only collapses to the insert point after the seed; seed+text Home skips the seed. | `composerCaret.ts` accepts `SeedCaretOpts`, but `applyComposerHomeEnd` is called **without** seed opts from `UserRequest`. Harmless while no seed is written. |
| **T183** | achieved (vanilla) | composer / code_supported | After hard/soft reload, a restored non-empty draft is immediately visible (correct colour/contrast). User must not need an edit keystroke. Root cause class: `composer-seed-only` left on when real text is present (`color: transparent`). | Drafts live in zustand + `localStorage` (`useDrafts`). Remount restores value. Hermetic T183 oracle already asserts restored draft text. **No seed class exists yet**, so the transparent-class half of T183 is latent until seed wiring returns. |
| **T192** | achieved (vanilla) | composer / partial | Seed-only / seed-shaped residue (`.`, ZWSP-only, partial thrash) counts as **empty** for empty-composer controls. Vanilla Alt+Enter was pop-last; React supersedes that with T241 force-send. | `isEffectivelyEmpty` already drives `hasRealDraft` → `composerEmpty` for `classifyEnterAction`. Force-send / `send_queue_now` already treat seed-shaped residue as empty (T562.7). Good foundation; still needs seed **write** + wire strip if EMPTY_SEED returns. |

**Supersessions to honour (not re-litigate):**

- 🎯T241 / 🎯T562.7: Alt+Enter is force-send of the draft, else focused queued item, else queue head — **never** pop-last of transcript history.
- Placeholder text must never be a native `placeholder=` attribute (Wispr/VoiceOver read it as typed content). The overlay `.composer-hint` pattern stays.

---

## 2. Product goals (desired state after the implement slice)

1. **Dictation quality (T21):** Speaking into an empty main or sidebar composer yields a capitalised, punctuated sentence. Speaking into a composer that already has a real draft appends without destroying the draft and without double-tidying mid-edit text.
2. **Bulk insert tidy (T80):** A first bulk insert into an effectively-empty composer is lightly tidied; keystroke-by-keystroke mid-edit is not.
3. **Seed policy (T133 + decision below):** Either (a) EMPTY_SEED is written and fully hidden while seed-only, or (b) EMPTY_SEED is deliberately retired because `#wispr-context` alone is proven sufficient. The choice is **not silent**: §3 pins it.
4. **Visibility (T183):** Restored drafts never paint transparent. Seed-only never paints a visible full stop. Switching agent, density, or hard-reloading never leaves the wrong paint state.
5. **Empty controls (T192 + T241):** Seed-only and seed-shaped residue behave as empty for Enter / Alt+Enter / Send enabledness, consistently in main and sidebar.
6. **Wire honesty:** Anything sent to the agent has seed characters stripped; real user `.?!` is preserved.
7. **One composer:** Main (`#input`) and sidebar (`#agent-inspect-input`) share one code path.

---

## 3. Design decisions (pinned before build)

### D1. `#wispr-context` is the primary Wispr document-mode path (T21)

- Mount a live region `#wispr-context` (visually hidden is fine) and set
  `aria-describedby` on **both** composer textareas to it.
- Contents: one short **sentence-shaped** string derived from the most recent
  owner-visible transcript context for the selected agent (last sealed
  assistant sentence, else last owner sentence, else a fixed grammatical
  seed sentence such as `"The owner is dictating a message to the agent."`).
- Update the region when the selected agent changes and when a new sealed
  turn lands. Do not dump the whole transcript.
- Forbidden: client-side "grammar fixer" LLMs or third-party rewrite APIs.
  Tidy is the local pure function only.

### D2. EMPTY_SEED is a **gated fallback**, not the default write

Vanilla needed `EMPTY_SEED` when a11y context alone was insufficient for some
Wispr builds. React already avoided the placeholder trap with `.composer-hint`.

**Default for the implement slice:**

1. Ship `#wispr-context` + bulk-insert tidy **without** writing `EMPTY_SEED`.
2. Keep the helpers and the load-bearing seed examples below as the contract
   for the fallback path.
3. Turn EMPTY_SEED **on** only if a live Wispr smoke (empty field, dictate a
   question) fails to capitalise / punctuate after `#wispr-context` is live.
   That flip is a one-line policy (`SEED_FALLBACK = true`) plus the wiring
   already demanded by the examples in §4.2 — not a redesign.

Rationale: writing seed without the full T133/T149/T183 class machine is how
vanilla regressed (visible dot, transparent restored drafts, Alt+Enter noop).
Prefer the safer primary path; keep the fallback fully specified so it is not
improvised under pressure.

### D3. When EMPTY_SEED is on, these invariants are mandatory

- Empty composer value is exactly one leading `EMPTY_SEED` (or becomes one on
  ensure). Never stack seeds.
- CSS class `composer-seed-only` (or equivalent) applies **iff**
  `needsSeedOnlyClass(value)` is true. Text colour transparent; caret colour
  remains visible.
- Any path that sets the textarea value (draft restore, agent switch, queue
  Edit, history recall leave, send clear, pageshow) **re-syncs** the class in
  the same turn (T183).
- `prepareWireText` / send path strips seed via `stripSeed` +
  `isEffectivelyEmpty` before mux send. Real punctuation stays.
- Backspace/delete on seed-only clears to `''` (or re-ensure seed per policy)
  in one action — no visible `.` flash loop.
- Home/End use `seedPrefixLen` / `isSeedOnly` opts (T149).
- `isEffectivelyEmpty` remains the single emptiness predicate for chords and
  Send enablement (T192 / T241 / T562.7).

### D4. Bulk tidy trigger (T80)

Call `tidyDictationInsert` only when **all** of:

- the insert is a bulk/document insert (paste of multi-char text, or an
  `beforeinput` / `input` with `inputType` in
  `{insertFromPaste, insertFromDrop, insertReplacementText, insertFromYank}`
  or a single `insertText` whose data length is ≫ 1 and the field was
  effectively empty before the insert), **and**
- the composer was `isEffectivelyEmpty` **before** the insert (seed-only
  counts as empty), **and**
- the inserted text has no internal sentence boundary already tidy enough
  that `tidyDictationInsert` is a no-op (the pure function already returns
  early for mixed-case + existing `.?!`).

Do **not** tidy:

- ordinary keystrokes (`insertText` of length 1),
- IME composition,
- inserts into a field that already holds a real draft (append as-is; the
  leading capital of a continuation mid-paragraph is the owner's problem,
  not the composer's),
- image paste (existing image path).

### D5. Draft recovery vs seed (T183)

Order on boot / agent select / pageshow:

1. Restore persisted draft for the agent (including `''`).
2. If SEED_FALLBACK and draft is effectively empty → ensure seed.
3. Else if draft is real → ensure **no** seed class and no leading seed
   prefix left over from a previous session thrash.
4. Paint. Never leave `composer-seed-only` on a non-empty visible draft.

### D6. Main and sidebar

One component, one policy. Oracles that mount `UserRequest` must run once in
comfortable and once in compact (or assert the shared module rather than a
main-only DOM id). `#input` vs `#agent-inspect-input` are id aliases only.

---

## 4. Load-bearing when-X-expect-Y examples (pinned oracles)

These are the examples an implementer must make true. Prefer hermetic unit /
RTL tests first; live Wispr is class-3 residual smoke where noted.

### 4.1 Dictation context and tidy (T21 / T80)

| # | When | Expect |
|---|---|---|
| E1 | Main composer is empty (no seed or seed-only). `#wispr-context` is mounted and referenced by `aria-describedby` on `#input`. | The region's text is non-empty, ends with sentence punctuation or is a known grammatical seed sentence, and does not equal the overlay hint "Message…". |
| E2 | Sidebar composer is empty for agent `A`. | `#agent-inspect-input` also points at the same `#wispr-context` (or a per-pane twin with the same sentence-shaped policy for `A`). |
| E3 | Selected agent switches from `A` to `B` while both composers exist. | Context region content updates to `B`'s recent transcript sentence (or the fixed fallback), not `A`'s. |
| E4 | Effectively-empty composer receives bulk insert `"  hello there  "` (paste or insertReplacementText). | Field value becomes `"Hello there."` (trim + capital + period). |
| E5 | Effectively-empty composer receives bulk insert `"Already done."`. | Value stays `"Already done."` (no double period, no case thrash). |
| E6 | Effectively-empty composer receives bulk insert `"what time is it"`. | Value becomes `"What time is it."` **or**, if a future STT path supplies `?` intent, `"What time is it?"`. Minimum bar is capital + some terminal punctuation; do not invent `?` from heuristics beyond T80's `.` default unless a separate accepted rule says so. |
| E7 | Composer already holds `"Notes:\n"` (real draft). Owner pastes `"more bullets"`. | Value is `"Notes:\nmore bullets"` (or caret-local paste). **No** forced leading capital on the paste and **no** trailing period forced onto the whole field. |
| E8 | Owner types character-by-character `h` `i` into an empty field. | Value tracks keystrokes (`h`, `hi`) without a delayed tidy rewriting to `Hi.`. |
| E9 | Image is pasted into the composer. | Existing image chip / upload path runs; tidy is not applied to binary paste. |

### 4.2 Seed-only text (T133 / T192 / T149) — required if SEED_FALLBACK, still unit-tested on helpers always

| # | When | Expect |
|---|---|---|
| E10 | `isEffectivelyEmpty("")`, `isEffectivelyEmpty("   ")`, `isEffectivelyEmpty(EMPTY_SEED)`, `isEffectivelyEmpty("\u200B")`, `isEffectivelyEmpty(".")`, `isEffectivelyEmpty(EMPTY_SEED + "  ")`. | All `true`. |
| E11 | `isEffectivelyEmpty("hi")`, `isEffectivelyEmpty(EMPTY_SEED + "hi")`, `isEffectivelyEmpty("?")`. | All `false`. |
| E12 | SEED_FALLBACK on; empty agent draft mounts. | Textarea value is `EMPTY_SEED` (or equivalent); computed style has transparent text colour **or** class `composer-seed-only`; caret remains visible; overlay hint may still show because effective emptiness is true. |
| E13 | SEED_FALLBACK on; seed-only field; owner types `H`. | Class `composer-seed-only` clears in the same update; `H` (after seed strip or append-after-seed per implementation) is visible at normal contrast. |
| E14 | SEED_FALLBACK on; seed-only field; Backspace once. | No visible full-stop flash loop; field becomes effectively empty again under policy (re-seed or `''` + hint) without a stuck visible `.`. |
| E15 | SEED_FALLBACK on; seed-only; Home then End. | Caret stays on the effective insert point (after seed); Home does not land inside ZWSP in a way that blocks the next insert. |
| E16 | SEED_FALLBACK on; value `EMPTY_SEED + "hello"`; Home. | Caret at start of `hello`, not before the seed. End moves to after `o`. |
| E17 | SEED_FALLBACK off (default). | Empty field value is `''`; hint overlay shows; no transparent text class; E10–E11 helpers still hold for residue that might arrive from paste thrash. |

### 4.3 Draft recovery (T183)

| # | When | Expect |
|---|---|---|
| E18 | `localStorage` holds draft `"restored draft"` for `jevons`; cold mount of main `UserRequest`. | `#input.value === "restored draft"` immediately; colour is normal (not transparent); no edit keystroke required. *(Already hermetic today — must not regress when seed class lands.)* |
| E19 | Same as E18 in compact density for agent `jv-x`. | `#agent-inspect-input.value === "restored draft"` with normal colour. |
| E20 | SEED_FALLBACK on; persisted draft is real text; a stale session left `composer-seed-only` logic armed. | After restore path, `needsSeedOnlyClass(value)` is false and the DOM class is absent. |
| E21 | SEED_FALLBACK on; persisted draft is `''` or seed-only residue. | After restore + ensure, field is seed-only under E12 — never a visible orphan `.` without the hide class. |
| E22 | Owner has typed a draft, hard-reloads the cockpit. | Draft text visible in both the previously focused density and after switching to the other density for the same agent. |

### 4.4 Empty controls and send wire (T192 / T241 / T562.7)

| # | When | Expect |
|---|---|---|
| E23 | Composer seed-only or `''`; queue empty; Alt+Enter. | `noop` (no pop-last, no send). |
| E24 | Composer seed-only or `''`; queue has head `"held"`; Alt+Enter. | `send_queue_now` of head (or focused item) with `FORCE_SEND_MODE`. |
| E25 | Composer has real draft `"ship it"`; Alt+Enter. | `force_send` of the draft; queue untouched. |
| E26 | Composer seed-only; plain Enter (desktop). | `noop`; busy turn not interrupted. |
| E27 | Composer shows seed-only; Send button. | Disabled (unless pending images / uploads change enablement under existing image rules). |
| E28 | SEED_FALLBACK on; field is `EMPTY_SEED + "Hello."`; owner sends. | Wire payload is `"Hello."` with **no** ZWSP/seed prefix. |
| E29 | Field is only seed-shaped residue `"."` / ZWSP; owner somehow triggers send. | No mux send of `"."` alone; treat as empty (same as Send disabled / submit early-return). |
| E30 | Queue Edit pulls text into a seed-only composer. | Resulting draft is the queue text as real visible draft (`isEffectivelyEmpty` false); seed class off. *(AgentInteraction already uses `isEffectivelyEmpty` when prepending Edit text.)* |

### 4.5 `#wispr-context` recovery and consistency

| # | When | Expect |
|---|---|---|
| E31 | Transcript gains a new sealed assistant sentence ending in `.` / `?` / `!`. | `#wispr-context` text updates to that sentence (truncated if huge) before the next dictation. |
| E32 | Transcript is empty (new seat). | Context falls back to the fixed grammatical sentence; still non-empty. |
| E33 | Composer unmount/remount (agent tab thrash). | `aria-describedby` still resolves to a live context node; no broken id. |

---

## 5. Explicit non-goals / out of scope for the implement slice

- Reintroducing Alt+Enter pop-last (superseded by T241).
- Native `placeholder=` attribute for the invitation text.
- Provider-side grammar correction or external STT pipeline changes (T21 is
  composer a11y/context, not Voice Agent Builder).
- Pixel-identical vanilla CSS beyond the visibility/caret rules above.
- Rewind/history (T562.6) and send-ack (T562.5) — separate slices.
- Claiming live Wispr parity from hermetics alone (🎯T552): after implement,
  residual owner smoke is: empty main composer, dictate a question, confirm
  capital + terminal punctuation, hard-reload with a half-typed draft and
  confirm visibility.

---

## 6. Suggested implement order (after this spec is achieved)

1. Hermetic expansion: E10–E11 always; E4–E8 pure + RTL bulk-insert; E18 regression.
2. Mount `#wispr-context` + `aria-describedby` (E1–E3, E31–E33).
3. Wire bulk-insert tidy on empty field (E4–E9) in `UserRequest` for both densities.
4. Live Wispr smoke on development `:13705`.
5. Only if smoke fails capital/punct on empty field: enable SEED_FALLBACK and
   complete E12–E17, E20–E21, E28–E30 with class sync on every value path.
6. Caret opts plumbing (E15–E16) when seed is on.
7. `bin/gate` hermetics + hard-reload visual prose (🎯T493.1) on main and sidebar.

---

## 7. Mapping back to T562.4 acceptance

> Read the T80/T183/T192 census rows and pin load-bearing when-X-expect-Y
> examples for seed-only text, dictation append and recovery and
> `#wispr-context`, consistently in main and sidebar (`wispr.ts` helpers
> exist but `UserRequest.tsx` never writes `EMPTY_SEED`); no implementation
> until the examples are pinned (T31.2).

| Acceptance fragment | Where pinned |
|---|---|
| Census T80 / T183 / T192 (plus T21 / T133 / T149) | §1 |
| Seed-only examples | §4.2 E10–E17 |
| Dictation append | §4.1 E4–E8 |
| Recovery | §4.3 E18–E22 |
| `#wispr-context` | §3 D1, §4.1 E1–E3, §4.5 E31–E33 |
| Main + sidebar | §3 D6, E2, E19, E22 |
| Helpers exist / no EMPTY_SEED write yet | §1 React-today column; §3 D2 |
| No implementation in this pass | This document only; §6 is ordered backlog |

Fuzzy residue (still open after implement, not blockers for **this** spec achieve):

- Whether any current Wispr build on the owner's machine still needs EMPTY_SEED
  after `#wispr-context` (decided by live smoke, D2).
- Exact transcript sentence picker when the latest assistant turn is a tool
  dump (prefer last prose sentence; details left to implementer with E31).

---

## 8. Code anchors (read-only pointers for the implementer)

- `ui/src/composer/wispr.ts` — `EMPTY_SEED`, `tidyDictationInsert`,
  `isEffectivelyEmpty`, `isSeedOnly`, `needsSeedOnlyClass`, `stripSeed`,
  `seedPrefixLen`.
- `ui/src/components/UserRequest.tsx` — shared main/sidebar composer; hint
  overlay; uses `isEffectivelyEmpty` for `hasRealDraft`; **no** seed write,
  **no** tidy on insert, **no** `#wispr-context`.
- `ui/src/keys/composerEnter.ts` — T241 / T562.7 force-send policy.
- `ui/src/keys/composerCaret.ts` — seed-aware bounds ready, opts unused.
- `ui/src/oracle/families/composer-chrome.test.ts` — existing T80 pure + T183
  restore oracles to extend, not replace.
