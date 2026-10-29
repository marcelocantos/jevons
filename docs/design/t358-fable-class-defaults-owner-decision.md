# Fable-class defaults for overseer and product owners (🎯T358)

**Status:** decision packet — **needs-owner**; wiring lands inert until `config.yaml` records ratification.  
**Related:** 🎯T325.2 portfolio routing, 🎯T325.2.1 fast-cheap workers, 🎯T392.3 coordinator cheap tier (blocked on this target), 🎯T357 audit Fable pin (audits only).

---

## 1. What we are deciding

Owner question (2026-08-09): should the **root overseer** and/or **product owners** default to **Claude Fable-class** (`claude-fable-5`) instead of flagship Opus / provider frontier defaults?

Workers are out of scope for this ratification: Build seats already route mechanical/ops through **fast_cheap** (Spark / grok-build) via 🎯T325.2.1 when task types match; implementation stays on code_implement unless explicitly pinned.

This packet does **not** achieve 🎯T358. Owner accept plus a config record is required to close the target.

---

## 2. Comparison (cost, latency, quality, interruptibility)

| Seat class | Typical work | Cost / latency | Quality / interruptibility | Today (unratified) |
|------------|--------------|----------------|----------------------------|--------------------|
| **(a) Root overseer** | Owner chat, fleet coordination, short decisions | Fable: lower $/token, faster turns, smaller context than Opus | Good for routing and owner Q&A; weaker on large multi-file design | `overseer_model` / `model` in config.yaml, else provider default on boot |
| **(b) Product owners** | Fan-out, verify, brief workers, ledger hygiene | Same Fable trade-off; PO turns are often dispatch-shaped | Strong enough for triage; risky for deep design without explicit model= | `task_type: ceo` portfolio class; omit-model → provider default (🎯T475) |
| **(c) Workers** | Implement, test, land | **fast_cheap** tier when classified mechanical/ops; else flagship path | Implementation needs depth | 🎯T325.2.1 automatic Spark/grok-build pins; not Fable by default |

**Interruptibility:** Fable-class models are tuned for interactive fleet loops (owner + PO). Flagship models remain appropriate for long-context implement and explicit `model=` pins.

---

## 3. Recommendation options (owner picks one)

| Option | Overseer default | PO default | Worker default | When to choose |
|--------|------------------|------------|----------------|----------------|
| **keep_current** | unchanged | unchanged | unchanged | No fleet-wide migration; explicit pins only |
| **root_fable** | `claude-fable-5` when provider is Claude and no explicit `overseer_model` | unchanged | unchanged | Owner chat speed; POs stay on today's pins |
| **po_fable** | unchanged | `claude-fable-5` on Claude PO mint | unchanged | Dispatch-shaped PO work without touching overseer |
| **both** | Fable when unconfigured | Fable on Claude PO mint | unchanged | Align overseer + PO on Fable; workers stay cheap-tier / explicit |
| **tiered** | Fable when unconfigured | **provider default** (not auto-Fable) | fast_cheap where routed | Root on Fable; POs on frontier; workers cheap |

PO recommendation (packet, not owner accept): start with **tiered** or **po_fable** if the bill pain is mostly PO/coordinator turns; **both** only if owner wants overseer and PO aligned.

---

## 4. How acceptance is recorded (not chat-only)

After owner chooses:

1. Set hot-reloadable config (`~/.jevons/config.yaml` on development):

```yaml
fable_class_defaults:
  ratified: tiered   # keep_current | root_fable | po_fable | both | tiered
```

2. Optional explicit overseer pin (wins over ratification):

```yaml
overseer_model: claude-fable-5
```

3. Optional portfolio overlay (`state_dir/llm-portfolio.json`) — CEO row `models.claude` may mirror the decision for documentation; omit-provider mint still follows config provider (🎯T476).

4. Bullseye attestation on achieve cites the config value and a hermetic or live mint probe.

**Does not** silently migrate running seats: remint or restart applies pins on next mint.

---

## 5. Wiring map (product surfaces)

| Surface | Behavior when ratified |
|---------|-------------------------|
| `registerOverseerSeat` | `cost.EffectiveOverseerModel` applies **root_fable / both / tiered** on Claude when `overseer_model` empty |
| `jevons_agent_start` PO | `PickMintModel` + `fable_class_default` knob for **po_fable / both** on Claude |
| Workers | Unchanged; 🎯T325.2.1 fast_cheap still wins for mechanical/ops |
| 🎯T392.3 | Remains blocked until this target is achieved with owner record |

---

## 6. Owner responses we will record

- **keep_current** → leave `ratified: keep_current` (default); no pin change.
- **root_fable / po_fable / both / tiered** → set `fable_class_defaults.ratified` as above; PO achieves after probe.
- **Defer** → `bullseye set_aside` with revisit predicate; not a silent drop.

---

## 7. Explicit non-claims

- This packet does **not** achieve 🎯T358.
- This packet does **not** enable 🎯T392.3 coordinator cheap tier by itself.
- Hermetic tests ratchet this file and the mint pins; **owner accept is required** to achieve the target.
