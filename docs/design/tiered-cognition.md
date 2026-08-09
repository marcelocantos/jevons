# Tiered cognition (🎯T403)

**Status:** design direction. Nothing here is built. 🎯T392.8 is the cheap first test.
**Date:** 2026-08-10
**Scope:** how an agent decides, and where the model sits inside that. Not a spend lever, though found while chasing one.
**Delivery:** local master only. No PR/ship.

---

## 1. What was measured

Everything below is reasoning. This section is not — it is the 🎯T392 baseline, read from
Grok's own `turn_completed` billing frames over 2026-08-08T01:53Z .. 2026-08-09T11:53Z,
reproducible with `make spend-baseline`.

| | |
|---|---|
| Input | 907.1 M tokens = 1,070 turns × 4.5 calls/turn × 187 k context |
| Output | 1.7 M (0.2%) — the fleet is billed for **re-reading**, not producing |
| Coordinators (overseer + POs) | **83.0%** of input, from 12 sessions |
| Implementers | 16.5%, from 51 sessions |
| `jevons-po` alone | 39.6%, at 235 k context per call |

And within `jevons-po`'s 245 MCP calls: 94 `agent_send` (38%), 47 `bullseye_query` (19%),
29 `transcript_read` (12%), 29 `agent_kill` (12%), 10 `agent_list` (4%) — **~85% reading
state, relaying, and reaping**. Only 24 `agent_start` and 11 `bullseye_commit` are
plausibly judgement. Of 219 shell commands, 104 were `true` and 73 a bare `cd`.

The fleet is paying frontier-model prices to run `agent_list` and forward a message.

## 2. The reframe

An agent is usually defined by its **substrate**: a model in a loop with tools. `doit` is
defined by its **structure**: perceive, decide through a ladder, act, record immutably.
Those are the same four things. The difference is where intelligence sits — a fleet agent
has one cognitive tier and spends ~200 k of context on every decision, trivial or not.

The objection that this cannot be right, and its answer:

> *"`doit` only adjudicates proposals; agents originate intent."*
> Wrong. A raw model does nothing until prompted — a turn exists only because something
> started one. The animus is in the harness: idle-nudge sweeps, sentinel, pressure loops,
> cockpit converge. The model never had any. Both are equally reactive.

`doit` is an advanced form of agent, advanced not because it differs in kind but because it
**tiers what the other spends flat**.

### Consequence: the work is narrow

Jevons already has the animus layer and it already fires. What changes is what those loops
*call*. `SweepIdleNudges` currently decides an agent should be prompted, then wakes a model
to do everything; under this design it calls `Evaluate`, acts on L1/L2, and wakes the model
only on escalation. **The harness is untouched.**

Jevons also already has L1. `ClassifyIdleNudge`, `ClassifyPOProactive`,
`ClassifyFrontierLeaf`, `HasOpenMissionForIdle`, `ResolveEventParent`,
`POOpenMissionForProactive` were all written so the harness could decide *whether to
prompt*. The system has L1 and L3 today and uses L1 **solely to decide whether to invoke
L3**. It never lets L1 act.

Missing: permission for L1 to act, and L2 to accumulate.

## 3. Why this failure regime is different

This is the part with no published answer, and it is the reason this document exists rather
than a link to the prior art.

| System | Cheap tier fails by producing… |
|---|---|
| FrugalGPT cascade | a wrong **answer** |
| Semantic cache | a wrong **answer** |
| Voyager skill | a **failed task**, with environment feedback |
| Learning-to-defer | a **mis-deferral**, measured against labels |
| **This** | **nothing at all** |

In every prior system the cheap tier fails by emitting a *worse artifact of the same kind*,
sitting where you can see it. Here it fails by **omission**: the escalation does not happen,
no deliberation exists to inspect, and the metric being optimised **improves**.

That asymmetry explains an observation that would otherwise be puzzling: Voyager's skill
library has no revision or removal path at all, and Progressive Crystallization's demotion
triggers are all *loud* — a parser threw, a test regressed, a safety rule fired. Neither
needed a silent-omission detector, because in their settings a bad cheap-tier decision is
**expensive and noisy**. Here it is **cheap and quiet**, so it wins by default.

Corollary, and the design's central hazard: **the cheapest expressible rule is "handle
everything, escalate nothing"**, and optimising token cost finds it immediately.

## 4. Design constraints, and where each came from

### From motor learning (owner)

