# Overseer turn-state chrome

**Status:** owner-accepted 2026-08-27. Implement via the subgraphs below — do not wait for a second accept.  
**Supersedes (audit half):** [acp-progress-signals.md](acp-progress-signals.md) (🎯T71). T71’s *shipped* vanilla policy stays the frozen-reference fallback; this note is the React + Claudia contract.  
**Related:** 🎯T355 `chrome_truthful` (boolean today), 🎯T39 / 🎯T71 / 🎯T202 census, 🎯T64.2–T64.4 (tool names + token stream into the bubble), 🎯T291 (owner priority + queue; chrome no longer hides fleet chews), 🎯T113 (owner send-queue / Control+Enter interrupt), 🎯T111.1 (fleet-seat queue/interrupt), 🎯T540.3 (vanilla boolean remount is necessary, not this target).

## Problem

Daily React (`ui/`, `:13705`) has no owner-visible “Jevons is working …” chrome. `#status` paints only `connected` / `connecting`. `ConversationMeta.working` is reduced from mux `history_meta` and then used only for the degraded banner. CSS for `.working-indicator` exists; nothing mounts it.

Vanilla (`web/`, `:13706`) still has the indicator. That is a **boolean plus an optional progress string**, driven by `status` frames (`thinking` / `idle`) and mux `working` level samples. It is not a truthful account of *which* in-flight state the overseer is in, and idle is the *absence* of chrome.

RHS fleet rows already paint `idle` / `working · <tool>` / `blocked` from `GET /api/agents` (`AgentProgressHub`). That is a different surface and a coarser enum. The owner’s question is the **main-chat overseer** plane.

## Desired state

At every moment the owner-facing cockpit shows **exactly one** overseer turn-state, including idle, plus **who the in-flight turn is for** when that is not the owner. The state is a **closed enum** derived from signals Claudia (and then jevonsd) actually publish — never inferred from “is there an open bubble” or from a spinner that the client lit on send and forgot to clear.

The status bar **states the status**. Do not prefix `Jevons is` / `Jevons:` — the bar is already Jevons.

`chrome_truthful` (🎯T355) upgrades from “boolean matches server” to “painted phase (+ correspondent) matches server sample.”

## Closed enum

| Phase | Status-bar copy | When it is true |
|-------|-----------------|-----------------|
| `idle` | `idle` | Nothing in flight on the overseer session (`waiting` / `PromptInFlight` false, stream sealed). **Painted**, not hidden. |
| `accepted` | `received` | A prompt is in flight, but **no** `session/update` has arrived yet. |
| `thinking` | `thinking` | Latest ACP update is `agent_thought_chunk`. |
| `tool` | `<tool title>` or `tool` | Latest update is `tool_call` / `tool_call_update` with status in progress (or unknown). |
| `streaming` | `writing` | Latest update is `agent_message_chunk` (tokens arriving). Optional suffix: token count when the provider published one. |
| `permission` | `permission` | `session/request_permission` is outstanding (rare under `--force`; still a real state). |
| `error` | `error` | `IsError` / named residual (`delivery_failed`, …). Stays until the next prompt or an explicit settle. |
| `stuck` | `stuck` | In-flight longer than the existing watchdog (vanilla: 10m) **with no new ACP progress**. Jevons-derived, not a Claudia invention. |

Derived boolean (compat): `working = phase ∉ {idle, error}` except `stuck` stays working-true so T355 `chrome_false_idle` still fires. This boolean is **session-busy**, not “busy for the owner.” Owner-vs-fleet is the correspondent field.

## Correspondent (who this turn is for)

The overseer has one ACP session. A turn is either the owner’s words or a notify-queue batch (worker replies, idle events, daemon notices). Hiding fleet chews as `idle` (the T71/T291 chrome policy) is what made “Jevons working” lie in the other direction: the owner saw idle while Jevons was answering `jevons-po`.

**Paint the in-flight batch, not “whoever last sent a message.”** Last-sent is wrong when an owner line is queued behind a fleet chew, or when a fleet note is queued behind the owner. Correspondent is whoever **owns the prompt that is in the session now.**

