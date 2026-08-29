# ACP / Claudia progress signals for in-flight chat status (🎯T71)

**Supersession:** the 2026-08 audit below is the **vanilla T71** snapshot.
Thinking-vs-acting is no longer “not on the wire” — both Grok docs and
Cursor emit `agent_thought_chunk`; Claudia drops it. The current contract
is [overseer-turn-state.md](overseer-turn-state.md) (owner accept residual).

Audit of what the overseer chat path can surface mid-turn, and what the
UI uses today. Grounded in shipped wire + claudia event mapping
(`internal/server/chat_wire.go`, `web/index.html` handle path).

## Available during an in-flight overseer turn

| Signal | Source | Notes |
|--------|--------|--------|
| Tool name + short args | `assistant` content `tool_use` | Mapped to activity strip + working-label progress |
| Tool result snippet | `tool_result` / user tool_result | Activity strip; progress label uses last step |
| Streaming assistant text | `assistant` text chunks | Merged into open bubble; working stays on until terminal |
| Terminal stop | `stop_reason` / `end_turn` (incl. empty-text ACP end) | Clears working; seals stream |
| Agent identity | registry / overseer name | Not per-tool mid-turn from ACP today |
| Thinking vs acting | not on Grok ACP wire we consume | Do **not** invent |

## Not available (do not invent)

- Token/step percentages from Grok ACP
- Sub-agent fleet progress inside a single overseer turn (fleet panel is separate, polled)
- Structured “phase” enum beyond tool_use / streaming / end_turn

## UI policy (shipped)

1. Default: “Jevons is working …” with dots when no tool signal yet.
2. When tool steps exist: `Jevons: N steps · <last tool summary>` (🎯T71 thin).
3. Activity strip (turn marker + hover tip) remains the detailed step log (T63/T64 family).
4. Graceful fallback: if only coarse busy state exists, keep the simple working indicator.

## Related

- Fleet multi-agent progress (T89/T81) is a separate surface (RHS panel + lineage), not fake mid-turn chrome.
