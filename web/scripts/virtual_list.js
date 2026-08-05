// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Shell+content transcript windowing (🎯T119).
//
// Data vs DOM (supersedes pure T57 infinite-scroll-as-data-throttle spirit):
//   - Full durable transcript lives in the browser (progressive fetch OK).
//   - Infinite scroll / windowing controls *render stress* only — not data caps.
//   - T57 still provides /api/history + WS recent replay; T119 loads remaining
//     history into client memory without requiring the owner to scroll for data.
//
// Model: every chunk has a persistent DOM *shell*; only near-viewport shells
// *materialize* rich content. Far shells freeze cached height at measured width
// and drop content. States: unmeasured | dematerialized | material.
//
// DOM-free core so Node can require(); browser integration in index.html.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.VirtualList = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Anticipation margin beyond the viewport (px).
  const DEFAULT_BUFFER = 800;

  // Placeholder height before first materialize (lazy measure — no render-all).
  const DEFAULT_ESTIMATE_HEIGHT = 72;
  // Rough line height for text-based estimates (14px * 1.6 + padding slack).
  const ESTIMATE_LINE_PX = 24;
  const ESTIMATE_PAD_PX = 28;
  const ESTIMATE_CHARS_PER_LINE = 72;

  const SHELL_UNMEASURED = 'unmeasured';
  const SHELL_DEMATERIALIZED = 'dematerialized';
  const SHELL_MATERIAL = 'material';

  // ── Viewport band ──────────────────────────────────────────────────

  // itemTops: array of { top, height } relative to container.
  // Returns indices that should be fully materialised.
  function visibleIndices(itemTops, scrollTop, clientHeight, buffer) {
    const buf = typeof buffer === 'number' ? buffer : DEFAULT_BUFFER;
    const viewTop = scrollTop - buf;
    const viewBot = scrollTop + clientHeight + buf;
    const out = [];
    for (let i = 0; i < itemTops.length; i++) {
      const it = itemTops[i];
      const top = it.top;
      const bot = top + (it.height || 0);
      if (bot >= viewTop && top <= viewBot) out.push(i);
    }
    return out;
  }

  function shouldMaterialize(top, height, scrollTop, clientHeight, buffer) {
    const buf = typeof buffer === 'number' ? buffer : DEFAULT_BUFFER;
    const viewTop = scrollTop - buf;
    const viewBot = scrollTop + clientHeight + buf;
    const bot = top + (height || 0);
    return bot >= viewTop && top <= viewBot;
  }

  // 🎯T246: strict viewport intersection (no materialize buffer).
  // Any overlap keeps a newly arrived auto-expanded bubble open.
  function anyPartInViewport(top, height, scrollTop, clientHeight) {
    const t = Number(top) || 0;
    const h = Number(height) || 0;
    const st = Number(scrollTop) || 0;
    const ch = Number(clientHeight) || 0;
    const bot = t + h;
    const viewBot = st + ch;
    return bot > st && t < viewBot;
  }

  // Fully scrolled above the fold (typical: newer messages pushed this up).
  function isFullyAboveViewport(top, height, scrollTop) {
    const t = Number(top) || 0;
    const h = Number(height) || 0;
    const st = Number(scrollTop) || 0;
    return t + h <= st;
  }

  // Auto-collapse policy for tall bubbles that were auto-expanded (T66/T246).
  // Latest always stays expanded. Manual toggle (_userToggled) never auto-collapses.
  // Non-latest auto-expanded may collapse only when fully outside the strict
  // viewport (any remaining pixel on-screen keeps it expanded).
  function shouldAutoCollapseOffScreen(opts) {
    const o = opts || {};
    if (o.isLatest) return false;
    if (o.userToggled) return false;
    if (!o.autoExpanded) return false;
    return !anyPartInViewport(o.top, o.height, o.scrollTop, o.clientHeight);
  }

  // 🎯T261: near the live end of the transcript (stick-to-bottom / post-pin).
  // Default slack matches typical jump-to-bottom hysteresis (~48px).
  const NEAR_END_SLACK_PX = 48;
  function isNearTranscriptEnd(scrollTop, scrollHeight, clientHeight, slackPx) {
    const slack = slackPx == null ? NEAR_END_SLACK_PX : Number(slackPx);
    const st = Number(scrollTop) || 0;
    const sh = Number(scrollHeight) || 0;
    const ch = Number(clientHeight) || 0;
    const s = Number.isFinite(slack) ? slack : NEAR_END_SLACK_PX;
    return st + ch >= sh - s;
  }

  // 🎯T261: while pinned near end, tall in-view bubbles (not user-toggled)
  // should be auto-expanded — not only the single latest message. Covers hard
  // reload after history pin and stick-to-bottom re-render. Mid-history free
  // scroll stays nearEnd=false so older rows are not force-opened.
  function shouldAutoExpandInView(opts) {
    const o = opts || {};
    if (o.userToggled) return false;
    if (!o.tall) return false;
    if (!o.nearEnd) return false;
    if (o.historyReplayActive) return false;
    return anyPartInViewport(o.top, o.height, o.scrollTop, o.clientHeight);
  }

  // Mid history-replay the viewport sits at the top; geometry is not the
  // post-pin end state. Auto-collapse must wait until pin (🎯T261).
  function shouldRunOffScreenCollapse(historyReplayActive) {
    return !historyReplayActive;
  }

  // Count how many items would stay materialised for a long list
  // scrolled to the bottom (oracle for "bounded DOM work").
  function materialisedCount(n, avgHeight, clientHeight, buffer) {
    const buf = typeof buffer === 'number' ? buffer : DEFAULT_BUFFER;
    const scrollTop = Math.max(0, n * avgHeight - clientHeight);
    const tops = [];
    for (let i = 0; i < n; i++) {
      tops.push({ top: i * avgHeight, height: avgHeight });
    }
    return visibleIndices(tops, scrollTop, clientHeight, buf).length;
  }

  // Which indices enter / leave the materialization band relative to a
  // previous material set (event-driven enter/leave, no polling).
  function enterLeaveBand(itemTops, scrollTop, clientHeight, buffer, previouslyMaterial) {
    const want = new Set(visibleIndices(itemTops, scrollTop, clientHeight, buffer));
    const prev = new Set(previouslyMaterial || []);
    const enter = [];
    const leave = [];
    want.forEach(function (i) {
      if (!prev.has(i)) enter.push(i);
    });
    prev.forEach(function (i) {
      if (!want.has(i)) leave.push(i);
    });
    enter.sort(function (a, b) { return a - b; });
    leave.sort(function (a, b) { return a - b; });
    return { enter: enter, leave: leave, material: Array.from(want).sort(function (a, b) { return a - b; }) };
  }

  // ── Size cache ─────────────────────────────────────────────────────

  // Entry: { height: number, width: number } — height valid only at that width.
  function createSizeCache() {
    return Object.create(null);
  }

  function recordSize(cache, id, height, width) {
    if (!cache || id == null) return null;
    const h = Number(height);
    const w = Number(width);
    if (!(h > 0) || !(w > 0)) return null;
    const entry = { height: h, width: w };
    cache[id] = entry;
    return entry;
  }

  function getSize(cache, id) {
    if (!cache || id == null) return null;
    const e = cache[id];
    if (!e || !(e.height > 0) || !(e.width > 0)) return null;
    return e;
  }

  // Horizontal resize invalidates all heights (wrapping changes natural height).
  // Returns number of entries cleared. Same width → no-op (0).
  function invalidateOnWidthChange(cache, previousWidth, nextWidth) {
    if (!cache) return 0;
    const prev = Number(previousWidth);
    const next = Number(nextWidth);
    if (!(next > 0)) return 0;
    if (prev === next) return 0;
    let n = 0;
    for (const k of Object.keys(cache)) {
      delete cache[k];
      n++;
    }
    return n;
  }

  // True when cache entry is usable at the given container width.
  function sizeValidAtWidth(entry, width) {
    if (!entry || !(entry.height > 0) || !(entry.width > 0)) return false;
    return Number(width) === Number(entry.width);
  }

  // Shell height after dematerialize: prefer valid cache, else estimate.
  function frozenShellHeight(entry, width, estimateHeight) {
    if (sizeValidAtWidth(entry, width)) return entry.height;
    const est = typeof estimateHeight === 'number' && estimateHeight > 0
      ? estimateHeight
      : DEFAULT_ESTIMATE_HEIGHT;
    return est;
  }

  // ── Shell state machine ────────────────────────────────────────────

  // material = content mounted; dematerialized = size known, content dropped;
  // unmeasured = shell exists, never measured at current width.
  function shellState(opts) {
    const o = opts || {};
    if (o.material) return SHELL_MATERIAL;
    if (o.hasValidSize) return SHELL_DEMATERIALIZED;
    return SHELL_UNMEASURED;
  }

  function nextStateOnEnterBand(state) {
    // Enter band always wants content (measure if needed).
    return SHELL_MATERIAL;
  }

  function nextStateOnLeaveBand(state, hasValidSize) {
    if (state === SHELL_MATERIAL && hasValidSize) return SHELL_DEMATERIALIZED;
    if (state === SHELL_MATERIAL && !hasValidSize) return SHELL_UNMEASURED;
    if (state === SHELL_DEMATERIALIZED) return SHELL_DEMATERIALIZED;
    return SHELL_UNMEASURED;
  }

  // After measure while material: transition path for leave.
  function afterMeasure(state) {
    if (state === SHELL_MATERIAL || state === SHELL_UNMEASURED) return SHELL_MATERIAL;
    return state;
  }

  // ── Lazy estimate (no render-all) ──────────────────────────────────

  function estimateHeightFromText(text, opts) {
    const o = opts || {};
    const charsPerLine = o.charsPerLine > 0 ? o.charsPerLine : ESTIMATE_CHARS_PER_LINE;
    const linePx = o.linePx > 0 ? o.linePx : ESTIMATE_LINE_PX;
    const pad = o.padPx != null ? o.padPx : ESTIMATE_PAD_PX;
    const minH = o.minHeight > 0 ? o.minHeight : DEFAULT_ESTIMATE_HEIGHT;
    const maxH = o.maxHeight > 0 ? o.maxHeight : 480;
    const s = text == null ? '' : String(text);
    if (!s) return minH;
    const explicitLines = s.split(/\n/).length;
    const wrapLines = Math.ceil(s.length / charsPerLine);
    const lines = Math.max(explicitLines, wrapLines, 1);
    const h = lines * linePx + pad;
    return Math.max(minH, Math.min(maxH, h));
  }

  // ── Recent-first hydrate plan ──────────────────────────────────────
  // Land on latest end: materialize only the end band first; older shells
  // stay unmeasured/estimated (never parade old→new through viewport).

  function recentFirstMaterializePlan(n, avgHeight, clientHeight, buffer) {
    const count = Math.max(0, n | 0);
    const h = avgHeight > 0 ? avgHeight : DEFAULT_ESTIMATE_HEIGHT;
    const ch = clientHeight > 0 ? clientHeight : 600;
    const buf = typeof buffer === 'number' ? buffer : DEFAULT_BUFFER;
    if (count === 0) {
      return { materialIndices: [], unmeasuredIndices: [], scrollTop: 0 };
    }
    const totalH = count * h;
    const scrollTop = Math.max(0, totalH - ch);
    const tops = [];
    for (let i = 0; i < count; i++) {
      tops.push({ top: i * h, height: h });
    }
    const materialIndices = visibleIndices(tops, scrollTop, ch, buf);
    const matSet = new Set(materialIndices);
    const unmeasuredIndices = [];
    for (let i = 0; i < count; i++) {
      if (!matSet.has(i)) unmeasuredIndices.push(i);
    }
    return {
      materialIndices: materialIndices,
      unmeasuredIndices: unmeasuredIndices,
      scrollTop: scrollTop,
      // Oracle: end of list is in the material set when scrolled to bottom.
      landsOnLatest: materialIndices.indexOf(count - 1) >= 0,
    };
  }

  // Startup plan: do NOT schedule full N materializations (lazy measure).
  function startupMaterializeBudget(n, avgHeight, clientHeight, buffer) {
    const plan = recentFirstMaterializePlan(n, avgHeight, clientHeight, buffer);
    return {
      mustMaterialize: plan.materialIndices.length,
      mayDefer: plan.unmeasuredIndices.length,
      total: n | 0,
      // Hard rule: material set bounded by viewport+buffer, not N.
      isLazy: plan.materialIndices.length < Math.max(1, n | 0) || (n | 0) <= plan.materialIndices.length,
    };
  }

  // ── Progressive history fetch plan (data, not scroll-gated) ────────
  // T119: after recent-first hydrate, fetch remaining history into client
  // memory without requiring owner to scroll for data.

  function progressiveHistoryPages(oldestIndex, pageLimit) {
    const limit = pageLimit > 0 ? pageLimit : 200;
    let end = Math.max(0, oldestIndex | 0);
    const pages = [];
    while (end > 0) {
      const start = Math.max(0, end - limit);
      pages.push({ end: end, limit: end - start, start: start });
      end = start;
    }
    return pages;
  }

  // ── Whole-chunk coalescing ─────────────────────────────────────────
  // History units are only complete cohesive chunks: whole owner request,
  // whole assistant response (coalesced consecutive assistant text frames).
  // Never emit partial mid-markdown fragments as separate display units.

  // 🎯T161: structural segment join only (no content sniff). Prefer
  // ChatEvents when available; local twin for isolated Node require.
  function joinAssistantSegmentsLocal(prev, next) {
    if (typeof ChatEvents !== 'undefined' && ChatEvents.joinAssistantSegments) {
      return ChatEvents.joinAssistantSegments(prev, next);
    }
    prev = String(prev == null ? '' : prev);
    next = String(next == null ? '' : next);
    if (!prev) return next;
    if (!next) return prev;
    if (/[\n\r]$/.test(prev) || /^[\n\r]/.test(next)) return prev + next;
    return prev + '\n\n' + next;
  }

  function appendAssistantStreamLocal(prev, next) {
    if (typeof ChatEvents !== 'undefined' && ChatEvents.appendAssistantStream) {
      return ChatEvents.appendAssistantStream(prev, next);
    }
    return String(prev == null ? '' : prev) + String(next == null ? '' : next);
  }

  function extractAssistantText(frame) {
    if (!frame || frame.type !== 'assistant') return '';
    const content = frame.message && frame.message.content;
    if (!Array.isArray(content)) return '';
    // Multi-block text parts are known segments → structural join.
    let txt = '';
    let nText = 0;
    for (let i = 0; i < content.length; i++) {
      const c = content[i];
      if (c && c.type === 'text' && typeof c.text === 'string' && c.text) {
        txt = nText === 0
          ? c.text
          : joinAssistantSegmentsLocal(txt, c.text);
        nText += 1;
      }
    }
    return txt;
  }

  function frameHasNonTextAssistant(frame) {
    if (!frame || frame.type !== 'assistant') return false;
    const content = frame.message && frame.message.content;
    if (!Array.isArray(content)) return false;
    for (let i = 0; i < content.length; i++) {
      const c = content[i];
      if (c && c.type && c.type !== 'text') return true;
    }
    return false;
  }

  function extractUserText(frame) {
    if (!frame || frame.type !== 'user') return '';
    const c = frame.message && frame.message.content;
    if (typeof c === 'string') return c;
    return '';
  }

  function streamIdOfFrame(m) {
    if (typeof ChatEvents !== 'undefined' && ChatEvents.streamIdOf) {
      return ChatEvents.streamIdOf(m);
    }
    if (!m) return '';
    const id = m.stream_id != null ? m.stream_id : m.streamId;
    return id == null ? '' : String(id).trim();
  }

  function isSilentTextLocal(text) {
    if (typeof ChatEvents !== 'undefined' && ChatEvents.isSilentAssistantText) {
      return ChatEvents.isSilentAssistantText(text);
    }
    const t = String(text == null ? '' : text).trim();
    if (!t) return false;
    const lower = t.toLowerCase();
    if (lower.startsWith('[silent]')) return true;
    const head = t.length > 80 ? t.slice(0, 80) : t;
    const lines = head.split('\n');
    for (let i = 0; i < lines.length; i++) {
      if (lines[i].trim().toLowerCase().startsWith('[silent]')) return true;
    }
    return false;
  }

  // 🎯T250/T264: aside wire user bodies never become main transcript chunks.
  function isAsideWireUserTextLocal(text) {
    if (typeof AttentionThreads !== 'undefined') {
      if (AttentionThreads.isAsideWireUserText) {
        return AttentionThreads.isAsideWireUserText(text);
      }
      if (AttentionThreads.looksLikeAsideWireMarker) {
        return AttentionThreads.looksLikeAsideWireMarker(text);
      }
    }
    const t = String(text == null ? '' : text).replace(/^\s*(?:\[image:\s*[^\]]*\]\s*)+/i, '');
    if (/^\s*\[attention\s*:/i.test(t)) return true;
    if (/^\s*\[target-aside\s*:/i.test(t)) return true;
    return false;
  }

  function isTerminalFrameLocal(m) {
    if (typeof ChatEvents !== 'undefined' && ChatEvents.isTerminalStop) {
      return ChatEvents.isTerminalStop(m);
    }
    if (!m || m.type !== 'assistant') return false;
    const msg = m.message || {};
    const stop = msg.stop_reason || msg.stopReason || '';
    return stop === 'end_turn' || stop === 'stop_sequence' || stop === 'max_tokens';
  }

  // frames: array of wire objects (or JSON strings). Returns whole chunks:
  //   { role: 'user'|'jevons', text: string, timestamp?: number }
  // 🎯T161: bare-concat continuous text frames; segment-join after
  // tool_use/tool_result gaps (protocol edge, not content sniff).
  // 🎯T223 / T245: join by stream_id when present; terminal stop_reason
  // seals a stream so the next assistant turn is a new bubble. agent_note
  // / system chrome between turns must NOT glue distinct replies (owner
  // screenshot: pure [silent] + next visible across agent_note).
  // 🎯T238 / T245: silent bodies never become display chunks.
  // 🎯T250: aside wire user bodies never become main display chunks.
  function coalesceTranscriptFrames(frames) {
    const out = [];
    // sid → open chunk ref already pushed to out (still accepting text).
    const openById = Object.create(null);
    // Unlabeled open stream (legacy frames without stream_id).
    let legacyOpen = null;
    const segmentEdgeById = Object.create(null);
    let segmentEdgeLegacy = false;
    // Once a stream is silent, drop further body until terminal seal.
    const silentById = Object.create(null);
    let legacySilent = false;

    function markSegmentEdges() {
      Object.keys(openById).forEach(function (id) {
        if (openById[id]) segmentEdgeById[id] = true;
      });
      if (legacyOpen) segmentEdgeLegacy = true;
    }

    function sealStream(sid) {
      if (sid) {
        delete openById[sid];
        delete segmentEdgeById[sid];
        delete silentById[sid];
      } else {
        legacyOpen = null;
        segmentEdgeLegacy = false;
        legacySilent = false;
      }
    }

    function sealAll() {
      Object.keys(openById).forEach(function (id) {
        delete openById[id];
        delete segmentEdgeById[id];
        delete silentById[id];
      });
      legacyOpen = null;
      segmentEdgeLegacy = false;
      legacySilent = false;
    }

    const list = Array.isArray(frames) ? frames : [];
    for (let i = 0; i < list.length; i++) {
      let m = list[i];
      if (typeof m === 'string') {
        try { m = JSON.parse(m); } catch (_) { continue; }
      }
      if (!m || typeof m !== 'object') continue;
      const ts = m.timestamp ? new Date(m.timestamp).getTime() : undefined;

      if (m.type === 'user') {
        const text = extractUserText(m);
        if (!text) continue;
        sealAll();
        // 🎯T250: attention/target-aside wires → sidebar only, not main.
        if (isAsideWireUserTextLocal(text)) continue;
        out.push({ role: 'user', text: text, timestamp: ts });
        continue;
      }

      if (m.type === 'tool_result' || m.type === 'result') {
        markSegmentEdges();
        // omitted from transcript window units
        continue;
      }

      if (m.type === 'system') {
        // Full settle — seal every open stream (matches live shouldClearWorking).
        sealAll();
        continue;
      }

      if (m.type === 'assistant') {
        const sid = streamIdOfFrame(m);
        if (frameHasNonTextAssistant(m)) {
          if (sid && openById[sid]) segmentEdgeById[sid] = true;
          else if (!sid && legacyOpen) segmentEdgeLegacy = true;
        }
        const text = extractAssistantText(m);
        if (text) {
          // 🎯T245 whole-stream silent: drop body for this stream.
          let silent = sid ? !!silentById[sid] : legacySilent;
          if (!silent && isSilentTextLocal(text)) {
            if (sid) silentById[sid] = true;
            else legacySilent = true;
            silent = true;
          }
          if (!silent) {
            if (sid) {
              let pending = openById[sid];
              if (pending) {
                const edge = !!segmentEdgeById[sid];
                pending.text = edge
                  ? joinAssistantSegmentsLocal(pending.text, text)
                  : appendAssistantStreamLocal(pending.text, text);
                segmentEdgeById[sid] = false;
                if (ts != null) pending.timestamp = ts;
              } else {
                pending = { role: 'jevons', text: text, timestamp: ts };
                out.push(pending);
                openById[sid] = pending;
                segmentEdgeById[sid] = false;
              }
            } else if (legacyOpen) {
              const edge = segmentEdgeLegacy;
              legacyOpen.text = edge
                ? joinAssistantSegmentsLocal(legacyOpen.text, text)
                : appendAssistantStreamLocal(legacyOpen.text, text);
              segmentEdgeLegacy = false;
              if (ts != null) legacyOpen.timestamp = ts;
            } else {
              legacyOpen = { role: 'jevons', text: text, timestamp: ts };
              out.push(legacyOpen);
              segmentEdgeLegacy = false;
            }
          }
        }
        // Terminal always seals — empty end_turn must not leave pending open
        // so a later assistant turn cannot glue across agent_notes (T245).
        if (isTerminalFrameLocal(m)) {
          sealStream(sid || '');
        }
        continue;
      }
      // agent_note / status / other chrome omitted from transcript window units
      // and do NOT seal open streams (join is by stream_id / terminal only).
    }
    return out;
  }

  // Reject partial markdown "frames" as display units: a chunk must be a
  // complete string unit we already hold, not a streaming mid-parse slice
  // treated as a separate history row. Policy helper for tests + callers.
  function isWholeChunk(chunk) {
    if (!chunk || typeof chunk !== 'object') return false;
    if (chunk.role !== 'user' && chunk.role !== 'jevons') return false;
    if (typeof chunk.text !== 'string' || chunk.text.length === 0) return false;
    // Explicit partial markers are rejected (streaming stubs).
    if (chunk.partial === true || chunk.incomplete === true) return false;
    return true;
  }

  function filterWholeChunks(chunks) {
    return (Array.isArray(chunks) ? chunks : []).filter(isWholeChunk);
  }

  // ── Jump-to-bottom policy ──────────────────────────────────────────
  // One step (hotkey + affordance); deliberately no jump-to-top.

  function jumpPolicy() {
    return {
      hasJumpToBottom: true,
      hasJumpToTop: false,
      // End key, or Cmd/Ctrl+ArrowDown (Mac/Windows).
      hotkeys: ['End', 'Meta+ArrowDown', 'Ctrl+ArrowDown'],
    };
  }

  function shouldShowJumpFab(followMode, isAtBottom) {
    // Show when owner is free-scrolling away from the live end.
    if (isAtBottom) return false;
    return followMode === 'free' || followMode === 'track' && !isAtBottom;
  }

  function isJumpToBottomHotkey(key, mods) {
    const m = mods || {};
    if (key === 'End' && !m.altKey && !m.shiftKey) return true;
    if (key === 'ArrowDown' && (m.metaKey || m.ctrlKey) && !m.altKey && !m.shiftKey) return true;
    return false;
  }

  // ── Resize ─────────────────────────────────────────────────────────

  function shouldInvalidateSizeCache(previousWidth, nextWidth) {
    const prev = Number(previousWidth);
    const next = Number(nextWidth);
    if (!(next > 0)) return false;
    if (!(prev > 0)) return true; // first known width after unknown
    return prev !== next;
  }

  // Near-viewport first remeasure order after width invalidate.
  function remeasureOrder(itemTops, scrollTop, clientHeight, buffer) {
    const near = visibleIndices(itemTops, scrollTop, clientHeight, buffer);
    const nearSet = new Set(near);
    const far = [];
    for (let i = 0; i < itemTops.length; i++) {
      if (!nearSet.has(i)) far.push(i);
    }
    return { immediate: near, deferred: far };
  }

  // ── Events that drive residency (documentation + test oracle) ──────

  function residencyDrivers() {
    return [
      'scroll',
      'intersection',
      'resize',
      'chunk_append',
      'chunk_seal',
      'jump_to_bottom',
    ];
  }

  // ── History replay scroll (reload / reconnect) ─────────────────────
  // WS chronological replay must not stick-to-bottom per message (parade).
  // Suppress pin while replayActive; one pin after history_meta / idle end.

  const REPLAY_IDLE_END_MS = 150;

  function shouldSuppressPinDuringReplay(replayActive) {
    return !!replayActive;
  }

  // Final scrollTop after one pin to the live end.
  function finalPinScrollTop(scrollHeight, clientHeight) {
    const sh = Math.max(0, Number(scrollHeight) || 0);
    const ch = Math.max(0, Number(clientHeight) || 0);
    return Math.max(0, sh - ch);
  }

  return {
    DEFAULT_BUFFER: DEFAULT_BUFFER,
    DEFAULT_ESTIMATE_HEIGHT: DEFAULT_ESTIMATE_HEIGHT,
    SHELL_UNMEASURED: SHELL_UNMEASURED,
    SHELL_DEMATERIALIZED: SHELL_DEMATERIALIZED,
    SHELL_MATERIAL: SHELL_MATERIAL,

    visibleIndices: visibleIndices,
    shouldMaterialize: shouldMaterialize,
    anyPartInViewport: anyPartInViewport,
    isFullyAboveViewport: isFullyAboveViewport,
    shouldAutoCollapseOffScreen: shouldAutoCollapseOffScreen,
    NEAR_END_SLACK_PX: NEAR_END_SLACK_PX,
    isNearTranscriptEnd: isNearTranscriptEnd,
    shouldAutoExpandInView: shouldAutoExpandInView,
    shouldRunOffScreenCollapse: shouldRunOffScreenCollapse,
    materialisedCount: materialisedCount,
    enterLeaveBand: enterLeaveBand,

    createSizeCache: createSizeCache,
    recordSize: recordSize,
    getSize: getSize,
    invalidateOnWidthChange: invalidateOnWidthChange,
    sizeValidAtWidth: sizeValidAtWidth,
    frozenShellHeight: frozenShellHeight,

    shellState: shellState,
    nextStateOnEnterBand: nextStateOnEnterBand,
    nextStateOnLeaveBand: nextStateOnLeaveBand,
    afterMeasure: afterMeasure,

    estimateHeightFromText: estimateHeightFromText,
    recentFirstMaterializePlan: recentFirstMaterializePlan,
    startupMaterializeBudget: startupMaterializeBudget,
    progressiveHistoryPages: progressiveHistoryPages,

    coalesceTranscriptFrames: coalesceTranscriptFrames,
    extractAssistantText: extractAssistantText,
    extractUserText: extractUserText,
    isWholeChunk: isWholeChunk,
    filterWholeChunks: filterWholeChunks,

    jumpPolicy: jumpPolicy,
    shouldShowJumpFab: shouldShowJumpFab,
    isJumpToBottomHotkey: isJumpToBottomHotkey,

    shouldInvalidateSizeCache: shouldInvalidateSizeCache,
    remeasureOrder: remeasureOrder,
    residencyDrivers: residencyDrivers,

    // 🎯T119.1 / reload-reconnect: suppress stick-to-bottom during WS
    // chronological replay; one pin after history_meta (or idle end).
    shouldSuppressPinDuringReplay: shouldSuppressPinDuringReplay,
    finalPinScrollTop: finalPinScrollTop,
    // Idle end fallback ms if history_meta never arrives (empty log).
    REPLAY_IDLE_END_MS: REPLAY_IDLE_END_MS,
  };
}));