| In-flight batch | Correspondent | Status-bar example |
|-----------------|---------------|--------------------|
| Owner (`[user]\n` / `overseerOwnerTurn`) | omit (the owner is the default) | `thinking` |
| One `[Agent <name> responded]` | that name | `thinking · jevons-po` |
| Fleet drain of several notes (T291 takes the whole fleet backlog in one prompt) | the names in that batch, stable order | `writing · jevons-po, jv-t555` |
| Event / system note with no agent name | `fleet` | `received · fleet` |

Idle has no correspondent. Do not leave a stale `· jevons-po` after seal.

Queue waiting **behind** the current turn is not the correspondent. A later residual may show depth (`+2 queued`); v1 does not.

Source of truth already exists on the drain path: `overseerOwnerTurn` plus `notifyAgentRespondedName` / the drained batch. Fan `correspondent: []string` on the same level sample as `phase`. Do not parse chat bubbles.

## Queue vs interrupt (as shipped — not a new policy)

Two planes. The owner’s “queue or interrupt” chord is **the owner composer onto the overseer**. Other agents do not get that chord onto Jevons.

**Onto the overseer (main chat, 🎯T62 / 🎯T291 / 🎯T113):**

| Sender | While Jevons is busy | Notes |
|--------|----------------------|--------|
| Owner, plain Enter | **Queue** | Client send-queue; server also enqueues. Owner turns never coalesce. Drain is owner-first, one at a time. |
| Owner, Control+Enter | **Interrupt** then send | Client cancel-and-send. The **server** will not interrupt an in-flight *owner* turn. |
| Owner, Esc | Interrupt, no send | Settles chrome (`cancel_settled`). |
| Owner send while a **fleet** chew is in flight | **Interrupt the fleet chew** (automatic) | T291: owner speech is not deferred behind idle churn. |
| Any agent / notify → Jevons | **Queue only** | Coalesce per worker (`[Agent name responded]` latest-wins). Cannot interrupt the owner. Cannot interrupt another fleet chew either — they wait, then drain as **one** fleet batch when the session is free. |

So: yes — you can queue or interrupt; everyone else queues on the way *in* to Jevons. The extra rule is that **you also preempt fleet**, without a chord.

**Onto other fleet seats** (`jevons_agent_send`, 🎯T111.1): default is still queue. `interrupt=true` is a stuck-recovery switch any authorized caller can pass (supervisor, owner HTTP, a parent). That is not the overseer-composer policy and must not be implied by this chrome.

T291’s *priority* rules stay. Only the *chrome* rule changes: a fleet chew is no longer painted as owner-idle.

## What the wire already has

### Claudia `Event` today

`Type` / `ProgressType` / `StopReason` / `Text` / `IsError` / `Usage` / `PromptInFlight()`. No first-class phase. Tool name and `toolCallId` live in `Event.Raw`. `Usage` is filled for Claude assistant records; ACP chunk `_meta.totalTokens` is not promoted.

### ACP `session/update` kinds (Grok docs + Cursor live)

| Kind | Grok docs | Cursor emits | Claudia today |
|------|-----------|--------------|---------------|
| `agent_message_chunk` | yes | yes | → `assistant` text |
| `agent_thought_chunk` | **yes** | **yes** (seen on disk) | **dropped** |
| `tool_call` / `tool_call_update` | yes | yes | → `progress` / `tool_use`; title often `MCP: tool` (🎯T64.2) |
| `plan` | yes | unknown | **dropped** |
| `user_message_chunk` | — | yes | → `user` (not owner chrome) |
| prompt RPC sent / result | client-side | client-side | `promptID` set; result → terminal assistant. **No Event on the send edge.** |

T71’s line “thinking vs acting: not on Grok ACP — do not invent” is **stale**. Both providers advertise `agent_thought_chunk`. Claudia is the drop.

Cursor also stamps `_meta.totalTokens` on thought/message chunks. That is the honest “tokens returned” signal. Grok may not. Do not invent a percentage.

`cursor/update_todos`, `cursor/task`, `cursor/generate_image` already arrive as `progress`. They refine `tool`, they do not mint new phases.

## Claudia uplift (yes — required)

Jevons must not parse `Event.Raw` to invent a phase the library already saw. Promote what ACP already sends.

1. **Forward dropped session updates** as typed progress, not transcript body:
   - `agent_thought_chunk` → `Type=progress`, `ProgressType=thought` (text stays off the owner transcript by default).
   - `plan` → `Type=progress`, `ProgressType=plan` (optional; chrome may ignore the plan body).
