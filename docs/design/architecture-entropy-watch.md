# Architecture entropy watch (🎯T516)

Status: design packet. Not built. Owner accept or park is the next gate.

## The story

A large ledger means a lot of landings. In a human shop that volume
raises entropy: modules blur, a second copy of a concern appears next to
the first, boundaries get crossed because the short path compiled, and
work slows because every next change has to understand two worlds.

Jevons already watches *behaviour* (the RSI coach), *defects in named
files* (the T357 auditor), and *whether the architecture page still
describes the built system* (T42). None of those watch the *direction of
the tree* — whether the top-level cut is still the cut, and whether this
week’s landings started to gum it up.

The product we want is not a one-shot spring clean. It is a standing
massage: cheap sensors notice the gum, a slow advanced-tier read names
the structural problem, the overseer decides, and the ledger grows a
cleanup / spring-cleaning / re-architect target *before* the owner sees
the symptom in the cockpit.

### One landing, all the way through

1. A worker lands a product fix. The short path is a second
   `growAssistant` in `foldDisplayEvent`, next to the one
   `applyLiveDisplayFrame` already had. Tests for the new fold go green.
   The old model still seals on a real owner user; the new one does not.
2. That night the **cheap sensors** fire (no LLM). The dual-implementation
   detector reports two display folds. The hot-file sensor notes
   `conversation_widget.js` grew again. The import graph is unchanged.
   Nothing pages the owner.
3. At the next **weekly architecture cycle** (capacity-gated, T353-slow,
   Fable-class like T357) the reader gets a *digest*, not the repo:
   sensor hits, `docs/architecture-current.md`, and a bounded manifest of
   the flagged packages. It writes one judgment: *two display models;
   owner-user-as-barrier holds in only one; this will paint replies above
   the question that provoked them.*
4. The judgment goes to the **overseer only**, on the same
   fire-and-forget rail as T243 / T357. The coach does not file.
5. The overseer files a **cleanup** target: one display fold, user row is
   a stream barrier, hermetic on the screenshot tape. That is T504,
   filed from structure, not from the owner’s screenshot.
6. `jevons-po` treats it like any other unblocked Build leaf (T155 / T193).
   A worker joins the models. The next architecture cycle sees the
   dual-path fingerprint gone and does not re-propose it (T333-shaped
   residue).

If we do nothing, the same landing still happens. The owner discovers it
as anachronistic bubbles. That is the failure this watch exists to make
rare.

## What already watches, and what they miss

| Loop | Watches | Misses |
|---|---|---|
| T243 drip | Owner chat, eventlog, sessions — *product/behaviour* | The tree. Residual is rule-based extract, not an architecture reader. |
| T353 retro | Bounded backward pass over the same surfaces + git | Git *churn* and revert noise, not “two implementations of one concern”. |
| T357 audit | Bounded file-list pass over code / skills / prompts; defect and prompt-drift findings; may *suggest* a target | Dual-path decay unless the auditor already knew to open both files. File-list, not graph. Daily defect cadence, not structural massage. |
| T42 | `architecture-current.md` tells the truth about the built spine | Whether the *modules* still match the cut that page describes. Honesty ≠ health. |
| hygiene.yaml | Declared floors (tests exist, scanners run) | Will not propose a re-cut. Drift on a floor is not “these two folds should be one”. |
| T130 filing reflex | Mid-turn opportunism when an agent notices a gap | The agent shipping the second fold does not notice. The owner did. |

T357 is the closest sibling (advanced model, suggests, never files,
residue, capacity). It is still the wrong *job*. An auditor asks “what
is wrong in these files?” An entropy watch asks “is the cut still the
cut?”

## Sensors (always-on, cheap, no LLM)

Sensors fire on a timer or on `HEAD` move. They write a digest. They do
not judge and they do not page.

| Sensor | Signal | Why it is load-bearing |
|---|---|---|
| Dual implementation | Two packages/files exporting the same concern (second fold, second join, second send path) | This is how T504, T371, T372 forks actually happen. |
| Boundary crossing | New import across a documented cut (`web/scripts` owning server policy; `cmd/jevonsd` growing product logic; `internal/mcpserver` importing UI concerns) | Grain decay. |
| Hot-file contention | Writes / commits to T376 guarded paths, T377 scope collisions | Shared-clone entropy; the tree is one working copy. |
| God-module growth | Byte/export growth of known hubs (`conversation_widget.js`, `internal/mcpserver`, `web/index.html`) | Unbounded files become undeletable second worlds. |
| Architecture-current drift | T42 ratchet + symbols the page names that no longer exist, or exist twice | The page can be “honest” about a mess. |
| Hygiene floors | Existing `hygiene.yaml` drift | Negative space only — not a proposal engine. |

The digest is the input to the LLM cycle. A sensor hit with no later
judgment is not a target.

## Cadence

Same split as the org map (§3): **always-on sense, episodic decide.**

- Sensors: on `HEAD` or a short interval (minutes). Token cost ≈ 0.
- Architecture cycle: **weekly** by default (T353-class, not T243’s 15m
  drip). Capacity class sits with audit / retro (T359): owner turns and
  open Build outrank it; tight budget skips the tick; critical does not
  run it.
