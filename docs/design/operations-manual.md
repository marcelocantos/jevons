# Engineering Operations Manual (design)

**Status:** draft, deferred (🎯T35, set aside post-MVP). Design captured
from a 2026-07-05 discussion. Not scheduled; revisit after the T30 butler
loop is production-worthy.

Companion to [../charter.md](../charter.md) (who Jevons is and what it may
decide). This document specifies a **living engineering Operations Manual**
that Jevons consults to drive and review fleet work — and, crucially, how a
model of the owner feeds that manual *without Jevons ever impersonating the
owner*.

## 1. The deliverable is the manual, not the persona

The naive framing — "build a persona of Marcelo so Jevons can act as if it
were Marcelo" — is **wrong** and explicitly rejected. Jevons is not a
Marcelo emulator. The deliverable is an **Operations Manual: how *we* do
engineering**, which Jevons consults to (a) direct sub-agents and (b) review
their output.

A model of the owner is an **intermediate input** to that manual, never the
output. And it is a *filtered* input: the manual is meant to be **better than
its owner** — Marcelo is a very good engineer but not a perfect one, and the
manual must not inherit his weaknesses. The pipeline:

```
trove + curated artifacts
  → extract     → persona model            (intermediate; never surfaced as "act as Marcelo")
  → filter      → keep taste/conviction, drop biography and weakness
  → synthesize  → Operations Manual        ← the deliverable
  → inject      → how Jevons directs & reviews the fleet
```

## 2. Thesis — why a personal model feeds a "best-practice" manual at all

If the goal is *good engineering*, and good engineering is objective, why let
one person's taste in? Because **"best practice" harvested from the ambient
discourse is low signal-to-noise**. The modal engineering advice on the open
web is microservices, message queues, and whatever is currently fashionable —
that is what dominates the distribution, so a naive LLM synthesizer regresses
straight to it. The owner's taste plays a dual role against that regression:

1. **Discriminator** — a filter that separates signal from slop in external
   material, in domains where the owner is otherwise silent.
2. **Protected contrarian priors** — specific, hard-won positions that the
   mean would otherwise erase.

So the owner's role in the pipeline is precisely to be **the filter and the
set of protected priors** — not the bulk of the content. Good engineering is
good engineering; *recognizing* it in a noisy field is a taste problem, and
the owner's taste is the instrument.

## 3. Sources and the confidence hierarchy

Persona atoms are extracted from the distilled layer (mnemo memories,
decisions, discover_patterns, synthesis docs; Claude Code auto-memory), with
raw transcripts sampled — never streamed — for evidence. Sources rank:

```
curated self-authored artifact  ≫  explicit statement  ≫  correction  ≫  inferred-from-behavior  ≫  not-contradicted
```

- **Curated artifacts** (bio, résumé, published writing, the CLAUDE.md files
  the owner wrote) top the hierarchy: he reviewed and endorsed the wording.
  They are the primary window into the professional/identity layer the
  transcript trove leaves dark.
- **Down-weight day-job specifics.** The LinkedIn/day-job material
  (payments-engineering leadership, applied AI) is a *minor* identity input.
  Its only lasting value is signalling **which domains the owner has real
  depth in**, so convictions there carry more weight. It is not a focus.
- **Corrections are the richest behavioral signal** — each is the owner
  actively closing the gap between an agent's behavior and his preference.

## 4. The persona is an intermediate representation with two slices

Two distinct things flow out of the persona and feed *different sections* of
the manual:

- **Engineering ethos** → the *content* of the manual (what good engineering
  is). Convictions, quality bar, contrarian priors live here.
- **Agent-direction style** → the *fleet-operating* section (how Jevons runs
  and reviews sub-agents). This is **not mirrored** — the owner was explicit:
  Jevons should *consider what he looks for and keep it in the back of its
  mind*, not imitate his keystrokes. Distilled into review lenses (§7).

Biography (location, hardware, plan) is extracted for completeness but does
**not** propagate into the manual.

## 5. Letting the manual exceed the owner — three filters

