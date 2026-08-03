# 🎯T37 — xAI Voice Agent Builder vs Jevons DIY voice

**Status:** decision recorded · **Date:** 2026-08-03 · **Verdict: no-go** (do not adopt Voice Agent Builder as Jevons's primary voice stack)

Research only. No voice stack was implemented in this work. Sources: xAI product pages and docs (Voice Agent Builder launch 2026-07-01; Speech-to-Speech / ephemeral tokens / remote MCP), plus Jevons ledger targets 🎯T22 / 🎯T25 / 🎯T28 and current architecture (`docs/architecture-current.md`).

---

## 1. Decision (read this first)

| Question | Answer |
|---|---|
| Adopt **Voice Agent Builder** (no-code console, free phone number, hosted agent config, call review) as the primary voice product? | **No-go.** |
| Use the underlying **Grok Speech-to-Speech / Realtime API** later for client audio? | **Yes, as the DIY transport candidate** — that is already the shape of 🎯T22, not the Builder product. |
| Wire overseer brain via Builder's **"connect MCP servers"** so Grok Voice is front-end and Jevons stays brain? | **Not as primary architecture.** Technically plausible as a side experiment; security, latency, and dual-brain problems make it a poor default. |
| Telephony / free number for car-mounted iPad? | **Poor fit.** Nice-to-have side channel at best; not the primary deployment. |
| Resume full-duplex voice work now? | **No.** Voice stays de-emphasized. When it resumes, follow §7 successor shape — not Builder. |

**Do not re-litigate** unless one of these changes: (a) owner wants a **phone-reachable** Jevons line as a product goal, (b) xAI ships a **first-class "client app voice frontend to existing agent session"** that shares continuity with Grok ACP (not a separate call agent), or (c) Jevons exposes a **deliberately hardened, internet-facing MCP** surface designed for short tool turns.

---

## 2. What xAI shipped (facts)

### Voice Agent Builder (beta, 2026-07-01)

Marketed as a **no-code platform for production voice agents** on Grok Voice ([x.ai/news/grok-voice-agent-builder](https://x.ai/news/grok-voice-agent-builder), [x.ai/voice](https://x.ai/voice)):

- Plain-language playbooks, knowledge collections, tools/connectors, guardrails, call recordings/transcripts/traces.
- **Telephony:** free provisioned number + bring-your-own via SIP.
- **MCP / APIs:** wire existing APIs and remote MCP servers.
- **Own client:** connect over WebSocket (same underlying voice path as the API).
- Pitch audience: operators shipping **high-volume support / sales / reception** agents without stitching STT + LLM + TTS.

### Grok Voice API stack (the layer under the Builder)

Documented as Speech-to-Speech over WebSocket (`wss://api.x.ai/v1/realtime`, model e.g. `grok-voice-latest`), plus separate STT/TTS, custom voices, ephemeral tokens for browsers/mobile ([docs.x.ai Voice overview](https://docs.x.ai/developers/model-capabilities/audio/voice), [Speech to Speech](https://docs.x.ai/developers/model-capabilities/audio/speech-to-speech), [Ephemeral tokens](https://docs.x.ai/developers/model-capabilities/audio/ephemeral-tokens)).

Relevant capabilities for Jevons:

| Capability | Notes |
|---|---|
| Full-duplex S2S | Server VAD, barge-in, sub-second latency claims |
| Tools | `web_search`, `x_search`, `file_search`, **remote `mcp`**, client `function` tools |
| Remote MCP | `server_url` must be **Streaming HTTP or SSE**; xAI runs the MCP client server-side |
| Session resumption | Opt-in `resumption.enabled`; history ~**30 min idle expiry** — still phone-call shaped, not day-long overseer continuity |
| Auth for clients | `POST /v1/realtime/client_secrets` → short-lived token; browser uses `xai-client-secret.*` subprotocol |
| Pricing (vendor) | **~$0.05 / min audio** (voices included); telephony on free number **+$0.01 / min** |

Builder is a **console + telephony + ops** product on top of that API. The API can be used without the Builder.

---

## 3. Jevons baseline (what we are deciding against)

### Shipped product posture (2026-08)

- Overseer is a **persistent Grok ACP agent** (`jevons`), not Claude-as-brain ([architecture-current](../architecture-current.md)).
- Canonical UI is **web chat**; iOS is a thin WKWebView shell over pigeon.
- **Web mic removed** — dictation is **Wispr Flow → text field** (🎯T21 achieved). Full-duplex voice de-emphasized pending this decision.
- Dormant DIY machinery still in-tree: `/ws/voice`, `internal/server/voice*.go` (Grok Realtime bridge + FSM), `ios/…/VoiceManager.swift`. Historical FSM notes in [voice-fsm.md](../voice-fsm.md).

### 🎯T25 (DIY voice I/O; overseer outside xAI session) — set aside

- Premise: Grok as **STT/TTS only**; overseer continuity in a **non-xAI** (Claude) session.
- **Invalidated:** the shipped harness makes the overseer itself a **Grok ACP** session. "Overseer runs outside xAI's session model" is no longer true.
- Phase 0 client-side episodic work was partially device-validated historically; the brain-bridge design never became current architecture.

### 🎯T22 (browser → Grok direct, bypass jevonsd for audio)

- Motivation still valid for **iPad-in-car**: avoid hauling PCM through home ngrok/jevon; cut latency and residential uplink (~64 KB/s × 2 per active session in the old proxy design).
- Approach matches modern xAI guidance: **ephemeral token from jevonsd**, client WebSocket to `wss://api.x.ai/v1/realtime`.
- Acceptance text is **stale**: it still describes `send_to_jevons` + Claude injection. That must be rewritten if T22 is ever executed under the Grok-overseer world.

### 🎯T28 (road-noise suppression)

- Capture-pipeline DSP (RNNoise / WebRTC APM / VP NS) on the **device**. Orthogonal to Builder telephony. Depends on a real mic capture path existing again.

---

## 4. Evaluation dimensions

### 4.1 MCP: "Grok Voice front-end, Jevons brain"

T37's interesting hook was Builder/API **remote MCP** pointing at Jevons `/mcp` so voice could call `jevons_*` while Claude/Jevons stayed the brain.

| Factor | Assessment |
|---|---|
| Transport match | Jevons already serves **Streamable HTTP** at `/mcp` — the transport family xAI documents for remote MCP. |
| Reachability | xAI's cloud must **dial out** to the MCP URL. Daily jevonsd is typically **localhost / LAN / personal ngrok**. Exposing `/mcp` to the public internet is a **new security product**, not a config toggle. |
| Auth | Remote MCP supports `authorization` + headers. Jevons MCP is built for trusted local agents, not internet callers with least-privilege scopes. |
| Tool shape | Fleet tools (`jevons_agent_start`, `jwork`, long directs) are **minutes-scale**. Voice tool loops want **sub-second to few-second** results or the call feels dead. |
| Dual brain | Voice S2S model **reasons and speaks**; MCP tools are side effects. Meanwhile the **ACP overseer** also reasons with the same fleet tools. Two conversational brains, one fleet → race conditions, split memory, cost double-spend. |
| Continuity | S2S resumption is **short-lived** (docs: ~30 min idle). Overseer work is **hours**. This is the same architectural mismatch that burned ~ten days on 🎯T18 / motivated 🎯T25. |

**Conclusion:** MCP-connect is a real xAI feature, but it does **not** cleanly give "thin voice front-end + Jevons brain." It gives **another agent** that can poke Jevons tools if you expose them. That is the opposite of collapsing complexity for a single-owner CEO butler.

### 4.2 Latency

| Path | Expected feel |
|---|---|
| Grok S2S alone (Builder or raw API) | Sub-second conversational turns (vendor claim); good for chitchat and short tool results. |
| S2S + remote MCP into jevonsd fleet | STT/voice latency **plus** MCP RTT **plus** agent/tool time. Coding-fleet actions dominate; voice layer cannot hide that. |
| Historical DIY: Grok voice + separate overseer round-trip (T25 design) | Explicitly accepted 2–5 s brain latency for reliability. |
| Current text path (Wispr → chat → overseer) | No full-duplex; already the daily path. |

Builder does **not** remove the hard latency of fleet work. It only makes the **voice envelope** faster when the model answers from its own context.

### 4.3 Telephony / free number vs car iPad

Primary deployment in the ledger is **iPad-in-car / hands-free app**, not "call a support number."

| Builder telephony | Jevons need |
|---|---|
| PSTN number, SIP trunk, call recordings | App mic + speaker (or BT), background audio, local VAD, cabin noise (T28) |
| Contact-center handoff to human | Owner **is** the human; handoff is meaningless |
| Browser preview of phone agent | Browser already has the **chat** product |

A free number is a **side channel** (call Jevons from any phone). It is not a substitute for in-cabin full-duplex in the app, and it does not justify re-centering the product on Builder.

### 4.4 Guardrails and observability

Builder: call playback, transcripts, tool traces, policy guardrails (no PII readback, etc.).

Jevons already has (or is building) **owner-facing** observability: chat transcript, fleet tree, cost clamp, journey/self-test packs, mnemo. Call-center review UI does not replace the chat panel and is the wrong primary audit surface for coding work.

### 4.5 Cost model

Vendor list prices (confirm on [docs.x.ai pricing](https://docs.x.ai/developers/pricing) before budgeting):

- **~$0.05 / min** bidirectional audio for S2S.
- **+$0.01 / min** if using provisioned telephony.
- Extra meters for hosted search tools, etc.

Rough intuition: one hour of continuous cabin voice ≈ **$3** audio-only at $0.05/min, before overseer/fleet tokens. Episodic PTT or Wispr-style dictation is usually cheaper and matches "issue a command, wait for work." Continuous always-on S2S is a different cost regime.

DIY using the **same** S2S API pays the same audio meter; Builder does not make audio free. Builder mainly avoids **your** engineering of telephony and console ops — costs Jevons does not currently need.

### 4.6 Lock-in

Jevons is already Grok-default (claudia ACP). Adopting Builder adds:

- Agent playbooks and knowledge living in **xAI console** (outside git / bullseye / persona.md).
- Optional dependency on xAI-hosted telephony and call storage.
- A second product surface (console) the owner must configure separately from `~/.jevons` and the web UI.

Using the **Realtime API only** keeps configuration in jevonsd + client code (same lock-in class as today's dormant voice bridge).

---

## 5. Product-shape mismatch (the decisive argument)

Voice Agent Builder optimizes for:

> High-volume **phone agents** that complete short workflows (lookup, book, refund, transfer) with tools and a knowledge base.

Jevons optimizes for:

> One owner talking to a **CEO overseer** that spawns and supervises a **coding fleet** over long horizons, with durable threads, cost governance, and a chat/fleet UI.

| Concern | Builder-native | Jevons-native |
|---|---|---|
| Session length | Call / short resume window | Hours, reconnect, ACP session resume |
| Primary I/O | Phone + optional web preview | Chat UI + optional full-duplex later |
| Tool duration | Seconds | Seconds to hours (workers) |
| Success metric | Call resolved | Mission/targets achieved |
| Config home | xAI console | Repo persona, bullseye, `~/.jevons` |

Adopting Builder as the primary stack would either (1) **fork** the product into "phone agent that happens to call MCP" or (2) **re-implement** half of Jevons inside Builder prompts — both are worse than keeping voice as a transport on the existing overseer.

The announcement's complaint about "stitching STT + LLM + TTS" is real for contact centers. Jevons's pain was different: **forcing a realtime voice session to be a long-lived overseer.** Builder does not solve that; it productizes the phone-call shape that already failed us as an overseer host.

---

## 6. Impact on 🎯T22 / 🎯T25 / 🎯T28

| Target | Disposition under this decision |
|---|---|
| **T22** (browser→Grok direct audio) | **Leave identified; still the right transport idea.** Do **not** supersede with Builder. When voice resumes, **rewrite acceptance** for Grok-overseer era (ephemeral token + client S2S; continuity via chat/overseer ACP — drop Claude/`send_to_jevons` wording). Builder is optional sugar, not a dependency. |
| **T25** (Grok voice I/O, overseer outside xAI) | **Leave set_aside.** Premise remains invalid under Grok ACP overseer. Do **not** revive as written. A successor (if needed) is "voice transport only; continuity in overseer ACP/chat," which is closer to an evolved T22 than to T25. |
| **T28** (road-noise suppression) | **Leave identified; leave untouched by Builder.** Still requires on-device capture pipeline. Remains blocked on having a real mic path again, not on Builder adoption. |
| **Dormant `/ws/voice` bridge** | **Do not invest further** as the long-term design (proxies audio through jevonsd — the problem T22 names). Deletion/cleanup can ride a later hygiene target when voice work resumes; out of scope for T37. |
| **Wispr / T21 text dictation** | **Unaffected** (already achieved). Remains the daily voice-adjacent path. |

**Supersede / augment / untouched summary**

- **Supersede:** nothing mandatory. Builder does not replace T22/T25/T28.
- **Augment:** optional future "phone side-channel" could use Builder or SIP cookbook **without** becoming the primary stack (file only if owner wants it).
- **Untouched:** T28 DSP; chat/fleet control plane; Wispr path.

---

## 7. Recommendation and successor shape

### Verdict: **no-go** on Voice Agent Builder as primary voice stack

Reasons, compressed:

1. **Wrong product** (contact-center agents vs CEO butler).
2. **MCP-as-brain is a mirage** without public hardened MCP + acceptance of dual-brain.
3. **Telephony does not serve the car-iPad primary deployment.**
4. **Same session-continuity mismatch** that already failed (short S2S vs long overseer).
5. **No cost or latency win** on the work that matters (fleet coding); only on pure voice chitchat.
6. **Lock-in of config/ops into xAI console** fights Jevons's in-repo control plane.

### When full-duplex voice resumes (successor doctrine — not a build order today)

Prefer this shape over Builder:

1. **Transport:** client (browser/iOS) → `wss://api.x.ai/v1/realtime` with **ephemeral tokens** minted by jevonsd (T22 core). Prefer **direct** path so car audio does not hairpin home.
2. **Role of Grok Voice:** speech I/O and short conversational latency — **not** the durable overseer session of record.
3. **Continuity / tools / fleet:** remain on the **existing overseer ACP + `/ws/chat` + jevons MCP** path (text and events). Bridge transcripts and spoken replies explicitly; do not rely on S2S session memory for multi-hour work.
4. **Capture quality:** device pipeline + T28 when in-cabin.
5. **Builder/telephony:** only if a separate target asks for "call Jevons by phone."

### Spike?

**No required spike** of the Builder console for this decision. Evidence from public docs + Jevons architecture is enough for no-go.

Optional owner-driven spikes (not filed unless requested):

- **Phone side-channel:** provision a number, MCP allowlist of **read-only / short** tools, measure usability.
- **Ephemeral-token smoke:** mint token from jevonsd, 60 s browser S2S echo (precursor to T22), no fleet tools.

---

## 8. Acceptance checklist (🎯T37)

| Criterion | Met? |
|---|---|
| Note at `docs/analysis/voice-agent-builder-eval.md` | Yes (this file) |
| Evaluates Builder vs client-side loop (T25) and browser-to-Grok (T22) | Yes (§3–§6) |
| MCP-as-brain, latency, telephony/car iPad, guardrails/observability, cost, lock-in | Yes (§4) |
| States supersede / augment / leave for T22/T25/T28 + go/no-go/spike | Yes (§6–§7): **no-go** |
| No-go rationale recorded to avoid re-litigation | Yes (§1, §5, §7) |

---

## 9. References

- xAI, *Introducing the Voice Agent Builder* (2026-07-01): https://x.ai/news/grok-voice-agent-builder
- xAI Voice Agent Builder product: https://x.ai/voice
- xAI Voice API overview: https://x.ai/api/voice
- Docs: Voice overview, Speech to Speech, Ephemeral tokens, Remote MCP (docs.x.ai)
- Jevons: `docs/architecture-current.md` (Voice section), `docs/voice-fsm.md`, bullseye 🎯T22 / 🎯T25 / 🎯T28 / 🎯T37