- Manual: `jevons_entropy_cycle` (name indicative) for an on-demand pass.
- One cycle, one digest, a hard cap on judgments (small — 1–3). Sparse
  on purpose. A watch that files twelve cleanups a week *is* the gum.

T243 keeps dripping behaviour. T357 keeps its daily/bounded file-list
audit. This cycle does not steal their ticks and does not ingest their
full transcripts.

## Shape: sibling staff cycle, T243 rail

**Recommend: a sibling architecture coach / staff cycle, not a T243
mode and not a T357 lens.**

| Option | Why not (or why) |
|---|---|
| **Extend T243** | Wrong sensors, wrong model tier, wrong quality bar. T353 already added a second mode (history). A third mode (structure) on a rule-based chat extractor either starves architecture or floods behaviour. |
| **T357 lens** | Smallest patch, wrong question. File-list defect scan will not see a second fold unless both files are in the manifest *and* the prompt already cares about duality. Mixing “bug in this file” residue with “the cut moved” residue conflates two memories. |
| **Sibling on the T243 rail** | Same *control plane*: bounded cycle, judgment → overseer, never mint, T333-shaped dispositions, T359 capacity, fire-and-forget notice. Different *job*: digest of structural sensors + architecture-current + bounded flagged packages; Fable-class reader (T357 pin, not the cheap extract). |
| **Park** | Relies on T130 + T357 + T42. The motivating incident is the counterexample: T504 waited for the owner’s screenshot. |

Org-map placement: another row under **Staff**, next to RSI coach and
the T357 auditor. Not a second CEO. Not a product worker.

## Filing authority

**Keep T243’s rule.** The entropy watch never calls bullseye. The
overseer files, parks, or ignores with reason. Mid-turn T130 stays for
agents who *notice while building*; this watch exists for the ones who
do not.

An exception (watch files directly) would recreate the unread-queue
failure T243 was designed to avoid. Do not take it.

## Target classes

These are the only three shapes the watch may suggest. Each suggestion
carries name + acceptance + evidence pointers (paths, SHAs, sensor
hits). Bare “this file is messy” is refused (T333 quality bar, applied
to structure).

| Class | Grain | Example | Who runs it |
|---|---|---|---|
| **cleanup** | One concern, one or two packages, no documented-boundary change | Join `foldDisplayEvent.growAssistant` to the `applyLiveDisplayFrame` seal rule (T504) | PO → worker, ordinary Build |
| **spring-cleaning** | A *batch* of related nits that are individually not worth a leaf | Rename the leftover `createStreamJoin` handles after the fold is the only grow; delete the dead cousin | One worker, one target, listed inventory as acceptance |
| **re-architect** | Changes a cut `architecture-current.md` names, or adds/merges a package | “One display fold lives in `chat_events.js`; the widget only paints `fold.out`” | Design-gated. Owner taste. Packet before Build. |

**Not this:**

- Lint, format, one-line naming.
- A product bug whose fix is local and whose *cause* is not dual-path or
  a crossed boundary (file the bug; do not launder it as cleanup).
- T357’s prompt/skill/persona drift.
- “The next ticket.” Frontier remains the ready set (T262).

A cleanup that turns out to be a re-cut gets *refiled* as design-gated,
not silently enlarged.

## Bounds (so it cannot become the gum)

- Cycle default 7d; `max_cycles_per_week` = 2; min gap on manual fire.
- Wall clock and manifest caps in the T357 family (flagged packages
  only — never “read `internal/`”).
- Max 3 judgments per cycle; notify only on new or reopened *re-architect*
  severity. Cleanups wait in residue for the overseer to drain.
- Residue keyed on a structural fingerprint (concern + packages), not
  line number, so a moved fold updates rather than duplicates.
- A cycle that did not cover a sensor class must not resolve that class
  (T357 “covering pass” rule).
- `JEVONS_ENTROPY=0` disables the schedule; tools stay.

## First live example (not hypothetical)

Already on the ledger, already in the tree:

- **Symptom:** T504 — replies paint above the owner turn that provoked them.
- **Structure:** `web/scripts/chat_events.js` `applyLiveDisplayFrame`
  seals on a real owner user; `web/scripts/conversation_widget.js`
  `foldDisplayEvent.growAssistant` skips user rows and never seals.
  Live ingest is the fold (`applyWireEvent`).
- **What the watch should have emitted:** a cleanup *when the second
  fold landed*, with those two call sites as evidence — not after the
  screenshot.
- **What T357 would have needed to stumble into it:** both files in one
  manifest *and* a prompt that asks about dual display models.
- **Follow-on if this packet is accepted:** implement T504 as the
  cleanup; the first *watch* Build target is the sibling cycle itself,
  with a hermetic that replays this dual-path digest and expects a
  cleanup-class judgment, not a bullseye write.

## Open questions (owner)

1. Accept sibling-on-T243-rail, or pick T357 lens / extend T243 / park?
2. Weekly cadence vs T357’s daily — keep them distinct, or fold the
   architecture read into a weekly T357 “structure” scope?
3. May the watch ever page the owner directly on re-architect, or only
   the overseer?

Until (1) is answered in-band, nothing is built.