Skill consolidation is not cortex-to-spine; it is a shift within the brain from prefrontal
and dorsomedial striatum toward dorsolateral striatum and cerebellum. The destination is not
dumber, it is **more rigid** — which is why bad technique bakes in, unlearning costs more
than learning, and experts often cannot articulate what they do. **Automaticity buys cost
with adaptability**, exactly the trade promotion makes.

Biology manages that trade three ways, each of which becomes a constraint:

1. **Consistency-gated** — you do not automatise a movement that keeps changing.
2. **Offline consolidation** — during sleep, not mid-performance (the 🎯T353 cadence).
3. **Bidirectional** — prediction error re-engages the expensive system.

### From Progressive Crystallization (arXiv 2607.07052, production)

Steal the thresholds rather than inventing them:

| Transition | Gate |
|---|---|
| agent → hybrid | ≥10 successful runs; zero safety violations; ≥90% of runs produce the same action sequence; auto-generated acceptance tests pass |
| hybrid → deterministic | ≥50 successful hybrid runs; ≥99% model-classification consistency; rule covers all observed input variation |

And their framing of autonomy, which is better than ours was: *"autonomy is attached to the
specific playbook class and action type, based on its evidence, rather than to the
capability of the underlying model."* Earned per decision-class, not granted per agent.

Demotion is a circuit breaker on execution failure, safety violation, or acceptance-test
regression. Their production example: a firmware update changed a command's output format,
the deterministic parser failed, the playbook demoted to hybrid so the model could handle it.

### From Voyager (arXiv 2305.16291)

- **Keyed on intent, not state.** Key = embedding of a natural-language *description* of
  what the program does; value = the code; retrieval = top-5 by similarity to a description
  of the new task. L2 can therefore accumulate **prose descriptions**, not an exhaustive
  predicate language.
- **Generality is requested at mint time**: *"your function will be reused… make it generic
  and reusable."*
- **A critic, not a verifier.** Voyager's self-verification is another GPT-4 acting as
  critic — not ground truth — and it fails sometimes (not recognising spider string as
  evidence of beating a spider). Removing it costs **73%** of discovered items, the single
  most load-bearing component in the system. Design the critic first and hardest.

## 5. The uncertainty protocol (🎯T403.1)

The insight that dissolves the resolve-vs-escalate binary:

> **Tiers pass uncertainty upward as data, not as control flow.**

A lower tier that is unsure need not choose between guessing silently and paying for an
escalation. It resolves provisionally and **marks** the resolution — `⟦your world →
yourworld2?⟧`. Escalation is binary and costs a model turn; an annotation is continuous and
costs a dozen tokens.

This is the primary answer to §3. **A marked guess is an artifact.** An unmarked guess
leaves nothing to demote on.

Three properties that make it work:

- **Fail-safe.** No feedback is *no evidence*, and the default without evidence is to keep
  annotating. The loop degrades to verbosity, never to silent error. Monotone in the safe
  direction: annotated → resolved only on positive evidence, never the reverse by default.
  Silence is therefore not weighed as weak assent — it is not counted at all.
- **Free labels.** Downstream behaviour confirms or denies, producing correctness labels
  from work that was happening anyway. This is what consistency gating cannot supply:
  consistency proves a rule is **stable**, never that it is **right**, and a systematically
  wrong resolution promotes just as smoothly as a correct one.
- **A general channel.** Ambiguity is its first user; provenance, staleness, elision
  (*N lines omitted, fetch with X*) and attention hints all want the same mechanism.

Reserved codepoints rather than `[...]`, because that makes marker survival **mechanically
testable** — count them in, count them out, and any pipeline stage that eats one fails.
And because it is a real channel that system prompts act on, it is a **trust boundary**:
anything able to emit markers can signal, so untrusted input is stripped on the way in.

## 6. Cognition and authority are orthogonal

| Axis | Question |
|---|---|
| **Cognition** | Who decided — a rule, a policy, or the model? |
| **Authority** | May this actor perform this action at all, however it was decided? |

Jevons collapses these today and gets away with it because there is exactly **one decider**:
"the agent decided" and "the agent is authorised" are the same checkpoint. Tiering breaks
that identity — the decider may now be a rule that nobody authorised to do anything.

So `doit` does **not** fold away. Its architecture folds in; its **instance must not**:

> A process may have arbitrarily sophisticated internal logic and still not write its own
> page table.

