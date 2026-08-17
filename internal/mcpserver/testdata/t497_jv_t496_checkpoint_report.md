Checkpoint — ending this turn at the depth ceiling. Where I am on 🎯T496:

**Done so far:**
- The brief's pointer `createStreamJoin` in `chat_events.js` is stale — no such symbol. The real live paint path is: `web/index.html` `handleChat` assistant branch (~line 3171) → `mainConversation.applyWireEvent(m)` → `web/scripts/conversation_widget.js` `applyWireEvent` (line 1244).
- The pure coalescer (`applyChatEvent` in `chat_events.js:387`) handles text→tool_use→text correctly in its model (segment edges via `segmentEdgeById`), so the hermetic oracle to write must target the **widget's** wire-apply path, which is where the paint decision actually lives.
- Suspicion to verify: `index.html:3186` only sets `emittedText`/chrome for text parts; the silent-classification (`isSilentAssistantText`) plus the widget's stream-handle logic around tool_use frames is where the post-tool final text can fail to mint/join a visible bubble.

**Next step (successor turn):**
1. Read `conversation_widget.js` around lines 1000–1300 (`applyWireEvent` and its assistant/text/tool_use handling) to find why post-tool final text doesn't paint.
2. Write the failing hermetic test — likely in `conversation_widget_test.js` (widget path) and/or `chat_events_test.js` per the brief: text deltas → tool_use frames → more text → empty end_turn ⇒ final text present in an owner-visible bubble.
3. Fix, then `bin/gate -- make test-web`, local commit (own paths only, `--only`), finish report with GATE line + SHA.

No files modified yet; nothing to commit.