2. **Emit a phase-bearing event on the prompt edges** Claudia already owns:
   - `session/prompt` dispatched → `ProgressType=prompt_accepted` (this is `accepted`).
   - prompt RPC result / `IsTerminalStop` → existing terminal assistant (maps to `idle` or `error`).
3. **Promote tool identity** onto `Event` (not only Raw): `ToolCallID`, `ToolTitle`, `ToolStatus`. Unblocks chrome and T64.2 without every consumer re-parsing ACP JSON.
4. **Promote usage when the provider published it:** map Cursor `_meta.totalTokens` (and Claude `Usage`) onto `Event.Usage` for chunk events.
5. **Keep `PromptInFlight()`.** It remains the stuck-busy / T204 sensor. It is not a UI phase by itself.

Do **not** add a parallel Claudia-only state machine that guesses “thinking” from silence. Silence after `accepted` stays `accepted` until a real update or `stuck`.

Grok that never emits thought chunks will never show `thinking`. That is truthful.

Additive `Event` fields (STABILITY row grows; no jevons import — Claudia 🎯T13):

| Field | Set when |
|-------|----------|
| `ProgressType=thought` | `agent_thought_chunk` |
| `ProgressType=plan` | `plan` |
| `ProgressType=prompt_accepted` | `session/prompt` dispatched |
| `ProgressType=permission` | `session/request_permission` received, before the auto-reply |
| `ToolCallID`, `ToolTitle`, `ToolStatus` | `tool_call` / `tool_call_update` |
| `Usage` | Claude assistant usage **or** Cursor `_meta.totalTokens` on a chunk |

Existing `Type=progress` / `ProgressType=tool_use` stays. New fields are empty when the backend did not publish them.

These Events are **interleaved on the existing `SubscribeEvents` / `onEvent` stream**, in ACP arrival order, with assistant chunks and `tool_use`. Claudia does not grow a `Phase()` / `Status()` API or a second subscriber. A consumer that already folds the stream sees the new kinds as further `Event`s. (Claudia 🎯T50.)

## Transport: interleaved in the Claudia stream

Phase is not a second WebSocket protocol and not a poll. Claudia already delivers a **single callback stream** (`SubscribeEvents` / `Event`). Thought, tool, tokens, prompt-accepted, and permission are further events **in that same stream**, in time order with assistant chunks. jevonsd already fans that stream onto `/ws/chat` via `DeliverOverseerEvent` → `chatWireLine` (progress frames already exist).

Chrome **reduces the stream**: latest phase-bearing event wins. Same path, same clock as T64.3 token deltas.

| Kind | In the stream as | Paints as bubble? |
|------|------------------|-------------------|
| `prompt_accepted` / jevons drain stamp | `progress` | no |
| `thought` / `plan` / `permission` / `tool_use` | `progress` | no |
| assistant text | `assistant` | yes (existing) |
| terminal / cancel | `assistant` end_turn or `status` idle | no new bubble |
| `stuck` (jevons watchdog) | jevons-minted `progress` | no |

Correspondent is **jevons annotation** on the in-flight prompt (notify-batch names), stamped onto those progress frames — ACP does not know `jevons-po`. A drain that precedes Claudia’s `prompt_accepted` still interleaves a jevons `progress` (`accepted` + correspondent) so the bar does not wait for the first ACP update.

`working` stays a **derived boolean** of the reduced phase (vanilla / T355). `history_meta` may carry the **current reduce** so a hard reload does not flash idle (🎯T272). That snapshot is not a second SoT — it is the tail of the same stream.

Do not add `/ws/status`, a phase ticker, or a client enum that diverges from the last progress frame.

## Jevonsd

- On drain success: interleave a progress frame (`accepted` + correspondent from that batch).
- On each Claudia Event: map and fan through the existing wire (share the mapper with `AgentProgressHub`).
- On terminal / `cancel_settled`: interleave settle (`idle`, clear correspondent). Do not settle on the first assistant chunk.
- `stuck` is a jevons-minted progress event when the watchdog fires.

`AgentProgressHub.Phase` today is `working | idle | blocked`. Either widen it to this enum or keep RHS as a projection (`working` = any in-flight phase). Same mapper as the bar.

Mapper (one function, both chrome and hub):

