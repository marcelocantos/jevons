# Collapse / expand UI polish (🎯T106)

Design pad for chat bubble collapse/expand chrome — not the tallness
gate alone (that already uses rendered height: full > 1.5× collapsed
preview; latest request/response stay expanded).

## Human discussion

**Owner feed-in lives here.** Paste thoughts, sketches, likes/dislikes,
reference UIs, and “must / must not” notes below. Agents treat this
section as product direction for 🎯T106; append dated notes rather than
rewriting history.

### Notes

#### 2026-08-03 — owner direction (collapse = clip, not re-preview)

Screenshots: collapsed prior bubble showed a broken partial table (missing
header row / wrong top of table); expanded same bubble showed the correct
table. Root cause of the bad look is **preview re-render** (truncate source
then re-`marked`), not “too short a max-height.”

**Must**

1. **Talk-bubble framing for assistant replies** — same bubble geometry as
   user requests, but **no fill**: transparent/backgroundless, **border only**.
2. **Collapse does not change the rendered document.** Full markdown HTML
   (tables, lists, code, …) stays mounted exactly as expanded. Collapse only
   changes the **container height** and **clips** overflow (`overflow: hidden`
   + fixed or capped height). No alternate “preview text,” no re-parse of a
   truncated source string for the collapsed state.
3. **Clip indicator without “Show more” / “Show less” text links.** Prefer a
   **bottom gradient overlay** (content flowing into a dark pocket over the
   last few pixels of the bubble).
4. **Expand/collapse affordance:** a **small tab on the bottom edge** of the
   bubble with a **chevron only** (down when collapsed → expand; up when
   expanded → collapse). Not a prose link in the flow of the message.

**Model (undecided detail, fixed principle)**

- Bubble is a real UI box: either shrink-wrap height to content when expanded,
  or a **fixed collapsed height** when collapsed; content inside is intact and
  clipped by the box.
- Tallness gate can stay height-ratio based (full vs collapsed box height);
  measurement must use the **same full render**, not a different preview DOM.

**Must not**

- Reintroduce source truncation + re-markdown as the collapsed body.
- Rely on “Show more ▾” / “Show less ▴” as the primary chrome.

---

## Current behaviour (shipped baseline) — **to replace for T106**

| Rule | Behaviour |
|------|-----------|
| Tallness | After layout: full `offsetHeight` > 1.5 × collapsed-preview height |
| Latest | Newest user **and** assistant bubbles stay expanded when tall (T66) |
| Prior | Auto-expanded tall bubbles collapse when they stop being latest (T77), unless manually toggled |
| Control | Text button `.msg-expand`: “Show more ▾” / “Show less ▴” |
| Preview | ~14 lines assistant / ~7 lines user **source**, re-parse → **breaks tables** (owner repro 2026-08-03) |

Hermetic: `scripts/chat-ui-test/collapse-test.js` (will need update under clip model).

## Design decisions (settled 2026-08-03)

| Topic | Decision |
|-------|----------|
| Affordance | Bottom **tab + chevron only** (no Show more/less text) |
| Clip cue | **Bottom gradient** into dark pocket |
| Collapsed body | **Full render + clip**, never truncated re-parse |
| Assistant bubble | Border-only talk bubble (like user geometry, no fill) |
| Latest / T66–T77 | Semantics stay unless owner revisits |

## Remaining open (implementation detail)

1. ~~Exact collapsed max-height (px vs × line-height vs ratio of viewport).~~
   **Settled 2026-08-03 (impl):** `COLLAPSED_MAX_HEIGHT = '14rem'` (CSS
   `--collapsed-max-height`); tall when `fullH > collapsedH × 1.5`.
2. ~~Gradient height / opacity / whether tab sits on top of gradient.~~
   **Settled 2026-08-03 (impl):** `.msg-clip-fade` ~2.75rem linear fade into
   chat/`user-bg`; chevron tab below body (after fade in DOM).
3. Motion: instant height vs short CSS transition (must not break stick-to-bottom).
   *Still open — current impl is instant clip toggle.*
4. ~~Whether user bubbles get the same border-only treatment or keep filled chrome.~~
   **Settled:** user keeps filled chrome; assistant is border-only.

### 2026-08-03 — implementation note (jv-t106-clip)

Shipped clip model in `web/index.html`: full `paintBody` always; collapse =
`.msg-clipped` + body `max-height`/`overflow:hidden`; gradient +
`.msg-expand-tab` chevron only. Deleted `previewOf` / PREVIEW_LINES re-parse
path. Hermetic: `scripts/chat-ui-test/collapse-test.js`.

## Non-goals (unless discussion says otherwise)

- Changing T66/T77 semantics without explicit owner OK.
- Replacing height-ratio tallness with char/line proxies again.
- Collapsing activity-strip / tool-turn chrome (separate surfaces).

## Acceptance pointer

See bullseye **🎯T106**. Implementation lands with hermetic collapse-test
green and this note updated if direction changes.
