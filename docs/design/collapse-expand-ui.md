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

<!-- Owner: write freely under this heading. Example:

#### 2026-08-01 — first pass
- …
-->

_(empty — awaiting owner notes)_

---

## Current behaviour (shipped baseline)

| Rule | Behaviour |
|------|-----------|
| Tallness | After layout: full `offsetHeight` > 1.5 × collapsed-preview height |
| Latest | Newest user **and** assistant bubbles stay expanded when tall (T66) |
| Prior | Auto-expanded tall bubbles collapse when they stop being latest (T77), unless manually toggled |
| Control | Text button `.msg-expand`: “Show more ▾” / “Show less ▴” |
| Preview | ~14 lines assistant / ~7 lines user source, then re-measure height |

Hermetic: `scripts/chat-ui-test/collapse-test.js`.

## Design open questions (for discussion)

Use the **Human discussion** section to answer or override these:

1. **Affordance** — Is a trailing text button enough, or do we want a
   chevron, “⋯”, gradient fade into the cut, or a full-width bar?
2. **Collapsed framing** — Hard text cut vs max-height + fade mask on
   the full render vs first-N lines only?
3. **Discoverability** — Should non-latest tall bubbles look “incomplete”
   at a glance without hunting for the control?
4. **Latest chrome** — When latest is tall-but-expanded, still show
   “Show less”, or hide the control until scroll/hover?
5. **Motion** — Instant swap vs short height animation (careful with
   scroll-follow)?
6. **Roles** — Same chrome for user vs assistant, or softer on requests?

## Non-goals (unless discussion says otherwise)

- Changing T66/T77 semantics without explicit owner OK.
- Replacing height-ratio tallness with char/line proxies again.
- Collapsing activity-strip / tool-turn chrome (separate surfaces).

## Acceptance pointer

See bullseye **🎯T106**. Implementation lands with hermetic collapse-test
green and this note updated if direction changes.