| Latest signal | `phase` |
|---------------|---------|
| drain / `prompt_accepted` and no later update | `accepted` |
| `ProgressType=thought` | `thinking` |
| `tool_use` / tool fields, not terminal | `tool` (`step` = `ToolTitle` when not `MCP: tool`) |
| assistant text chunk | `streaming` |
| `ProgressType=permission` | `permission` |
| `IsError` / named residual | `error` |
| terminal / `cancel_settled` / sealed + `!PromptInFlight` | `idle` |
| watchdog, no new ACP progress | `stuck` |

## React chrome

**One ink:** the status bar (`#status`, next to `connected`). It always shows the phase word, plus ` · <correspondent>` when the in-flight batch is not the owner. Survives scroll and virtualization.

Do not remount a transcript footer that says `Jevons is working …`. The bar is the account. Vanilla’s `.working-indicator` stays the frozen reference for T540.3 boolean parity, not the React product copy.

When `step` is a real tool title, the phase copy *is* that title (🎯T71). When the title is `MCP: tool`, show `tool` — do not invent a name Cursor never wrote (🎯T64.2 residual).

Do not drive this from “composer just submitted.” The client may *optimistically* flash `received` on owner send, but the next interleaved progress frame is authoritative (T355 `chrome_false_working` / `chrome_false_idle`).

## What we refuse

- Token or step **percentages** the ACP did not emit.
- Painting `thinking` because `PromptInFlight` is true and nothing else has arrived (`accepted` is the honest state).
- Dumping thought-chunk text into `#messages`.
- Painting a fleet chew as owner-idle, or an owner turn as if it were `jevons-po`.
- Using “last message in the transcript” as correspondent.
- Treating T39/T71 census green on vanilla as React-done.
- A second phase enum in the client that diverges from the last progress frame.
- A parallel status socket, ticker, or poller for overseer phase.

## Oracle-coverage map (🎯T31.2)

| Clause | Bucket | Example |
|--------|--------|---------|
| Status bar always has exactly one phase word, including after hard reload | **pinned** | Fixture: idle snapshot → `idle`; open owner turn with no ACP yet → `received`; thought → `thinking`; tool_call → title; message chunk → `writing`; end_turn → `idle`. No `Jevons` prefix. |
| Fleet chew paints phase + correspondent, not owner-idle | **pinned** | Drain `[Agent jevons-po responded]` → `received · jevons-po` then ACP advances; owner boolean `working` may stay false for T291 owner-chrome tests, but the phase sample is not `idle`. |
| Owner in flight has no correspondent suffix | **pinned** | `thinking`, not `thinking · owner`. |
| Queued-but-not-drained note does not become correspondent | **pinned** | Owner turn in flight, jevons-po queued → bar stays owner phase with no `· jevons-po`. |
| Optimistic send then server idle sample clears chrome | **pinned** | T355 `chrome_false_working`. |
| Thought text not in transcript | **pinned** | Thought progress event present; no new assistant bubble from it. |
| Grok with no thought chunks never shows `thinking` | **pinned** | Only `accepted` → `tool`/`streaming` → `idle`. |
| Token count suffix | **fuzzy** | Show when `Usage` / `_meta.totalTokens` present; omit otherwise. Pin after one live Cursor capture. |
| `plan` chrome | **taste / defer** | Forward in Claudia; React may ignore until a later target. |
| Exact wording / dots / italics | **taste** | Owner look after first paint (🎯T493.1 prose verdict). |

## Implement graph

Owner accepted. Two ledgers — no cross-repo `depends_on`. Jevons 🎯T555.3 names the Claudia parent in acceptance.

```
claudia  T50          ACP session lifecycle is first-class on Event
         ├── T50.1    forward thought + plan as typed progress
         ├── T50.2    prompt-accepted + permission events on RPC edges
         └── T50.3    promote tool id/title/status + published usage

jevons   T555         umbrella (chrome = phase + correspondent)
         ├── T555.1   interleave phase progress on the existing Event → /ws/chat stream
         ├── T555.2   React status bar paints the sample          ← T555.1
         ├── T555.3   map new Claudia Event fields into the sample ← T555.1
         └── T555.4   census T39/T71/T202 assert the phase word   ← T555.2
```

T555.1 and Claudia T50.1–T50.3 are parallel. T555.3 must not be achieved until Claudia T50 is achieved (sibling HEAD via 🎯T448 `go.work`, not a committed `replace`). `thinking` is unreachable on the bar until T555.3; `received` / tool / `writing` / `idle` / correspondent are not.
