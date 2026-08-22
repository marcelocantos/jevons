# T537.2.7 — Vanilla cockpit holds vs React daily-driver regressions

Snapshot: 2026-08-23. Surfaces compared: vanilla GET `/` (`web/index.html`) vs React `/app/` and Vite `:5173` (`ui/src`). Daily GET `/` remains vanilla (T505). Owner declared the React driver in use.

This leaf **reports**. It does not implement T537.2.2–T537.2.6. Freeze still holds; do not lift `frontier_consume.disabled`.

Vanilla reference captures in the working tree (not committed): `old-cockpit-1440x900.png`, `leftover-*.png`, `old-cockpit-a11y.md`.

## Known leaves (do not rediscover)

| Leaf | Vanilla | React daily driver | Verdict |
|---|---|---|---|
| T537.2.1 ping / owner_health | `web/scripts/transport.js` `_startHeartbeat` on `/ws/chat` | `MuxClient` sends `{"type":"ping"}` on `/ws/mux`; `handleMuxRaw` pongs + `NoteOwnerUIHeartbeat` | **Hold** (achieved 2026-08-23, SHA `f4bef1e6`). Live: `ws://127.0.0.1:13705/ws/mux` ping→pong. Residual: React tab must reload once onto that client. |
| T537.2.2 plan-usage bars | T390 `#plan-ticker` left of `#theme-toggle` with session/weekly fills | `PlanUsageBar` mounts, but paints an empty `#plan-ticker` when windows lack `remaining_percent` (`ui/src/components/PlanUsageBar.tsx` filter + empty-chip branch) | **Regress** — already T537.2.2 |
| T537.2.3 vendor mark + version | `web/scripts/model_prefix.js` on every live row (T287/T311) | `AgentTree.tsx` badge only when `provider===claude`; Grok/Codex rows have no mark. Vanilla a11y tree shows Grok `4.5` subscript on jevons and jevons-po | **Regress** — already T537.2.3 |
| T537.2.4 paste images | T76/T224 paste → POST `/api/images` → `[image: id]` | `UserRequest.tsx` empty `#composer-images` slot; no `onPaste` / `clipboardData` | **Regress** — already T537.2.4 |
| T537.2.5 Home/End caret | T126/T149 `composer_keys.js` `selectionAfterHomeEnd` + preventDefault | `useCockpitKeys` handles Tab and PageUp/Down only. Home/End fall through (macOS scrolls transcript). `pageScroll.test.ts` asserts Home delta 0 | **Regress** — already T537.2.5 |
| T537.2.6 frontier ▶ kickoff | T182 `playFrontierTarget` POST `/api/agents/{po}/send` | `FrontierTable.tsx` `ft-play-btn` has no `onClick` | **Regress** — already T537.2.6 |

## Other achieved vanilla cockpit/UX (hold or regress)

| Vanilla target | Verdict | Evidence |
|---|---|---|
| Theme light/dark/system | **Hold** | `#theme-toggle` in `ui/src/App.tsx`; vanilla a11y tree ☼ / ◐ / ☾ |
| Tab cycle main ↔ sidebar composers | **Hold** | `useCockpitKeys` + `planComposerTabCycle` (commit `889e8f95`) |
| PageUp/Down scroll transcript | **Hold** | `useCockpitKeys` + `pageScrollDelta` |
| Nested fleet tree, Closed/Open viz, Personal/Squz | **Hold** | `AgentTree.tsx`; vanilla screenshot + a11y tree |
| Frontier-first tabs then Transcript then Coach | **Hold** | `SidebarPanel.tsx`; `t537_2_product_path.test.ts` |
| ⋯ n steps fold (not NOTE rows) | **Hold** (code) | `ui/src/conversation/display.ts` lifts `foldDisplayEvent`; `agent_note` is a turn-slot item. Residual: owner hard-reload of a live Grok stream |
| Round user/assistant bubbles, markdown, mermaid | **Partial hold** | React paints bubbles; T537.1.1 stream coalesce achieved in ledger; T537.1.2 (echo pair + late hydrate) still identified |
| Composer auto-grow / send | **Hold** (basic) | textarea + Enter-to-send in `UserRequest.tsx` |
| T224 image thumbs in owner bubble | **Regress** | no paste path → covered by T537.2.4 |
| T149 Wispr seed Home skip | **Regress** | covered by T537.2.5 |
| T182 busy-409 / T278 spinning ▶ | **Regress** | covered by T537.2.6 (control is a no-op) |
| Jump-to-bottom (End when composer not focused) | **Unknown / likely regress** | `#jump-bottom` exists, `hidden`; no End handler in `useCockpitKeys`. Not filed separately — belongs on T537.2.5 |

## Already-filed follow-on (not rediscovered)

- **T537.1.2** — duplicate `[user]` owner bubbles and late assistant paint after mux hydrate.
- **T390.1.5.2** — Fable monthly spend window distinct from weekly/session (sibling of T537.2.2, not a React paint bug).

## Unknown regressions worth a leaf if they survive the known set

None filed this turn. Jump-to-bottom End is noted under T537.2.5 rather than a new id. T67 Shift-Return list-continue and T370 fleet-cycle chords are still `identified` on vanilla itself, so they are not React regressions of achieved work.

## Residual

- GET `/` is still vanilla. Owner-visible React is `/app/` and `:5173`.
- Pixel leftover captures are vanilla (red overlay on `leftover-full.png`); they are the old-cockpit census, not a React screenshot oracle (T537.2 Playwright pixel compare remains open).
- No Ship. Local master T104.
