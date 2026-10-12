# 🎯T540.7.1.1 — MAIN-view owner-turn order (development surface)

## Observation (read-only)

- Surface: development cockpit `http://127.0.0.1:13705/` (no message typed or sent).
- Harness: `scripts/react-ui-test/t540-main-order-dev.cjs` + confirmatory Playwright re-run.
- Seed triple from existing overseer history (owner-origin row already on disk):
  - **a (assistant PRE):** `Confirmed exact error message, consistent across all four`
  - **owner (user):** `can we use git clones instead of worktrees`
  - **b (assistant POST):** `Sent — asking jevons-po to evaluate clones vs. worktrees`
- DOM probe on `#messages [data-kind]`: assistant index &lt; user index &lt; assistant index.

### Script result (`run.log`)

```
FIRST {"rows":165,"ia":63,"iu":64,"ib":67,"ok":true}
RELOAD {"rows":161,"ia":59,"iu":60,"ib":63,"ok":true}
PASS: main view order assistant, user, assistant on 127.0.0.1:13705 first load and after hard reload
```

### Confirmatory re-run (same host, hard reload)

```
order  {"rows":156,"ia":54,"iu":55,"ib":58,"ok":true}
order2 {"rows":166,"ia":53,"iu":54,"ib":57,"ok":true}
live end after reload: atBottom=true
```

Live equals reload for the ordered run: both passes paint **assistant → user → assistant** in the **MAIN** `#messages` scroller (not only a sidebar).

## Screenshots

| File | What it shows |
|------|----------------|
| `main-first-load.png` | MAIN view scrolled to the owner triple on first load |
| `main-after-reload.png` / `main-ordered-run-after-reload.png` | Same triple after hard reload |
| `main-ordered-run.png` | Ordered-run frame (pre-reload confirm) |
| `main-live-end-after-reload.png` | Live tail after hard reload (pin to bottom) |

OCR of the ordered-run-after-reload frame (tesseract) reads, in order:

1. assistant body containing “Confirmed exact error message, consistent across all four…”
2. user body “can we use git clones instead of worktrees,”
3. a small `aborted` status line (not a second user turn)
4. assistant body “Sent — asking jevons-po to evaluate clones vs. worktrees…”

## 🎯T493.1 prose verdict

Done. Viewport screenshots of the MAIN `#messages` pane on the development surface, first load and after hard reload, plus the live end after reload.

**Ink:** packed assistant and user bubbles with readable body text — the PRE assistant, the owner turn “can we use git clones instead of worktrees”, and the POST assistant “Sent — asking jevons-po…” are all legible ink on screen (not shells, not blank turn-slots).

**Empty canvas:** none — the ordered-run frames are filled with transcript rows edge-to-edge; the live-end frame is likewise packed with recent assistant bubbles at the bottom of the scroller. No blank empty slots; the pane is full.

**Latest:** hidden/absent at the live end after hard reload (`main-live-end-after-reload.png`; OCR shows `idle` and the newest overseer bubbles, and no Latest jump control in that frame). The ordered-run frames are mid-history by design so the owner triple is on screen; the live-end frame is the Latest check.

Does this look like a normal chat transcript after a hard reload? Yes.
