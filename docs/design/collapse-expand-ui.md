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

#### 2026-08-03 (later) — pocket metaphor polish (owner screenshots)

Post-`ff7c5ee` smoke. Collapsed bubble must feel like a **pocket** the content
is slotted into — not a body with a floating control below.

**Must**

1. **Tab orientation / attachment:** tab **comes out of the bottom of the
   container border**, hard flush with the outer border (part of the border
   language), not floating mid-gap under the content. Current tab reads
   upside-down / detached — fix so it is a bottom-edge pocket tab.
2. **Chevron sense:** match pocket tab (protruding from bottom edge). Not
   “hanging from content above.”
3. **Fade = pocket darken, not text dissolve:** bottom overlay is a
   **slightly darker scrim** (fade-to-dark / into the pocket), flush to the
   **inner bottom of the border**. Not a fade-to-transparent of the text into
   the page `--bg`, and not floating above the border with a gap.
4. **Timestamp outside the box:** place `.msg-time` **outside / slightly
   under** the bubble border so the border can sit hard against clipped
   content. Time must not compete with the pocket fade/tab inside the box.
5. **Tab only when warranted:** **no expand tab on non-tall messages.**
   Tiny bubbles must not grow a chevron. When expanded (e.g. latest tall),
   prefer tab only when useful (collapsed always if tall; expanded: optional
   collapse — if shown, still flush bottom-border tab, not always-on for
   short content).
6. **Collapsed = pocket:** content flows under the dark pocket edge;
   container looks like content is slotted into a pocket.

**Ship baseline still wrong until this lands** (`ff7c5ee` clip model kept;
chrome/layout only).

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

### 2026-08-03 later — pocket polish (post-smoke)

Chrome-only on top of clip model:

| Piece | Polish |
|-------|--------|
| Fade | Absolute bottom of `.msg`; `transparent → rgba(0,0,0,0.45)` scrim; not `var(--bg)` dissolve |
| Tab | Absolute `top:100%` center, `margin-top:-1px` merge with outer bottom border; chevron up when collapsed / down when expanded |
| Time | Absolute under border (`top:100%`), corner-aligned; not inside content box |
| Clipped pad | `padding-bottom: 0` so border butts clip edge |
| Short | No tab / no fade unless `measureCollapse` says tall |

### 2026-08-03 — residual chrome (tab-in + short scrim)

Owner smoke after pocket polish: hang-off tab and multi-line tall scrim felt wrong.

| Piece | Residual polish |
|-------|-----------------|
| Fade height | `height: var(--radius)` (same token as `.msg` corner radius) — not `2.75rem` |
| Tab | Flipped inside: `bottom: -1px` + `border-bottom: none` + top radius; tongue into pocket edge, not hang-off under box |
| Chevron | Unchanged sense: up collapsed / down expanded |
| Keep | Dark scrim, tall-only tab, time outside, full-render clip |

### 2026-08-03 — scrim opacity + tab z-order

| Piece | Polish |
|-------|--------|
| Scrim max alpha | `0.45` → `0.22` (half); height still `var(--radius)`; fade-to-dark structure |
| Tab stack | `.msg-expand-tab` `z-index: 3` above `.msg-clip-fade` `z-index: 1` for **both** `.msg.user` and `.msg.jevons` |

## Non-goals (unless discussion says otherwise)

- Changing T66/T77 semantics without explicit owner OK.
- Replacing height-ratio tallness with char/line proxies again.
- Collapsing activity-strip / tool-turn chrome (separate surfaces).

## Acceptance pointer

See bullseye **🎯T106**. Implementation lands with hermetic collapse-test
green and this note updated if direction changes.
