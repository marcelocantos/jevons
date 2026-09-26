# OMP / Claudia repeat-storm (T219 + T65)

**Date:** 2026-09-26  
**Status:** open — handed off from an HMS Cursor session that received this thread by mistake  
**Continue here.** Do not treat the HMS Ralph/T14 unpacker work as the cause.

Screenshots live beside this file:

- `2026-09-26-omp-repeat-storm/01-ralph-t14-healthy-unpacker.jpg`
- `2026-09-26-omp-repeat-storm/02-t65-repeat-wall.jpg`
- `2026-09-26-omp-repeat-storm/03-t65-multi-bubble-eolos.jpg`

OMP is oh-my-pi (`omp` / `@oh-my-pi/pi-ai` / `pi-agent-core`), newly wired as a Claudia backend. Design notes: [`docs/design/oh-my-pi-claudia-backend.md`](../design/oh-my-pi-claudia-backend.md). `pi-ai` `stream()` yields `text_*`, `thinking_*`, `toolcall_*`, `done`, `error`.

## What the owner saw

Claudia chat (`marcelocantos/claudia`) after a user line about **T219 / T65**, interrupt, and spawn/no-product.

1. A **storm of T219 chatter** above the T65 mess. The owner could not screenshot it because the view **keeps snapping to the bottom** (separate UI bug). The T219 storm **survives reload**, so it is in the persisted transcript, not a paint artifact. Find it in the session JSONL; do not ask the owner to scroll.

2. **Abort** (`aborted`, “1 step”).

3. Several **short assistant bubbles**, each restarting the same plan: inspect workspace, T65 brief, directory listing, git status.

4. **`<|eolos|>` leaked into visible text** — mid-sentence and as a terminator. Example shapes from the capture: `…notes.<|eolos|>` then more “I’ll…”; a later bubble ends `…tree.<|eolos|>`.

5. One **runaway bubble**: the same “I’ll inspect / I’ll start / I’ll search T65…” plan, then collapse into `I'll start. I'll list. I'll search. I'll git.`

6. After that wall, **more short bubbles** with the same opener. This is many turns, not one fat generation.

![T65 wall of repeated I'll-inspect sentences](2026-09-26-omp-repeat-storm/02-t65-repeat-wall.jpg)

![Abort, multiple restating bubbles, leaked eolos, then the wall](2026-09-26-omp-repeat-storm/03-t65-multi-bubble-eolos.jpg)

## How to tell harness vs model

The painted bubble cannot decide it. Consecutive **raw** `text_*` (or equivalent session events) can.

| Next raw event | Likely cause |
|---|---|
| Byte-identical to the last, or same stream/message id replayed | Harness / OMP duplicate |
| Full text **starts with** the last event’s full text (growing snapshot) **and** the UI **appends** the whole string | Harness. Display length goes roughly quadratic; the model only wrote the longest copy. |
| New suffix is more “I’ll inspect…” that is **not** a prefix of what you already have | Model generated another sentence (can still be a loop) |
| Output-token usage stays small while the bubble is huge | Harness |
| Usage climbs in step with the wall | Model, or a loop that is actually calling the API again |

`<|eolos|>` in the transcript is a **stop/end token leak**, not prose. If OMP does not treat it as a stop — or treats it as “turn over, start another” — you get new bubbles that restate the plan, sometimes with the token still visible.

Three layers, not one:

- **Leaked `<|eolos|>` / turn split** — check OMP stop sequences and whether that token closes a message.
- **Many short bubbles with the same opener** — false turn split, or the agent really re-plans after abort/interrupt.
- **The one collapsed wall** — still snapshot-append vs degeneration until you diff raw events.

The T219 storm (persisted, above this, owner could not capture) is in scope. The scroll-to-bottom snap is a second bug; fix or work around it so a long session is inspectable.

## Where to look

Do not stop at the pretty chat. The mess is on disk after reload.

Likely trees (today’s Claudia grok-homes were under `~/.local/state/claudia/grok-homes/`):

- Session `chat_history.jsonl`, `updates.jsonl`, `events.jsonl` for the Claudia workspace that showed `marcelocantos/claudia`
- Jevons: `~/.jevons/chatlog/jevons.jsonl`, `~/.jevons/logs/events.jsonl`, `~/.jevons/spool/events-2026-09-26.log`
- OMP: `~/.omp/` (empty on the machine that first looked; the live home may be a Claudia grok-home, not `~/.omp`)

Search for `T219`, `T65`, `<|eolos|>`, and repeated `I'll inspect`.

For each assistant/text event in the T219 storm and the T65 run: record type, id, timestamp, text length, whether text N+1 startswith text N, and whether the UI stored one row or N appended copies.

## Out of scope (wrong harness)

A parallel HMS session was unpacking Cursor `agent --output-format stream-json` for **Ralph T14** (desktop visual parity). That unpacker had its own bugs (word-per-line assistant deltas, raw `GetMcpTools` JSON). Those were fixed in `~/.local/bin/ralph` (dotfiles), not Jevons.

![Ralph T14 unpacker looking healthy — not this incident](2026-09-26-omp-repeat-storm/01-ralph-t14-healthy-unpacker.jpg)

Do not spend time on Ralph unless you need a contrast case for snapshot-vs-delta rendering.

## Suggested next steps

1. Identify the exact session files for the Claudia view that still shows the T219 storm after reload.
2. Quantify T219: event count, unique vs repeated bodies, timestamps, abort/interrupt edges.
3. Classify `<|eolos|>`: which layer emits it (model vocab vs OMP vs Claudia), and whether it should stop the turn.
4. Apply the snapshot/duplicate table to the fat T65 bubble.
5. File or fix the scroll-to-bottom snap so a long persisted thread can be read from the top.