**One engine, two stores.** Sharing the evaluation engine is correct and avoids two
implementations of promotion. The authority chain keeps its own policy store, capability
registry, audit log and administration path, **outside the agent it gates**. The tempting
wrong turn, which looks like sensible de-duplication: *"we already have doit, point both
ladders at it."*

**Promotion grants no authority.** A rule promoted for judgement reasons inherits exactly
the authority its action class already had. Otherwise *"we always spawn a worker for an
unblocked Build target"* becomes, by promotion alone, permission for an unattended
deterministic tier to spawn workers — which is `writ`'s `drift` exactly.

**A single action never invokes a model twice.** `doit`'s L3 asks *is this safe*; this
design's L3 asks *does this need judgement*. Composed naively that is two model calls where
a single-tier agent spends one turn answering both.

## 7. Anti-patterns

1. Treating the escalation rate purely as an efficiency number. A collapse toward zero
   escalations is **indistinguishable from the system optimising away its own oversight**,
   and must be treated as a defect signal until proven otherwise.
2. Letting the agent that benefits from a rule be the agent that installs it.
3. Promoting on consistency alone, with no correctness signal.
4. Promoting in the hot path of the decision being generalised.
5. Pre-digesting context without preserving uncertainty markers — context assembly and the
   annotation protocol become adversaries if the summariser normalises markers away.
6. Assuming a deterministic tier that *can* answer *should* answer. Known-hard classes
   should short-circuit **upward**.

## 8. What is measured, what is reasoned

Honesty about the evidence base, because most of this document is the second kind.

**Measured:** the §1 baseline; `jevons-po`'s call distribution; Progressive
Crystallization's production results (>70% per-incident cost reduction over eight months,
deterministic executions 0%→45%, tens of thousands of incidents/month); Voyager's ablations.

**Reasoned:** that the ladder transfers to fleet coordination; that L1 can cover a
meaningful share of coordinator calls; that annotation-plus-feedback produces enough signal
to gate promotion; that the critic can judge coordinator decisions at all.

**The load-bearing assumption**, and the one to test first: Voyager's critic reads a
bounded, fully-observable state — inventory, position, biome, health. A jevons critic would
judge *"was spawning that worker right?"* against an outcome that unfolds over hours and is
entangled with everything else the fleet did. **That is a harder judgement in a
harder-to-observe world, and it has no precedent in either cited system.**

**Open question with the widest blast radius:** whether the plan meters raw context or
discounts cached reads. The baseline was 96.6% cache reads — 17.6 M fresh out of 907 M. If
raw context is metered, the context levers dominate. If cached reads are cheap, the *turns*
levers matter far more and much of the context work is wasted effort. One day of plan-usage
data settles it (🎯T390).

## 9. Prior art

| Work | Relationship |
|---|---|
| [Progressive Crystallization](https://arxiv.org/abs/2607.07052) | **This architecture, in production.** Thresholds and circuit-breaker demotion taken directly. Does not address silent omission. |
| [Voyager](https://arxiv.org/abs/2305.16291) | Skill-library promotion for capability rather than cost. Critic, intent-keying, mint-time generality. No demotion path. |
| [SkillAudit](https://arxiv.org/abs/2606.14239) | **Unread.** Ground-truth-free skill evolution — nearest published work to §3's open problem. Read before building. |
| [CODE-SHARP](https://arxiv.org/abs/2602.10085) | **Unread.** Skill evolution as hierarchical reward programs. |
| FrugalGPT / model cascades | Model→model tiering. This is code→model, and per durable agent rather than per request. |
| Learning-to-defer, Reflexion | Adjacent; deferral and self-improvement respectively. |
| Fraud/risk rule promotion | The mature production analogue of L3→L2, predating LLMs. Escalates to **humans**, who notice under-escalation. |

Gas Town ([yegge.ai/gastown](https://yegge.ai/gastown)) is the nearest comparable fleet
orchestrator and has **no analogue**: Mayor, Polecats and Witness are all LLM-backed, only
the Refinery is deterministic, and neither the landing page nor the launch essay discusses
token cost, tiering, or rule promotion at all. The instinct exists (the Refinery), applied
once to a mechanical bottleneck, not generalised.

## 10. Tension to hold

🎯T125 (POs are spawn-only), 🎯T129 (overseer never parents product workers) and 🎯T31
(overseer is the independent completion gate) put agents in the loop **deliberately**.

A tiered agent must not become the thing that decides what work exists.