The hard requirement ("not constrained by my weaknesses") reduces to:
extract the owner's *good judgment*, exclude his *bad habits*, from one
corpus containing both. Three filters:

1. **Conviction vs. habit.** A *conviction* is stated, defended when
   challenged, applied consistently, and carries a rationale (he can say
   *why* relational beats object stores). A *habit* is revealed only by
   behavior, unexamined, inconsistent. Weight convictions near-axiomatically;
   treat habits as weak evidence.
2. **Outcome-grounding.** A conviction that repeatedly produced good outcomes
   (sqlpipe works; the jevons/spyder monolith accretion is paying off) is
   validated; a pattern that led to rework/reverts is a candidate weakness.
   mnemo supplies the oracle (`mnemo_rework_history`, revert/CI signal). This
   is what lets the manual *overrule a habit* on evidence.
3. **External validation.** Each candidate principle is checked against the
   serious engineering canon — not to override the owner, but to classify:
   contrarian-and-defensible → *elevate*; contrarian-and-unsupported →
   *flag for the owner*. This is where "good engineering is good engineering"
   enters — as a validator, never a silent overwrite.

**The protective asymmetry:** *weaknesses* get overruled by evidence;
*convictions* get revised only by the owner or by evidence he accepts. A
synthesizer must never quietly replace a stated conviction with a
"best-practice" default — that is exactly the regression this design guards
against.

## 6. Anti-mean-regression — the protected-contrarian mechanism

This is the load-bearing requirement. Known contrarian priors are tagged so
synthesis cannot wash them out. Seed set (from the 2026-07-05 discussion):

- Relational model ≫ object stores.
- Synchronization primitives ≫ messaging primitives (sqlpipe is the paradigm).
- Monoliths ≫ microservices (jevons and spyder accrete responsibility on
  purpose).

A **doctrine entry** (the atom of the manual):

```yaml
statement:  Prefer a monolith accreting responsibility over a fleet of microservices.
rationale:  One coordination boundary beats N network boundaries; operational
            and cognitive cost of a service mesh rarely repays its modularity.
            jevons/spyder accretion is deliberate proof.
class:      protected-contrarian   # synthesis MAY NOT overwrite with the conventional default
provenance: conviction | outcome-validated(jevons T8.3 doit-absorption, spyder accretion)
tension:    when does accretion become an unmaintainable god-object? — the honest counter-case
evidence:   [<session-uuid>, <memory-name>, ...]
confidence: high
last_affirmed: 2026-07-05
status:     active
```

Entry classes: `protected-contrarian` (defend against the conventional
default), `validated-best-practice`, `contested-experimental`. The
`protected-contrarian` flag is the mechanism: it tells the synthesizer "the
conventional position is wrong here *by policy* — defend this one with its
rationale, do not average it away." The `tension` field keeps it honest — a
protected prior still states its own failure mode, so the manual is doctrine,
not dogma.

## 7. Agent-direction → review lenses

The owner's corrections are the training set for *what he looks for*; every
rejection encodes an implicit acceptance criterion. Distilled into a **review
rubric** Jevons applies to sub-agent output — a set of lenses it holds while
evaluating, not a checklist it robotically runs. Examples mined from existing
feedback memories:

- `north-star-not-moving-target` → flag tautological / self-validating tests.
- `semantic-port-not-transliteration` → flag transliteration where redesign
  was wanted.
- `oracle-first` / `feedback_oracle_driven_convergence` → flag tweak-fests and
  fudge-factors instead of analytical convergence against an oracle.
- pigeon codegen silent-skip → flag silently-dropped logic that still compiles.

This is adjacent to 🎯T31 (Jevons enforces oracle-first as a system
property) — the review-lens rubric is one concrete vehicle for it.

## 8. Living document — two cadences, owner as editor

- **Operating preferences** (terse voice, e2e>unit, timeouts on swift builds):
  fast auto-capture from sessions — the flywheel. Corrections apply near-
  instantly.
- **Doctrine principles** (contrarian priors, review lenses): these *expand*
  the manual, and **the owner is the editor**. Jevons proposes additions and
  revisions with evidence and rationale; principle-level changes are
  human-gated. The manual grows monotonically in coverage but accepts doctrine
  only on owner sign-off — which is also what stops a bad day from rewriting a
  good principle.

