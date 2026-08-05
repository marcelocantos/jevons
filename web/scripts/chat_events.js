// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Pure chat-event helpers shared by the browser UI and headless tests.
// Keep this file free of DOM / transport so Node can require() it.
//
// Grok ACP streams many assistant text chunks (often one token each),
// then a terminal empty-content end_turn. The UI must:
//   1. Keep "working" until a terminal stop (not the first text chunk)
//   2. Coalesce all text chunks into a single jevons bubble per turn

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ChatEvents = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  const TERMINAL_STOPS = new Set(['end_turn', 'stop_sequence', 'max_tokens']);

  function stopReason(m) {
    const msg = m && m.message;
    if (!msg) return '';
    return msg.stop_reason || msg.stopReason || '';
  }

  function isTerminalStop(m) {
    return m && m.type === 'assistant' && TERMINAL_STOPS.has(stopReason(m));
  }

  function assistantTextBlocks(m) {
    const content = m && m.message && m.message.content;
    if (!Array.isArray(content)) return [];
    return content.filter(c => c && c.type === 'text' && c.text);
  }

  function hasAssistantText(m) {
    return assistantTextBlocks(m).length > 0;
  }

  // ── Silent overseer ops replies (🎯T238 / T240 / T245) ──────────
  // Mirrors internal/silentresponse.Is: owner-filterable when the turn
  // text starts with [silent] (optional leading whitespace / short lead-in).
  const SILENT_PREFIX = '[silent]';

  /**
   * Whether assistant completion text must not paint as an owner bubble.
   * @param {string|null|undefined} text
   * @returns {boolean}
   */
  function isSilentAssistantText(text) {
    const t = String(text == null ? '' : text).trim();
    if (!t) return false;
    const lower = t.toLowerCase();
    const pref = SILENT_PREFIX.toLowerCase();
    if (lower.startsWith(pref)) return true;
    // Marker on its own line within a short lead-in (first 80 chars).
    const head = t.length > 80 ? t.slice(0, 80) : t;
    const lines = head.split('\n');
    for (let i = 0; i < lines.length; i++) {
      if (lines[i].trim().toLowerCase().startsWith(pref)) return true;
    }
    return false;
  }

  // ── Segment-edge join (🎯T161; replaces T147 content sniff) ─────
  // Grok/ACP emits separate assistant *segments* at protocol boundaries
  // (tool rounds, multi-block content parts). Bare concat across those
  // edges smashes paragraphs/fences/lists. T145 ensureFenceNewlines is
  // display-only for already-fused fences.
  //
  // Zero content heuristics: join never inspects for `.`, capitals, ```,
  // headings, lists, or word counts. The caller knows it is a segment
  // edge from the event/protocol shape and uses joinAssistantSegments.
  // Intra-segment token deltas use appendAssistantStream (bare concat).

  /**
   * Join two assistant segments at a known segment boundary.
   * If neither side already has a line break at the boundary, insert a
   * blank line. Does not inspect markdown or prose shape.
   *
   * @param {string|null|undefined} prev
   * @param {string|null|undefined} next
   * @returns {string}
   */
  function joinAssistantSegments(prev, next) {
    prev = String(prev == null ? '' : prev);
    next = String(next == null ? '' : next);
    if (!prev) return next;
    if (!next) return prev;
    if (/[\n\r]$/.test(prev) || /^[\n\r]/.test(next)) return prev + next;
    return prev + '\n\n' + next;
  }

  /**
   * Intra-segment stream append (token/delta). Always bare concat.
   *
   * @param {string|null|undefined} prev
   * @param {string|null|undefined} next
   * @returns {string}
   */
  function appendAssistantStream(prev, next) {
    prev = String(prev == null ? '' : prev);
    next = String(next == null ? '' : next);
    return prev + next;
  }

  /**
   * Join an ordered list of known segments (multi-block content).
   * Prefer this over `.join('')` for multi-block assistant content.
   *
   * @param {Array<string|null|undefined>|null|undefined} parts
   * @returns {string}
   */
  function joinAssistantTexts(parts) {
    return (parts || []).reduce((acc, p) => joinAssistantSegments(acc, p), '');
  }

  // shouldClearWorking: terminal seal signal (end_turn / system). Mid-stream
  // text chunks must NOT match — sealing early dropped workingEl and used to
  // force a new bubble per token (the "Hello / . / What / do / you / need / ?"
  // regression). Stream seal still keys on this; owner working *chrome* uses
  // shouldClearWorkingChrome (🎯T260) so tools-only end_turn does not idle UI.
  function shouldClearWorking(m) {
    if (!m || !m.type) return false;
    if (m.type === 'system') return true;
    if (m.type !== 'assistant') return false;
    const content = m.message && m.message.content;
    // Reject non-array content (raw ACP shapes).
    if (!Array.isArray(content)) return false;
    return TERMINAL_STOPS.has(stopReason(m));
  }

  /**
   * Owner working-chrome policy (🎯T260).
   * Terminal seal ≠ idle chrome. A tools-only ACP end_turn often precedes
   * agent_note re-prompts and more overseer work; clearing chrome there
   * made the UI look idle while the response was still coming.
   *
   * Clear chrome when:
   *   - system settle / cancel
   *   - terminal after owner-visible (non-silent) assistant text
   *   - terminal on a pure-silent stream (residual: no bubble OK)
   *   - vacuous terminal (no tools and no visible text — empty settle)
   * Keep chrome when:
   *   - terminal after tool/progress activity only (tools-only stop)
   *
   * @param {{hadVisible?: boolean, hadTool?: boolean, silent?: boolean}|null|undefined} chrome
   * @param {object} m wire event
   * @returns {boolean}
   */
  function shouldClearWorkingChrome(chrome, m) {
    if (!shouldClearWorking(m)) return false;
    if (m.type === 'system') return true;
    const c = chrome || {};
    if (c.hadVisible) return true;
    if (c.silent) return true;
    // Tools-only terminal: keep "Jevons is working…" (T260).
    if (c.hadTool) return false;
    // Vacuous terminal (no tools, no body): clear so chrome is not stuck.
    return true;
  }

  /** Fresh per-owner-send chrome flags for shouldClearWorkingChrome. */
  function createWorkingChrome() {
    return { hadVisible: false, hadTool: false, silent: false, open: true };
  }

  /**
   * Update chrome flags from one wire event (assistant/user/system).
   * Does not decide clear — pair with shouldClearWorkingChrome after apply.
   */
  function applyWorkingChrome(chrome, m) {
    if (!chrome || !m || !m.type) return chrome;
    if (m.type === 'user') {
      const content = m.message && m.message.content;
      if (typeof content === 'string' && content) {
        chrome.hadVisible = false;
        chrome.hadTool = false;
        chrome.silent = false;
        chrome.open = true;
      }
      return chrome;
    }
    if (m.type === 'system') {
      return chrome;
    }
    if (m.type !== 'assistant') return chrome;
    const content = m.message && m.message.content;
    if (!Array.isArray(content)) return chrome;
    for (let i = 0; i < content.length; i++) {
      const c = content[i];
      if (!c) continue;
      if (c.type === 'tool_use') {
        chrome.hadTool = true;
      } else if (c.type === 'text' && c.text) {
        if (isSilentAssistantText(c.text)) {
          chrome.silent = true;
        } else {
          chrome.hadVisible = true;
        }
      }
    }
    return chrome;
  }

  // Pure stream coalescer — models bubble count without DOM.
  // Used as the oracle for multi-chunk turns.
  // segmentEdgePending: next text after non-text protocol content
  // (tool_use, tool_result) joins via joinAssistantSegments, not bare concat.
  function streamIdOf(m) {
    if (!m) return '';
    const id = m.stream_id != null ? m.stream_id : m.streamId;
    return id == null ? '' : String(id).trim();
  }

  function createTurnState() {
    return {
      working: false,
      userTexts: [],
      // Each entry is one jevons bubble's accumulated raw text.
      assistantBubbles: [],
      // Index of the open unlabeled stream bubble, or -1 when sealed.
      openStream: -1,
      // 🎯T223: stream_id → bubble index (join by identity, not adjacency).
      streamBubbles: Object.create(null),
      // stream_id → true while that stream is still open.
      openById: Object.create(null),
      segmentEdgePending: false,
      // Per-stream segment edge (id → bool); legacy uses segmentEdgePending.
      segmentEdgeById: Object.create(null),
      // 🎯T245: stream_id → true once classified silent (drop further body).
      silentById: Object.create(null),
      // Legacy unlabeled open stream is silent (whole-stream suppress).
      legacySilent: false,
      // 🎯T260: owner working-chrome flags (see shouldClearWorkingChrome).
      workingChrome: createWorkingChrome(),
    };
  }

  function applyChatEvent(state, m) {
    if (!m || !m.type) return state;
    if (!state.workingChrome) state.workingChrome = createWorkingChrome();

    if (m.type === 'user') {
      const content = m.message && m.message.content;
      if (typeof content === 'string' && content) {
        const last = state.userTexts[state.userTexts.length - 1];
        if (last === content) return state; // dedupe echo + ACP user chunk
        state.userTexts.push(content);
        state.working = true;
        applyWorkingChrome(state.workingChrome, m);
        // 🎯T223: user mid-stream does NOT seal open assistant streams.
        // Streams seal only on terminal stop_reason (or system). Interleave
        // is OK when fragments share stream_id; legacy openStream also stays.
        state.segmentEdgePending = false;
      }
      return state;
    }

    if (m.type === 'tool_result' || m.type === 'result') {
      if (state.openStream >= 0) state.segmentEdgePending = true;
      Object.keys(state.openById || {}).forEach(function (id) {
        if (state.openById[id]) state.segmentEdgeById[id] = true;
      });
      return state;
    }

    if (m.type === 'assistant') {
      const content = m.message && m.message.content;
      const sid = streamIdOf(m);
      // 🎯T245: whole-stream silent suppress (mirror T240 server). Once this
      // stream's accumulated text is silent, drop further body until seal.
      if (!state.silentById) state.silentById = Object.create(null);
      // Walk content in order: non-text marks a segment edge for the next
      // text part; multiple text blocks in one message are segment edges
      // between them; continuous single-block frames stay bare-concat.
      if (Array.isArray(content)) {
        let textPartsThisEvent = 0;
        for (let i = 0; i < content.length; i++) {
          const c = content[i];
          if (!c) continue;
          if (c.type === 'text' && c.text) {
            // Stream already marked silent — drop body (do not paint).
            if (sid && state.silentById[sid]) {
              applyWorkingChrome(state.workingChrome, {
                type: 'assistant',
                message: { content: [{ type: 'text', text: c.text }] },
              });
              continue;
            }
            if (!sid && state.legacySilent) {
              applyWorkingChrome(state.workingChrome, {
                type: 'assistant',
                message: { content: [{ type: 'text', text: c.text }] },
              });
              continue;
            }
            // First fragment (or full turn) that is pure silent: mark + drop.
            if (isSilentAssistantText(c.text)) {
              if (sid) state.silentById[sid] = true;
              else state.legacySilent = true;
              applyWorkingChrome(state.workingChrome, {
                type: 'assistant',
                message: { content: [{ type: 'text', text: c.text }] },
              });
              continue;
            }
            let idx = -1;
            let edge = false;
            if (sid) {
              if (state.streamBubbles[sid] == null) {
                state.assistantBubbles.push(c.text);
                idx = state.assistantBubbles.length - 1;
                state.streamBubbles[sid] = idx;
                state.openById[sid] = true;
                state.segmentEdgeById[sid] = false;
              } else {
                idx = state.streamBubbles[sid];
                edge = !!(state.segmentEdgeById[sid] || textPartsThisEvent > 0);
                state.assistantBubbles[idx] = edge
                  ? joinAssistantSegments(state.assistantBubbles[idx], c.text)
                  : appendAssistantStream(state.assistantBubbles[idx], c.text);
                state.segmentEdgeById[sid] = false;
              }
              // Keep legacy openStream pointing at latest labeled stream for
              // callers that still read the singleton handle.
              state.openStream = idx;
            } else if (state.openStream >= 0) {
              idx = state.openStream;
              edge = !!(state.segmentEdgePending || textPartsThisEvent > 0);
              state.assistantBubbles[idx] = edge
                ? joinAssistantSegments(state.assistantBubbles[idx], c.text)
                : appendAssistantStream(state.assistantBubbles[idx], c.text);
              state.segmentEdgePending = false;
            } else {
              state.assistantBubbles.push(c.text);
              state.openStream = state.assistantBubbles.length - 1;
              state.segmentEdgePending = false;
            }
            textPartsThisEvent += 1;
            state.workingChrome.hadVisible = true;
            // Re-arm if a prior tools-only end_turn left chrome up, or if
            // chrome was wrongly cleared (defense).
            state.working = true;
          } else if (c.type === 'tool_use') {
            state.workingChrome.hadTool = true;
            state.working = true;
            if (sid) {
              if (state.openById[sid]) state.segmentEdgeById[sid] = true;
            } else if (state.openStream >= 0) {
              state.segmentEdgePending = true;
            }
          } else if (c.type && c.type !== 'text') {
            // Other non-text content: next text is a new segment
            if (sid) {
              if (state.openById[sid]) state.segmentEdgeById[sid] = true;
            } else if (state.openStream >= 0) {
              state.segmentEdgePending = true;
            }
          }
        }
      }
      if (shouldClearWorking(m)) {
        // Seal stream always on terminal; chrome only when policy says so.
        const chromeSnap = {
          hadVisible: !!state.workingChrome.hadVisible,
          hadTool: !!state.workingChrome.hadTool,
          silent: !!(state.workingChrome.silent || state.legacySilent ||
            (sid && state.silentById && state.silentById[sid])),
        };
        if (shouldClearWorkingChrome(chromeSnap, m)) {
          state.working = false;
          state.workingChrome.open = false;
        }
        if (sid) {
          delete state.openById[sid];
          delete state.segmentEdgeById[sid];
          delete state.silentById[sid];
          // Only clear singleton if it pointed at this stream.
          if (state.streamBubbles[sid] === state.openStream) {
            state.openStream = -1;
          }
        } else {
          state.openStream = -1;
          state.legacySilent = false;
        }
        state.segmentEdgePending = false;
      }
      return state;
    }

    if (m.type === 'system') {
      state.working = false;
      state.openStream = -1;
      state.openById = Object.create(null);
      state.segmentEdgeById = Object.create(null);
      state.silentById = Object.create(null);
      state.legacySilent = false;
      state.segmentEdgePending = false;
      if (state.workingChrome) state.workingChrome.open = false;
    }
    return state;
  }

  function applyChatEvents(events) {
    const state = createTurnState();
    for (const m of events) applyChatEvent(state, m);
    return state;
  }

  // workingLifecycle: final working flag after events (send already set true).
  function workingLifecycle(events) {
    const state = createTurnState();
    state.working = true;
    for (const m of events) applyChatEvent(state, m);
    return state.working;
  }

  /**
   * 🎯T272 level-trigger: derive whether owner working chrome should show
   * from a *current* sample of in-flight state — not from remembering a
   * send/kickoff edge. Reload/reconnect works because history_meta (or
   * status) re-samples server truth; pure client edge memory is not SoT.
   *
   * @param {object|null|undefined} sample
   * @param {boolean} [sample.working] server history_meta.working
   * @param {boolean} [sample.busy]
   * @param {string} [sample.state] status frame state (thinking|idle|…)
   * @param {boolean} [sample.promptInFlight]
   * @param {boolean} [sample.openStream]
   * @param {boolean} [sample.serverWorking]
   * @returns {boolean}
   */
  function workingLevelFromSample(sample) {
    if (!sample || typeof sample !== 'object') return false;
    if (sample.working === true || sample.busy === true ||
        sample.promptInFlight === true || sample.openStream === true ||
        sample.serverWorking === true) {
      return true;
    }
    if (sample.working === false || sample.busy === false) {
      // Explicit idle level — only override if another positive signal.
      // (state alone handled below when working/busy absent)
    }
    const st = sample.state != null ? String(sample.state).toLowerCase() : '';
    if (st === 'thinking' || st === 'working' || st === 'busy') return true;
    if (st === 'idle' || st === 'cancel_settled') return false;
    // history_meta without working key: not in flight (sealed history only).
    if (sample.working === false || sample.busy === false) return false;
    return false;
  }

  // ── 🎯T279 post-send owner-turn retention ──────────────────────────
  // Soft reconnect suppresses user-echo frames until history_meta (T143).
  // Without optimistic paint, a flap after wire.accept clears the composer
  // and never paints the outbound bubble → silent vanish. Hydrate/WS
  // reconcile must merge/keep optimistic owner turns, not drop them.

  function _entryText(entry) {
    if (entry == null) return '';
    if (typeof entry === 'string') return entry;
    if (typeof entry === 'object' && entry.text != null) return String(entry.text);
    return '';
  }

  /**
   * Plan optimistic main #messages paint after transport.accept.
   * Aside wires stay off main (T264) — paint:false with reason aside-wire.
   *
   * @param {Array<{text?:string}|string>} existing owner turns already painted
   * @param {string} text wire body accepted by transport
   * @param {(t:string)=>boolean} [isAsideWire] true when body is attention/target-aside
   * @returns {{ paint: boolean, text: string, reason: string }}
   */
  function planOptimisticMainUserPaint(existing, text, isAsideWire) {
    const t = String(text == null ? '' : text).trim();
    if (!t) return { paint: false, text: '', reason: 'empty' };
    if (typeof isAsideWire === 'function' && isAsideWire(t)) {
      return { paint: false, text: t, reason: 'aside-wire' };
    }
    const list = existing || [];
    const last = list.length ? _entryText(list[list.length - 1]).trim() : '';
    if (last === t) return { paint: false, text: t, reason: 'already-last' };
    return { paint: true, text: t, reason: 'optimistic' };
  }

  /**
   * Merge painted owner texts with pending/optimistic tails after
   * hydrate/WS reconcile. Never drop a pending owner turn that is still
   * missing from the painted list (shorter hydrate must not win).
   *
   * @param {Array<string|{text?:string}>} paintedUserTexts
   * @param {Array<string|{text?:string}>} pendingTexts
   * @returns {{ keepTexts: string[], missing: string[] }}
   */
  function planRetainOwnerTurns(paintedUserTexts, pendingTexts) {
    const painted = (paintedUserTexts || []).map(function (e) {
      return _entryText(e);
    });
    const pending = (pendingTexts || []).map(function (e) {
      return _entryText(e).trim();
    }).filter(function (t) { return t.length > 0; });
    const have = Object.create(null);
    for (let i = 0; i < painted.length; i++) {
      const k = String(painted[i]).trim();
      if (k) have[k] = true;
    }
    const missing = [];
    for (let j = 0; j < pending.length; j++) {
      if (!have[pending[j]]) {
        missing.push(pending[j]);
        have[pending[j]] = true;
      }
    }
    return {
      keepTexts: painted.concat(missing),
      missing: missing,
    };
  }

  /**
   * After soft reconnect history_meta: which unacked main-paintable bodies
   * still need a bubble (echo was suppressed; optimistic may have been wiped
   * only on hard reconnect — soft keeps DOM, but race can still miss).
   *
   * @param {{ paintedUserTexts?: Array, pendingTexts?: Array, isAsideWire?: function }} opts
   * @returns {{ repaint: string[] }}
   */
  function planRepaintAfterSoftReconnect(opts) {
    const o = opts || {};
    const painted = o.paintedUserTexts || [];
    const pending = o.pendingTexts || [];
    const isAside = o.isAsideWire;
    const have = Object.create(null);
    for (let i = 0; i < painted.length; i++) {
      const k = _entryText(painted[i]).trim();
      if (k) have[k] = true;
    }
    const repaint = [];
    for (let j = 0; j < pending.length; j++) {
      const t = _entryText(pending[j]).trim();
      if (!t) continue;
      if (typeof isAside === 'function' && isAside(t)) continue;
      if (have[t]) continue;
      repaint.push(t);
      have[t] = true;
    }
    return { repaint: repaint };
  }

  /**
   * Normalize owner echo text for equality (🎯T281 / T279 shared).
   * Unwraps a single outer <user_query>…</user_query> so optimistic plain
   * body matches ACP-wrapped server echo without double-painting.
   */
  function normalizeOwnerEchoText(text) {
    let t = String(text == null ? '' : text).trim();
    if (!t) return '';
    // One-shot unwrap (same shape as AgentTranscript.unwrapInspectUserText).
    for (let i = 0; i < 3; i++) {
      const m = t.match(
        /^\s*<user_query(?:\s[^>]*)?>\s*([\s\S]*?)\s*<\/user_query>\s*$/i,
      );
      if (!m) break;
      t = String(m[1] || '').trim();
    }
    return t;
  }

  /**
   * Server user echo after optimistic paint: drop when last painted text
   * already matches (no double bubble). Unwrap-aware (🎯T281).
   */
  function isDuplicateUserEcho(lastPaintedText, echoText) {
    const a = normalizeOwnerEchoText(lastPaintedText);
    const b = normalizeOwnerEchoText(echoText);
    return !!(a && b && a === b);
  }

  return {
    stopReason,
    isTerminalStop,
    assistantTextBlocks,
    hasAssistantText,
    isSilentAssistantText,
    streamIdOf,
    joinAssistantSegments,
    appendAssistantStream,
    joinAssistantTexts,
    // Alias: only for known segment-edge joins (not token streams).
    coalesceAssistantText: joinAssistantSegments,
    shouldClearWorking,
    shouldClearWorkingChrome,
    createWorkingChrome,
    applyWorkingChrome,
    workingLifecycle,
    workingLevelFromSample,
    createTurnState,
    applyChatEvent,
    applyChatEvents,
    // 🎯T279 retention / 🎯T281 send-once
    planOptimisticMainUserPaint,
    planRetainOwnerTurns,
    planRepaintAfterSoftReconnect,
    normalizeOwnerEchoText,
    isDuplicateUserEcho,
    TERMINAL_STOPS,
    SILENT_PREFIX,
  };
}));
