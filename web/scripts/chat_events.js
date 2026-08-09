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

  // 🎯T381: turn origin carried on the wire. Agent/system reports (worker
  // seal reports, budget alerts) share the ACP "user" role with the owner's
  // own words, so role alone cannot decide how a bubble paints. The server
  // stamps turn_origin on user-role lines; owner turns stay verbatim and
  // agent turns paint markdown. Provenance, never a sniff of the body — an
  // owner who types "**Commit:**" must still see "**Commit:**".
  const TURN_ORIGIN_OWNER = 'owner';
  const TURN_ORIGIN_AGENT = 'agent';

  /**
   * Classify a wire event (or an already-coalesced display line) by who
   * spoke it. Anything unmarked is the owner: verbatim is the safe default,
   * so an old journal line or a provider that never learned the field can
   * never cause owner input to be reinterpreted as formatting.
   *
   * @param {object|null} event
   * @returns {'owner'|'agent'}
   */
  function turnOriginOf(event) {
    if (!event || typeof event !== 'object') return TURN_ORIGIN_OWNER;
    const raw = event.turn_origin !== undefined ? event.turn_origin
      : (event.turnOrigin !== undefined ? event.turnOrigin : event.origin);
    if (typeof raw !== 'string') return TURN_ORIGIN_OWNER;
    return raw.trim().toLowerCase() === TURN_ORIGIN_AGENT
      ? TURN_ORIGIN_AGENT : TURN_ORIGIN_OWNER;
  }

  /**
   * Does a bubble of this role/origin paint through the markdown renderer?
   * Assistant always does; a user-role bubble does only when the turn came
   * from an agent, never when the owner typed it.
   *
   * @param {string} role  display role ('user' | 'jevons' | 'assistant' | …)
   * @param {string} origin  'owner' | 'agent'
   * @returns {boolean}
   */
  function bubblePaintsMarkdown(role, origin) {
    if (role === 'jevons' || role === 'assistant') return true;
    if (role !== 'user') return false;
    return origin === TURN_ORIGIN_AGENT;
  }

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

  /**
   * 🎯T384: the owner's words out of a user-role wire message, in EITHER
   * content shape.
   *
   * The server now writes owner turns as typed blocks — the same shape every
   * assistant turn has always used — because a bare string was the one thing
   * in the journal that a block-walking consumer could not read, and the
   * owner's own message therefore painted empty and vanished from asides.
   *
   * Reading both shapes is the load-bearing half. Every agent chatlog and
   * owner journal already on disk holds bare-string owner content, so a
   * reader that accepted only the new shape would trade one vanish for a far
   * worse one: the owner's entire history. Bare string in, blocks in, same
   * text out.
   *
   * @param {object|null} m wire message ({message:{content}})
   * @returns {string} the owner's text, or '' when there is none
   */
  function userContentText(m) {
    const content = m && m.message && m.message.content;
    if (typeof content === 'string') return content;
    if (!Array.isArray(content)) return '';
    let out = '';
    for (let i = 0; i < content.length; i++) {
      const c = content[i];
      if (c && c.type === 'text' && typeof c.text === 'string') out += c.text;
    }
    return out;
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

  // ── 🎯T329 non-boundary user lines (harness / fleet injects) ─────
  // These paint as inject nuggets (inspect) or are skipped as owner turns,
  // but must NEVER seal an open assistant stream. Real owner prose remains
  // a boundary for *display* of user rows; stream seal is still terminal-
  // only (T223). Matches extractTurns / AgentTranscript.classifyInspectUserLine.

  /**
   * True when a type=user body is harness/fleet inject, not an owner turn.
   * Mid-turn system-reminder / standing-brief / background-task users must
   * not open a new assistant bubble after them within the same owner turn.
   *
   * @param {string|null|undefined} text
   * @returns {boolean}
   */
  /**
   * True when a type=user body is really a client protocol control frame —
   * a bare JSON object with a non-empty string "type" ({"type":"ux_state",…},
   * ping / rewind / inspect_* wires) (🎯T362).
   *
   * These leaked into the owner journal before the server filtered them, so
   * they arrive on both the live wire and history hydrate. They are machine
   * wire, never an owner turn: never paint one as a user bubble, never let one
   * seal a stream. Deliberately generic on shape, not an allowlist of names —
   * the next control frame the client learns to send must not reopen the hole.
   *
   * @param {string|null|undefined} text
   * @returns {boolean}
   */
  function isProtocolControlFrameText(text) {
    const t = String(text == null ? '' : text).trim();
    if (t.length < 2 || t[0] !== '{' || t[t.length - 1] !== '}') return false;
    let obj;
    try { obj = JSON.parse(t); } catch (_) { return false; }
    if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return false;
    return typeof obj.type === 'string' && obj.type.trim() !== '';
  }

  function isNonBoundaryUserText(text) {
    const raw = text == null ? '' : String(text);
    if (!raw.trim()) return false;
    // 🎯T362: a leaked protocol frame is never an owner turn boundary.
    if (isProtocolControlFrameText(raw)) return true;
    // Unwrap a single outer <user_query> so inject checks see inner body.
    let display = raw;
    const uq = raw.match(
      /^\s*<user_query(?:\s[^>]*)?>\s*([\s\S]*?)\s*<\/user_query>\s*$/i,
    );
    if (uq) display = uq[1];
    const trimmed = String(display).replace(/^\s+/, '');
    if (
      /<system-reminder[\s>]/i.test(raw) ||
      /<\/system-reminder>/i.test(raw) ||
      /<system-reminder[\s>]/i.test(display)
    ) {
      return true;
    }
    if (
      trimmed.indexOf('[Jevons fleet standing brief') === 0 ||
      /Jevons fleet standing brief/.test(display)
    ) {
      return true;
    }
    if (/^\[event:\s*[^\]]+\]/i.test(trimmed)) return true;
    if (trimmed.indexOf('[Daemon restart') === 0) return true;
    // Background-task / harness walls without full tags (ACP shape variants).
    if (/^Background task\b/i.test(trimmed)) return true;
    if (/\bbackground task\b/i.test(trimmed) && /completed/i.test(trimmed)) {
      return true;
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
      const content = userContentText(m); // 🎯T384: either content shape
      if (content) {
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
      const content = userContentText(m); // 🎯T384: either content shape
      if (content) {
        // 🎯T362: leaked protocol control frames are not owner turns either —
        // they must not re-arm working chrome or count as an owner send.
        if (isProtocolControlFrameText(content)) return state;
        // 🎯T329: harness inject / system-reminder is not an owner turn — do
        // not count, do not re-arm working chrome, never seals streams.
        if (isNonBoundaryUserText(content)) return state;
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

  /**
   * 🎯T327: whether main #messages working chrome should bind to this
   * owner send. Aside-routed turns (attention/target-aside wire, or any
   * composer path that marks routed) leave main idle — the aside pane
   * owns working state. Main must not show step count / fleet progress
   * / working dots for work that left main.
   *
   * @param {string} wireText accepted wire body
   * @param {(t: string) => boolean} [isAsideWire] true for attention/target-aside bodies
   * @param {{ routed?: boolean }|null|undefined} [opts]
   * @returns {boolean} true → open/keep main WorkingProgress; false → main idle
   */
  function shouldBindMainWorkingChrome(wireText, isAsideWire, opts) {
    if (opts && opts.routed === true) return false;
    const t = String(wireText == null ? '' : wireText);
    if (typeof isAsideWire === 'function' && isAsideWire(t)) return false;
    if (!t.trim()) return false;
    return true;
  }

  /**
   * 🎯T327: plan main working chrome after transport.accept.
   * Aside routes suppress main chrome for the whole in-flight turn
   * (level samples / tool re-arms must not re-open main).
   *
   * @param {string} wireText
   * @param {(t: string) => boolean} [isAsideWire]
   * @param {{ routed?: boolean }|null|undefined} [opts]
   * @returns {{ openMainWorking: boolean, suppressMainWorking: boolean }}
   */
  function planMainWorkingAfterSend(wireText, isAsideWire, opts) {
    const bind = shouldBindMainWorkingChrome(wireText, isAsideWire, opts);
    return {
      openMainWorking: bind,
      suppressMainWorking: !bind,
    };
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
    // 🎯T362: a protocol control frame is never an owner bubble, on send or
    // anywhere else. The wire and the composer share this path, so the send
    // side is gated here rather than trusting every caller to check.
    if (isProtocolControlFrameText(t)) {
      return { paint: false, text: t, reason: 'protocol-frame' };
    }
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
      // 🎯T362: never resurrect a leaked control frame as an owner bubble.
      if (isProtocolControlFrameText(t)) continue;
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

  // ── 🎯T329 shared display-line coalesce (main + RHS inspect) ─────
  // ONE model: stream_id join, terminal-stop seal only, tool_use never
  // seals, non-boundary users never seal. Open assistant is tracked by
  // _stream / _streamId on lines — NOT by adjacency to the last row
  // (inject nuggets may sit between fragments of the same turn).

  function copyDisplayLines(lines) {
    return (lines || []).map(function (l) {
      if (!l) return l;
      const c = { role: l.role, text: l.text };
      if (l.when !== undefined) c.when = l.when;
      if (l._stream) c._stream = true;
      if (l._streamId != null && l._streamId !== '') c._streamId = l._streamId;
      if (l._edgePending) c._edgePending = true;
      if (l._silent) c._silent = true;
      if (l.timestamp !== undefined) c.timestamp = l.timestamp;
      return c;
    });
  }

  /**
   * Index of the open assistant line that should absorb the next text.
   * Prefers matching stream_id; unlabeled frames join the last open stream.
   * @param {Array} lines
   * @param {string} sid
   * @returns {number}
   */
  function findOpenAssistantIndex(lines, sid) {
    const arr = lines || [];
    if (sid) {
      for (let i = arr.length - 1; i >= 0; i--) {
        const l = arr[i];
        if (l && l.role === 'assistant' && l._stream && String(l._streamId || '') === sid) {
          return i;
        }
      }
      return -1;
    }
    for (let i = arr.length - 1; i >= 0; i--) {
      const l = arr[i];
      if (l && l.role === 'assistant' && l._stream) return i;
    }
    return -1;
  }

  function sealOpenAssistantAt(lines, idx) {
    if (idx < 0 || !lines[idx]) return;
    delete lines[idx]._stream;
    delete lines[idx]._streamId;
    delete lines[idx]._edgePending;
    // Empty silent-only trackers are not display rows — drop on seal.
    if (lines[idx]._silent && !(lines[idx].text && String(lines[idx].text).length)) {
      lines.splice(idx, 1);
      return;
    }
    delete lines[idx]._silent;
  }

  function sealOpenAssistantBySid(lines, sid) {
    if (sid) {
      const idx = findOpenAssistantIndex(lines, sid);
      if (idx >= 0) sealOpenAssistantAt(lines, idx);
      return;
    }
    // Seal every open stream. Reverse so splice of empty silent trackers
    // does not skip the next open index.
    for (let i = lines.length - 1; i >= 0; i--) {
      if (lines[i] && lines[i].role === 'assistant' && lines[i]._stream) {
        sealOpenAssistantAt(lines, i);
      }
    }
  }

  function markSegmentEdges(lines, sid) {
    if (sid) {
      const idx = findOpenAssistantIndex(lines, sid);
      if (idx >= 0) lines[idx]._edgePending = true;
      return;
    }
    for (let i = 0; i < lines.length; i++) {
      if (lines[i] && lines[i].role === 'assistant' && lines[i]._stream) {
        lines[i]._edgePending = true;
      }
    }
  }

  function extractAssistantTextParts(m) {
    const content = m && m.message && m.message.content;
    if (!Array.isArray(content)) return { texts: [], hasNonText: false };
    const texts = [];
    let hasNonText = false;
    for (let i = 0; i < content.length; i++) {
      const c = content[i];
      if (!c) continue;
      if (c.type === 'text' && c.text) {
        texts.push(c.text);
      } else if (c.type && c.type !== 'text') {
        hasNonText = true;
      }
    }
    return { texts: texts, hasNonText: hasNonText };
  }

  /**
   * Apply one chat-wire event onto a display-line list (main + RHS).
   * Pure — no DOM. Same seal policy as applyChatEvent / T159 / T223 / T249.
   *
   * @param {Array<{role:string,text:string,_stream?:boolean,_streamId?:string,when?:number}>|null} lines
   * @param {object|null} event
   * @param {{now?: number}} [opts]
   * @returns {Array}
   */
  function applyLiveDisplayFrame(lines, event, opts) {
    const out = copyDisplayLines(lines);
    if (!event || !event.type) return out;

    const arrived = (function () {
      const w = event.when != null ? event.when : event.timestamp;
      if (w != null && w !== '') {
        if (typeof w === 'number' && isFinite(w) && w > 0) {
          return w < 1e11 ? Math.round(w * 1000) : Math.round(w);
        }
        const ms = Date.parse(String(w));
        if (isFinite(ms)) return ms;
      }
      if (opts && opts.now !== undefined) {
        const n = Number(opts.now);
        return isFinite(n) ? n : Date.now();
      }
      return Date.now();
    })();

    if (event.type === 'user') {
      // 🎯T384: either content shape. This is the sidebar/aside display model
      // — the exact place the owner's block-shaped turn would otherwise read
      // as '' and never reach the pane.
      const text = userContentText(event);
      if (!text) return out;
      // 🎯T362: a leaked protocol control frame is machine wire — never paint
      // it as an owner bubble on the live wire or on history hydrate, and
      // never let it seal an open assistant stream.
      if (isProtocolControlFrameText(text)) return out;
      const last = out[out.length - 1];
      // Dedupe consecutive owner echo (optimistic + WS); also consecutive inject.
      if (last && last.role === 'user') {
        const a = normalizeOwnerEchoText(last.text);
        const b = normalizeOwnerEchoText(text);
        if (a && b && a === b) return out;
      }
      // 🎯T329: harness inject / system-reminder paints (inspect nugget) but
      // never seals an open assistant stream.
      // Real owner user is a display turn boundary (history hydrate without
      // stop_reason / stream_id) — seal open streams so the next assistant
      // is a new bubble. Live applyChatEvent keeps T223 mid-stream policy
      // separately; this is the shared *display-line* model.
      if (!isNonBoundaryUserText(text)) {
        sealOpenAssistantBySid(out, '');
      }
      const line = { role: 'user', text: text, when: arrived };
      // 🎯T381: provenance rides the display line so a reload paints the turn
      // the same way the live wire did. Owner lines stay unmarked — the
      // absence of the field IS the owner default, everywhere.
      if (turnOriginOf(event) === TURN_ORIGIN_AGENT) line.origin = TURN_ORIGIN_AGENT;
      out.push(line);
      return out;
    }

    if (event.type === 'tool_result' || event.type === 'result') {
      markSegmentEdges(out, '');
      return out;
    }

    if (event.type === 'system') {
      sealOpenAssistantBySid(out, '');
      return out;
    }

    if (event.type === 'assistant') {
      const sid = streamIdOf(event);
      const parts = extractAssistantTextParts(event);
      if (parts.hasNonText) {
        markSegmentEdges(out, sid);
      }
      for (let p = 0; p < parts.texts.length; p++) {
        const text = parts.texts[p];
        if (!text) continue;
        let idx = findOpenAssistantIndex(out, sid);
        // 🎯T245 whole-stream silent: once this stream is silent, drop body
        // until terminal seal (do not paint later fragments as a new bubble).
        if (idx >= 0 && out[idx]._silent) {
          continue;
        }
        if (isSilentAssistantText(text)) {
          if (idx >= 0) {
            out[idx]._silent = true;
          } else {
            // Open silent tracker without a visible body so later fragments
            // of the same stream stay suppressed until seal.
            const silentLine = {
              role: 'assistant',
              text: '',
              when: arrived,
              _stream: true,
              _silent: true,
            };
            if (sid) silentLine._streamId = sid;
            out.push(silentLine);
          }
          continue;
        }
        if (idx >= 0) {
          const edge = !!(out[idx]._edgePending || p > 0);
          out[idx].text = edge
            ? joinAssistantSegments(out[idx].text, text)
            : appendAssistantStream(out[idx].text, text);
          out[idx]._edgePending = false;
          out[idx].when = arrived;
          if (sid) out[idx]._streamId = sid;
        } else {
          const line = {
            role: 'assistant',
            text: text,
            when: arrived,
            _stream: true,
          };
          if (sid) line._streamId = sid;
          out.push(line);
        }
      }
      if (isTerminalStop(event) || shouldClearWorking(event)) {
        sealOpenAssistantBySid(out, sid);
      }
      return out;
    }

    // progress / agent_note / tool_call chrome: do not seal, do not paint.
    return out;
  }

  /**
   * Batch applyLiveDisplayFrame over wire frames (history hydrate / main
   * coalesceTranscriptFrames). Optionally maps assistant→jevons for main.
   *
   * @param {Array} frames
   * @param {{roleMap?: {assistant?: string}, skipUser?: (text:string)=>boolean}} [opts]
   * @returns {Array<{role:string,text:string,when?:number,timestamp?:number}>}
   */
  function coalesceLiveDisplayFrames(frames, opts) {
    opts = opts || {};
    let lines = [];
    const list = Array.isArray(frames) ? frames : [];
    for (let i = 0; i < list.length; i++) {
      let m = list[i];
      if (typeof m === 'string') {
        try { m = JSON.parse(m); } catch (_) { continue; }
      }
      if (!m || typeof m !== 'object') continue;
      if (m.type === 'user') {
        const text = userContentText(m); // 🎯T384: either content shape
        if (text && typeof opts.skipUser === 'function' && opts.skipUser(text)) {
          // Aside wires etc.: still must not seal if we skip paint — apply
          // a no-op path: non-boundary already never seals; for aside we
          // skip entirely without sealing (T223/T329).
          continue;
        }
      }
      lines = applyLiveDisplayFrame(lines, m, opts);
    }
    const asstRole = (opts.roleMap && opts.roleMap.assistant) || 'assistant';
    return lines.map(function (l) {
      if (!l) return l;
      const role = l.role === 'assistant' ? asstRole : l.role;
      const row = { role: role, text: l.text };
      if (l.origin) row.origin = l.origin; // 🎯T381 provenance survives hydrate
      if (l.when !== undefined) {
        row.when = l.when;
        row.timestamp = l.when;
      }
      return row;
    }).filter(function (l) {
      return l && l.text != null && String(l.text).length > 0;
    });
  }

  return {
    stopReason,
    isTerminalStop,
    assistantTextBlocks,
    hasAssistantText,
    userContentText,
    isSilentAssistantText,
    isNonBoundaryUserText,
    // 🎯T362 leaked-protocol-frame gate (shared by main paint + hydrate)
    isProtocolControlFrameText,
    // 🎯T381 turn provenance: who spoke, and therefore how it paints
    turnOriginOf,
    bubblePaintsMarkdown,
    TURN_ORIGIN_OWNER,
    TURN_ORIGIN_AGENT,
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
    // 🎯T327: main chrome does not bind to aside-routed turns
    shouldBindMainWorkingChrome,
    planMainWorkingAfterSend,
    createTurnState,
    applyChatEvent,
    applyChatEvents,
    // 🎯T329 shared display coalesce (main + RHS)
    applyLiveDisplayFrame,
    coalesceLiveDisplayFrames,
    findOpenAssistantIndex,
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
