# Attention threads (🎯T65)

**Status:** phase-1 — prefix-first / voice-first client slice.  
**Scope:** human ↔ overseer chat UX only. Not fleet/butler threads (T16/T30/T68/T72).

## Problem

Mid-chat the owner has a side thought that must not steal the main focus.
Flat multi-session is wrong: the need is **attention management inside one
conversation**. Interaction must be **voice-heavy** — natural spoken/typed
prefixes, not a button row.

## Interaction model (binding)

Primary UI is the **composer text** (typed or dictated). A small set of
case-insensitive **command prefixes** at the start of the draft:

| Prefix | Effect |
|--------|--------|
| `aside: …` | Send `…` as not-main; track as an open attention thread; strip prefix before routing. |
| `capture: …` | Arrest `…` into a side thread **without sending** and **without leaving main focus**. |
| `park:` / `park: <title>` | Park the focused side thread, or the first open match by title substring. Local; no send. |
| `main:` / `main: …` | Return focus to main. Optional body is a normal main send. |
| `pursue: <title>` | Focus the first stack match by title substring; load its body into the composer. Local; no send. |

Rules:

- Case-insensitive; optional space before/after `:`.
- Prefix is stripped before routing / storage body.
- No button-primary Capture / Aside / Main row.
- Capture is **not** “enabled while typing” as a default action — only the
  explicit `capture:` prefix (or equivalent later) creates a side thread
  without send.
- Quiet **stack** of open/parked threads (when non-empty): click a chip to
  pursue. Return to main via `main:` or a Main stack chip.
- **No focus label chrome** (no “Focus: main” strip). Focus is implied by:
  - stack chip highlight, and
  - composer **placeholder** when on a side thread:
    `[aside: short-title] Write a message to Jevons`
    (square brackets are part of the hint). On main, placeholder is the
    normal clean prompt — no `[main]`.
- Optional later: tiny cheatsheet of recognized prefixes — **not** in phase-1.

## Model

| Concept | Meaning |
|---------|---------|
| **Main thread** | Default focus: the live overseer chat stream. |
| **Attention thread** | Captured/aside side thought: title + body, `open` or `parked`. |
| **Stack** | Visible list of open + parked attention threads. |

## Wire shape (phase-1)

Aside / pursued sends use a single-line prefix the owner can still edit:

```text
[attention:<id>|<title>]
<body>
```

Main-focus sends without `aside:` are unchanged.

## Persistence

`localStorage` for stack + drafts in phase-1. Durable server threads later
if sticky.

## Non-goals

Separate overseer ACP sessions per attention thread; fleet worker trees;
generative canvas (T29); button-primary attention controls.

## Implementation slices

| Slice | Ship |
|-------|------|
| **1 (this)** | Design note; pure prefix parser + model + hermetic tests; quiet stack + composer-placeholder focus; no command buttons / Focus: chrome. |
| **2** | Server-side durable attention threads if phase-1 sticks. |
| **3** | Parent links, merge/close, richer transcript filter; optional prefix cheatsheet. |

## Acceptance mapping

| Criterion | Phase-1 proof |
|-----------|----------------|
| Capture without losing main | `capture: …` → stack row; focus stays main; no wire send. |
| Park or pursue; both tracked | `park:` / stack click / `pursue:`; both remain in stack. |
| Mark next message not main | `aside: …` → attention wire prefix; body stripped of command. |
| Visible open-thread stack | Quiet `#attention-stack` when threads exist; no Focus: label. |
| Side-thread composer hint | Placeholder `[aside: short-title] Write a message to Jevons`; main = clean placeholder. |
| Voice-first | Prefixes work on typed or spoken composer text; no Capture/Aside/Main buttons. |
| Chat UX only | No fleet/RHS agent-tree changes. |