Maintenance rides three mechanisms (persona layer): continuous capture from
sessions Jevons oversees, decay+reinforcement of atoms (unreinforced claims
lose confidence; re-observed ones bump `last_affirmed`), and periodic
consolidation. Same architecture as mnemo's index→search→synthesize→compact,
and as human episodic→semantic→decay memory.

## 9. Product integration (seams)

Grounded in the current codebase (as of 2026-07-05):

- **Storage:** a new persona/manual store under `~/.jevons/`, alongside the
  `internal/thread` JSON registry, same grain (atomic write-and-rename).
  File-based first (the SQLite store was deliberately removed); graduate to
  real SQLite only if decay/frequency queries demand it.
- **Overseer injection (available today):** extend the generated-CLAUDE.md
  hook in `cmd/jevonsd/main.go` (which already appends `~/.claude/managed-
  repos.md`). The compiled manual replaces the hardcoded "about Marcelo" prose
  in `jevonsCLAUDEMD`, regenerated on daemon start and on manual update. Same
  treatment for the Grok voice `SystemPrompt` in `internal/server/voice.go`.
- **Worker injection (a real gap):** spawned workers have **no**
  jevonsd-controlled system-prompt seam. claudia's `AgentDef` has no prompt
  field and `Registry.Launch` never populates the `ExtraArgs` its `Config`
  already exposes. The clean fix is a small **claudia** change: a settable
  `--append-system-prompt`/`ExtraArgs` per agent, populated by `Launch`. Then
  inject the **task-scoped slice** of the manual (a Rust task gets the
  relational/testing/monolith doctrine, not the owner's biography). This is
  the one durable piece of new plumbing.
- **Maintenance loop:** a post-session distiller riding jevonsd's existing GC
  ticker — on `ReapIdle`, feed the just-finished transcript to a lightweight
  extractor that proposes deltas.

## 10. Bootstrap (one-shot)

A `Workflow`-shaped job over the *distilled* layer, not raw transcripts:
parallel extractors (traits over `user` memories; ethos+convictions over
`feedback`+`decisions`+synthesis docs; review lenses over corrections; a
sampled voice/style pass over transcripts) → dedupe barrier → synthesize the
initial manual with each entry classified and evidence-anchored. Run once;
the maintenance loop takes over.

## 11. Guardrails / failure modes

- **Anti-sycophancy.** An inferred claim that merely goes uncorrected is
  *weak* confirmation; do not let it bootstrap to high confidence.
- **Anti-recency.** One bad day must not rewrite a stable conviction;
  frequency + the doctrine human-gate guard this.
- **Contradiction → propose, don't rewrite.** New evidence conflicting with a
  high-confidence entry raises a proposed delta; it never silently flips.
  (The owner's own `feedback` memory "pause before acting on a fresh
  reframing" *is* this policy — the maintainer must embody it.)
- **Presented-self ≠ operating-self.** Curated artifacts are authoritative for
  outward-facing representation, not for how the work gets done. Never
  conflate the two.
- **Auditability.** Every entry cites evidence; the owner can always ask "why
  is this doctrine?" and get the session/quote. Trust-critical for an agent
  acting on his behalf.

## 12. Open questions

- How aggressively should external validation be allowed to *elevate* a
  non-owner principle the owner has never touched? (Default: propose, don't
  auto-adopt.)
- Where exactly is the boundary between "operating preference" (auto-capture)
  and "doctrine principle" (human-gate)? Some corrections are clearly
  doctrine (north-star tests); some are clearly preference (build timeouts).
  The gray zone needs a heuristic.
- Does the manual stay Jevons-internal, or does it become a shareable artifact
  the owner edits directly (a real `docs/`-style file under owner version
  control)?

## Relationship to targets

Successor to 🎯T30 (butler/CEO loop — must exist before doctrine-driven
direction can layer on top) and adjacent to 🎯T31 (oracle-first as a system
property). Filed as 🎯T35, deferred post-MVP.
