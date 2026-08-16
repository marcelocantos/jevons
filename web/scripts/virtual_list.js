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

  // Overlap of [top, top+height) with the strict viewport (px). 🎯T341.
  function visibleOverlapPx(top, height, scrollTop, clientHeight) {
    const t = Number(top) || 0;
    const h = Number(height) || 0;
    const st = Number(scrollTop) || 0;
    const ch = Number(clientHeight) || 0;
    if (h <= 0 || ch <= 0) return 0;
    const bot = t + h;
    const viewBot = st + ch;
    const oTop = Math.max(t, st);
    const oBot = Math.min(bot, viewBot);
    return Math.max(0, oBot - oTop);
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
  // 🎯T341: require real ink in view to auto-expand — a 1px edge contact
  // plus expand→pin→fully-above→collapse is the continuous jiggle loop.
  // Collapse still uses fully-outside (overlap 0), so enter≥8 / leave=0
  // hysteresis stops band-edge thrash.
  const MIN_VISIBLE_PX_FOR_AUTO_EXPAND = 8;
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
  // 🎯T341: min visible px (default MIN_VISIBLE_PX_FOR_AUTO_EXPAND) for enter.
  function shouldAutoExpandInView(opts) {
    const o = opts || {};
    if (o.userToggled) return false;
    if (!o.tall) return false;
    if (!o.nearEnd) return false;
    if (o.historyReplayActive) return false;
    const minPx = o.minVisiblePx != null
      ? Number(o.minVisiblePx)
      : MIN_VISIBLE_PX_FOR_AUTO_EXPAND;
    const need = Number.isFinite(minPx) && minPx > 0 ? minPx : MIN_VISIBLE_PX_FOR_AUTO_EXPAND;
    return visibleOverlapPx(o.top, o.height, o.scrollTop, o.clientHeight) >= need;
  }

  // Mid history-replay the viewport sits at the top; geometry is not the
  // post-pin end state. Auto-collapse must wait until pin (🎯T261).
  function shouldRunOffScreenCollapse(historyReplayActive) {
    return !historyReplayActive;
  }

  // 🎯T341: stick-to-bottom pin gate. Micro height / scrollTop noise (subpixel
  // remat settle, 1px shell thrash) must not rewrite scrollTop every frame —
  // that is the continuous whole-page text jiggle while "idle".
  // force: true always pins (send, jump-to-bottom, stream growth path).
  const PIN_HEIGHT_THRESHOLD_PX = 2;
  function shouldPinScroll(opts) {
    const o = opts || {};
    if (o.force) return true;
    const thr = o.threshold != null ? Number(o.threshold) : PIN_HEIGHT_THRESHOLD_PX;
    const t = Number.isFinite(thr) && thr >= 0 ? thr : PIN_HEIGHT_THRESHOLD_PX;
    const prevH = Number(o.prevScrollHeight) || 0;
    const nextH = Number(o.nextScrollHeight) || 0;
    if (Math.abs(nextH - prevH) >= t) return true;
    const ch = Number(o.clientHeight) || 0;
    const st = Number(o.scrollTop) || 0;
    const target = finalPinScrollTop(nextH, ch);
    return Math.abs(st - target) >= t;
  }

  // 🎯T341: ignore sub-pixel container width jitter (scrollbar gutter, flex).
  const WIDTH_INVALIDATE_EPS_PX = 1;

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

  // 🎯T119.3: attached .msg count is bounded by the viewport band.
  // T119 still keeps every chunk in a JS array; T119.2 windowed measure;
  // this stops insertBefore of one shell per turn (the 11k-node freeze).
  const MIN_ATTACHED_SHELLS = 64;
  const ATTACHED_BAND_SCREENS = 3;

  function maxAttachedShells(clientHeight, avgHeight) {
    const ch = clientHeight > 0 ? Number(clientHeight) : 600;
    const h = avgHeight > 0 ? Number(avgHeight) : DEFAULT_ESTIMATE_HEIGHT;
    const byView = Math.ceil((ch * ATTACHED_BAND_SCREENS) / Math.max(1, h)) + 8;
    return Math.max(MIN_ATTACHED_SHELLS, byView);
  }

  function shouldAttachHistoryPage(attachedCount, incomingCount, maxAttach) {
    const have = Math.max(0, attachedCount | 0);
    const inc = Math.max(0, incomingCount | 0);
    const max = maxAttach > 0 ? maxAttach : MIN_ATTACHED_SHELLS;
    return have + inc <= max;
  }

  // Hydrate pages walk backward, so each new page is older than what we
  // already hold. Prepend incoming onto the detached (oldest-first) list.
  function prependDetachedRecords(existing, incoming) {
    const have = Array.isArray(existing) ? existing : [];
    const add = Array.isArray(incoming) ? incoming : [];
    if (!add.length) return have.slice();
    if (!have.length) return add.slice();
    return add.concat(have);
  }

  function spacerPxForRecords(records) {
    let h = 0;
    const list = Array.isArray(records) ? records : [];
    for (let i = 0; i < list.length; i++) {
      const r = list[i];
      if (!r) continue;
      if (r.estHeight > 0) h += r.estHeight;
      else h += estimateHeightFromText(r.text || '');
    }
    return h;
  }

  // Newest detached records sit at the end (just older than the DOM head).
  function takeNewestDetached(records, n) {
    const list = Array.isArray(records) ? records.slice() : [];
    const count = Math.max(0, n | 0);
    if (!count || !list.length) return { take: [], remain: list };
    const split = Math.max(0, list.length - count);
    return { take: list.slice(split), remain: list.slice(0, split) };
  }

  function recordsFromChunks(chunks, estimateFn) {
    const est = typeof estimateFn === 'function' ? estimateFn : estimateHeightFromText;
    const list = Array.isArray(chunks) ? chunks : [];
    const out = [];
    for (let i = 0; i < list.length; i++) {
      const c = list[i];
      if (!c) continue;
      const text = c.text == null ? '' : String(c.text);
      out.push({
        role: c.role,
        text: text,
        timestamp: c.timestamp,
        origin: c.origin,
        estHeight: est(text),
      });
    }
    return out;
  }

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
  // 🎯T329: ONE path — ChatEvents.coalesceLiveDisplayFrames (same model as
  // applyInspectLiveFrame / applyLiveDisplayFrame). No dual-stack residual.
  // 🎯T161/T223/T245/T250 policies live in ChatEvents; main only maps roles
  // and skips aside-wire user bodies.
  function coalesceTranscriptFrames(frames) {
    if (typeof ChatEvents !== 'undefined' && ChatEvents.coalesceLiveDisplayFrames) {
      return ChatEvents.coalesceLiveDisplayFrames(frames, {
        roleMap: { assistant: 'jevons' },
        skipUser: function (text) {
          // 🎯T250: attention/target-aside wires → sidebar only, not main.
          if (isAsideWireUserTextLocal(text)) return true;
          // 🎯T329: harness injects are non-boundaries; omit from main window.
          if (ChatEvents.isNonBoundaryUserText && ChatEvents.isNonBoundaryUserText(text)) {
            return true;
          }
          return false;
        },
      });
    }
    // Node require path: load ChatEvents sibling when not on window.
    if (typeof module === 'object' && module.exports) {
      try {
        const CE = require('./chat_events.js');
        if (CE && CE.coalesceLiveDisplayFrames) {
          return CE.coalesceLiveDisplayFrames(frames, {
            roleMap: { assistant: 'jevons' },
            skipUser: function (text) {
              if (isAsideWireUserTextLocal(text)) return true;
              if (CE.isNonBoundaryUserText && CE.isNonBoundaryUserText(text)) return true;
              return false;
            },
          });
        }
      } catch (_) { /* fall through empty */ }
    }
    return [];
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
    // 🎯T341: sub-pixel / scrollbar-gutter jitter must not nuke the size cache.
    return Math.abs(prev - next) >= WIDTH_INVALIDATE_EPS_PX;
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

  // ── Progressive rematerialize (🎯T336 Page Up) ─────────────────────
  // Rematerialize is expensive (parseAssistantMarkdown / renderBody). Page Up
  // brings many dematerialized shells into the band at once; Page Down stays
  // near the live end where shells are already material — one-sided lag.
  // Bound work per animation frame; paint strict-viewport first.

  // Soft cap: enough for a short screenful, not a full 800px buffer thrash.
  const REMATERIALIZE_PER_FRAME = 6;

  // Distance-based priority: 0 = any pixel in strict viewport (paint first);
  // larger = farther outside (buffer / progressive fill).
  function rematerializePriority(top, height, scrollTop, clientHeight) {
    const t = Number(top) || 0;
    const h = Number(height) || 0;
    const st = Number(scrollTop) || 0;
    const ch = Number(clientHeight) || 0;
    const bot = t + h;
    const viewBot = st + ch;
    if (bot > st && t < viewBot) return 0;
    if (bot <= st) return 1 + (st - bot);
    return 1 + (t - viewBot);
  }

  /**
   * Plan one rAF of rematerializations.
   * pending: [{ index?, top, height, ...el }] — shells that want content.
   * Returns thisFrame (≤ maxPerFrame, viewport-first) and remaining.
   * syncWouldThrash is the oracle flag: unbounded sync rematerialize would
   * exceed the per-frame budget (Page-Up thrash).
   */
  function planRematerializeFrame(pending, scrollTop, clientHeight, maxPerFrame) {
    const max = maxPerFrame > 0 ? maxPerFrame : REMATERIALIZE_PER_FRAME;
    const list = Array.isArray(pending) ? pending.slice() : [];
    list.sort(function (a, b) {
      const pa = rematerializePriority(a.top, a.height, scrollTop, clientHeight);
      const pb = rematerializePriority(b.top, b.height, scrollTop, clientHeight);
      if (pa !== pb) return pa - pb;
      const ia = a.index != null ? a.index : 0;
      const ib = b.index != null ? b.index : 0;
      return ia - ib;
    });
    return {
      thisFrame: list.slice(0, max),
      remaining: list.slice(max),
      syncWouldThrash: list.length > max,
      maxPerFrame: max,
    };
  }

  /**
   * Simulate Page-Up-equivalent band enter after scrolling up by ~0.8 viewport
   * from a bottom-pinned material set. Oracle for thrash: enter count is large
   * while a progressive plan bounds thisFrame ≤ REMATERIALIZE_PER_FRAME.
   *
   * opts: { n, avgHeight, clientHeight, buffer, pageFactor, maxPerFrame }
   */
  function pageUpRematerializeBudget(opts) {
    const o = opts || {};
    const n = Math.max(0, o.n | 0);
    const h = o.avgHeight > 0 ? o.avgHeight : DEFAULT_ESTIMATE_HEIGHT;
    const ch = o.clientHeight > 0 ? o.clientHeight : 600;
    const buf = typeof o.buffer === 'number' ? o.buffer : DEFAULT_BUFFER;
    const pageFactor = o.pageFactor > 0 ? o.pageFactor : 0.8;
    const maxPerFrame = o.maxPerFrame > 0 ? o.maxPerFrame : REMATERIALIZE_PER_FRAME;
    if (n === 0) {
      return {
        enterCount: 0,
        thisFrameCount: 0,
        syncWouldThrash: false,
        maxPerFrame: maxPerFrame,
      };
    }
    const tops = [];
    for (let i = 0; i < n; i++) {
      tops.push({ top: i * h, height: h });
    }
    const totalH = n * h;
    const bottomScroll = Math.max(0, totalH - ch);
    const materialAtBottom = new Set(visibleIndices(tops, bottomScroll, ch, buf));
    // Page Up: scroll up by ~0.8 viewport (matches index.html PageUp handler).
    const pageUpScroll = Math.max(0, bottomScroll - ch * pageFactor);
    const wantAfter = visibleIndices(tops, pageUpScroll, ch, buf);
    const enter = [];
    for (let i = 0; i < wantAfter.length; i++) {
      const idx = wantAfter[i];
      if (!materialAtBottom.has(idx)) {
        enter.push({
          index: idx,
          top: tops[idx].top,
          height: tops[idx].height,
        });
      }
    }
    const plan = planRematerializeFrame(enter, pageUpScroll, ch, maxPerFrame);
    return {
      enterCount: enter.length,
      thisFrameCount: plan.thisFrame.length,
      remainingCount: plan.remaining.length,
      syncWouldThrash: plan.syncWouldThrash,
      maxPerFrame: maxPerFrame,
      pageUpScroll: pageUpScroll,
      bottomScroll: bottomScroll,
    };
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

  // ── Reload hydrate: end-first, lazy, parade-proof (🎯T347) ─────────
  // Owner regression: hard-reload of a long chat painted every replayed turn
  // old→new (full markdown parse each) and, once per-frame work opened >150ms
  // gaps, the idle fallback ended suppression mid-burst — every remaining
  // frame then stick-to-bottom pinned: a ~minute-long upward scroll parade.
  // Model: (a) replay appends are lazy shells (zero parse before pin),
  // (b) the single pin at history_meta triggers an end-band materialize only,
  // (c) frames arriving while the connection still awaits history_meta always
  // re-enter suppression — the fallback can never flip the burst into
  // per-message pin mode.

  // A server that never sends history_meta must degrade to live behaviour
  // (otherwise streaming would stay pin-suppressed forever) — time-cap the
  // re-entry window well above any sane replay burst.
  const REPLAY_REENTER_MAX_MS = 30000;

  function shouldReenterReplay(opts) {
    const o = opts || {};
    if (!o.awaitingHistoryMeta) return false;
    if (o.historyReplayActive) return false;
    const ms = Number(o.msSinceConnect);
    const max = o.maxMs != null ? Number(o.maxMs) : REPLAY_REENTER_MAX_MS;
    if (Number.isFinite(ms) && Number.isFinite(max) && ms > max) return false;
    return true;
  }

  // Replay-burst appends stay lazy shells; paint is post-pin, band-only.
  function shouldPaintOnReplayAppend(historyReplayActive) {
    return !historyReplayActive;
  }

  // Materialization is deferred wholesale during the burst — the pin at
  // history_meta schedules the one end-band pass.
  function shouldVirtualizeDuringReplay(historyReplayActive) {
    return !historyReplayActive;
  }

  /**
   * Reload/hydrate oracle: simulate a chronological replay of n turns and the
   * single end pin. Policy knobs mirror the product wiring so the hermetic
   * test proves both directions:
   *   defaults (lazyAppend + suppressPin) → no mid-hydrate scrollTop climb,
   *     zero paints during replay, end-band-bounded materialize, dist-to-end 0;
   *   suppressPin:false → midHydrateClimbs > 0 (the parade — caught);
   *   lazyAppend:false → paintedDuringReplay === n (the slow full-list
   *     materialize behind the ~60s settle — caught).
   */
  function replayHydrateTrace(opts) {
    const o = opts || {};
    const n = Math.max(0, o.n | 0);
    const h = o.avgHeight > 0 ? o.avgHeight : DEFAULT_ESTIMATE_HEIGHT;
    const ch = o.clientHeight > 0 ? o.clientHeight : 600;
    const buf = typeof o.buffer === 'number' ? o.buffer : DEFAULT_BUFFER;
    const lazyAppend = o.lazyAppend !== false;
    const suppressPin = o.suppressPinDuringReplay !== false;
    let scrollTop = 0;
    let climbs = 0;
    const scrollTops = [];
    for (let i = 1; i <= n; i++) {
      if (!suppressPin) {
        // Per-message stick-to-bottom during the burst (the regression).
        const next = Math.max(0, i * h - ch);
        if (next > scrollTop) { scrollTop = next; climbs++; }
      }
      scrollTops.push(scrollTop);
    }
    const plan = recentFirstMaterializePlan(n, h, ch, buf);
    scrollTops.push(plan.scrollTop);
    const totalH = n * h;
    return {
      scrollTops: scrollTops,
      midHydrateClimbs: climbs,
      paintedDuringReplay: lazyAppend ? 0 : n,
      materializedAtPin: plan.materialIndices.length,
      finalDistToBottom: Math.max(0, totalH - ch) - plan.scrollTop,
      landsOnLatest: plan.landsOnLatest,
      // Band bound in item units: viewport + buffer both sides + edge slack.
      materialBandBound: Math.ceil((ch + 2 * buf) / h) + 2,
    };
  }

  // ── Windowed height backfill (🎯T119.2) ─────────────────────────────
  // History may grow without limit; computing the view must not. A pass
  // over N shells measures only the viewport + anticipation band. Rows
  // outside stay on last cache or estimate. Paging up slides the window
  // back and backfill resumes; jump-to-bottom does not re-measure the
  // prefix. T119 data-in-memory is unchanged.

  // Extra rows each side of the estimated window — estimates can drift
  // a row or two from real offsetTop.
  const MEASURE_INDEX_SLACK = 2;

  function measureWindowBounds(scrollTop, clientHeight, buffer) {
    const buf = typeof buffer === 'number' ? buffer : DEFAULT_BUFFER;
    const st = Number(scrollTop) || 0;
    const ch = Number(clientHeight) || 0;
    const b = Number.isFinite(buf) ? buf : DEFAULT_BUFFER;
    return { top: st - b, bot: st + ch + b, buffer: b };
  }

  /**
   * Inclusive [first, last] of items whose top intersects [winTop, winBot].
   * getTop(i) is caller-provided (estimated prefix or DOM offsetTop).
   * Binary search + linear walk of the window — O(log N + window) probes.
   * The row immediately before the first top ≥ winTop is included (it may
   * still overlap the window).
   */
  function findIntersectingIndexRange(n, getTop, winTop, winBot) {
    const count = Math.max(0, n | 0);
    if (!count || typeof getTop !== 'function') {
      return { first: -1, last: -1, count: 0, probes: 0 };
    }
    const wt = Number(winTop);
    const wb = Number(winBot);
    const viewTop = Number.isFinite(wt) ? wt : 0;
    const viewBot = Number.isFinite(wb) ? wb : 0;
    let probes = 0;
    let lo = 0, hi = count - 1, firstAtOrPast = count;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1;
      probes++;
      const top = Number(getTop(mid)) || 0;
      if (top >= viewTop) {
        firstAtOrPast = mid;
        hi = mid - 1;
      } else {
        lo = mid + 1;
      }
    }
    let first = firstAtOrPast > 0 ? firstAtOrPast - 1 : firstAtOrPast;
    if (first >= count) return { first: -1, last: -1, count: 0, probes: probes };
    if (first < 0) first = 0;
    let last = first;
    let sawInside = false;
    for (let i = first; i < count; i++) {
      probes++;
      const top = Number(getTop(i)) || 0;
      if (top > viewBot) break;
      last = i;
      sawInside = true;
    }
    if (!sawInside) return { first: -1, last: -1, count: 0, probes: probes };
    return { first: first, last: last, count: last - first + 1, probes: probes };
  }

  function widenMeasureRange(range, n, slack) {
    const count = Math.max(0, n | 0);
    const s = slack == null ? MEASURE_INDEX_SLACK : Math.max(0, slack | 0);
    if (!range || range.first < 0 || range.last < 0 || !count) {
      return { first: -1, last: -1, count: 0, probes: range ? range.probes : 0 };
    }
    const first = Math.max(0, range.first - s);
    const last = Math.min(count - 1, range.last + s);
    return {
      first: first,
      last: last,
      count: last - first + 1,
      probes: range.probes || 0,
    };
  }

  /**
   * Sentinel top for an off-window row so planVirtualizePass demats it
   * without a layout read. below:true → far past the live end.
   */
  function offWindowSentinelTop(below) {
    return below ? 1e9 : -1e9;
  }

  /**
   * Oracle: thousands of shells, view at the live end, then a page-up,
   * then jump-to-bottom. Measure count stays O(viewport + band); the
   * oldest measured index recedes on page-up and the prefix is not
   * re-measured after the jump.
   */
  function measureBackfillTrace(opts) {
    const o = opts || {};
    const n = o.n > 0 ? o.n | 0 : 3000;
    const h = o.avgHeight > 0 ? Number(o.avgHeight) : DEFAULT_ESTIMATE_HEIGHT;
    const ch = o.clientHeight > 0 ? Number(o.clientHeight) : 600;
    const buf = typeof o.buffer === 'number' ? o.buffer : DEFAULT_BUFFER;
    const pageFactor = o.pageFactor > 0 ? Number(o.pageFactor) : 0.8;
    const tops = new Array(n);
    for (let i = 0; i < n; i++) tops[i] = i * h;
    const getTop = function (i) { return tops[i]; };
    const windowPx = ch + 2 * buf;
    const maxCount = Math.ceil(windowPx / h) + 2;
    const logBound = Math.ceil(Math.log(n + 1) / Math.log(2)) + 4;
    function at(scrollTop) {
      const w = measureWindowBounds(scrollTop, ch, buf);
      const r = findIntersectingIndexRange(n, getTop, w.top, w.bot);
      return {
        scrollTop: scrollTop,
        first: r.first,
        last: r.last,
        count: r.count,
        probes: r.probes,
        maxCount: maxCount,
        bounded: r.count <= maxCount && r.probes <= maxCount + logBound,
      };
    }
    const endST = Math.max(0, n * h - ch);
    const atEnd = at(endST);
    const afterPageUp = at(Math.max(0, endST - ch * pageFactor));
    const afterJumpToBottom = at(endST);
    return {
      n: n,
      atEnd: atEnd,
      afterPageUp: afterPageUp,
      afterJumpToBottom: afterJumpToBottom,
      receded: afterPageUp.first >= 0 && atEnd.first >= 0 && afterPageUp.first < atEnd.first,
      jumpDoesNotRemeasurePrefix: afterJumpToBottom.first === atEnd.first && atEnd.first > 0,
    };
  }

  // ── Main-thread liveness: phased, budgeted virtualize (🎯T349) ──────
  // Owner regression: composer froze for tens of seconds (blind-typed blobs,
  // Firefox slow-script dialog) on a long transcript. Root cause: the
  // virtualize pass interleaved layout reads (offsetTop/offsetHeight of the
  // next row) with dematerialize writes (class/style/innerHTML of the
  // previous row). Every write invalidated layout, so each subsequent read
  // forced a full reflow of the flow container — O(rows²) layout work in one
  // sync pass when many rows dematerialize at once. Secondary: the
  // rematerialize cap (6/frame) bounded item count but not time — a few
  // giant markdown bodies still blocked a frame for seconds.
  // Model: (a) all geometry reads happen before any write, (b) writes are
  // capped per frame (DEMATERIALIZE_PER_FRAME) and time-budgeted
  // (FRAME_BUDGET_MS), (c) leftover work re-arms the next frame — a single
  // unbounded sync pass no longer exists on the virtualize path.

  // Demat writes are cheap (innerHTML='' + class/style) — a generous cap
  // still clears thousands of rows within a second of frames.
  const DEMATERIALIZE_PER_FRAME = 40;
  // Per-frame paint budget: half a 60Hz frame, leaving room for input,
  // style/layout, and the browser's own work.
  const FRAME_BUDGET_MS = 10;

  function frameBudgetExceeded(startMs, nowMs, budgetMs) {
    const b = budgetMs > 0 ? Number(budgetMs) : FRAME_BUDGET_MS;
    const s = Number(startMs);
    const t = Number(nowMs);
    if (!Number.isFinite(s) || !Number.isFinite(t) || !Number.isFinite(b)) return false;
    return t - s > b;
  }

  /**
   * Plan one phased virtualize frame from pre-read geometry (no DOM here).
   * items: [{ top, height, shell, stream?, dematOk?, index?, ...el }].
   *   stream: live streaming bubble — never planned (caller handles).
   *   dematOk:false — dematerializeMsg would refuse (auto-expanded, no body);
   *   excluded from the plan so the leftover re-arm cannot spin on rows that
   *   never make progress.
   * Returns viewport-first remat ≤ rematPerFrame, demat ≤ dematPerFrame,
   * and the deferred remainders; `more` re-arms the next frame.
   */
  function planVirtualizePass(items, scrollTop, clientHeight, buffer, caps) {
    const c = caps || {};
    const dematMax = c.dematPerFrame > 0 ? c.dematPerFrame : DEMATERIALIZE_PER_FRAME;
    const rematMax = c.rematPerFrame > 0 ? c.rematPerFrame : REMATERIALIZE_PER_FRAME;
    const rematPending = [];
    const dematPending = [];
    const list = Array.isArray(items) ? items : [];
    for (let i = 0; i < list.length; i++) {
      const it = list[i];
      if (!it || it.stream) continue;
      const want = shouldMaterialize(it.top, it.height, scrollTop, clientHeight, buffer);
      if (want && it.shell) rematPending.push(it);
      else if (!want && !it.shell && it.dematOk !== false) dematPending.push(it);
    }
    const rplan = planRematerializeFrame(rematPending, scrollTop, clientHeight, rematMax);
    return {
      rematThisFrame: rplan.thisFrame,
      rematRemaining: rplan.remaining,
      dematThisFrame: dematPending.slice(0, dematMax),
      dematRemaining: dematPending.slice(dematMax),
      more: rplan.remaining.length > 0 || dematPending.length > dematMax,
    };
  }

  /**
   * Stress oracle: n materialized rows, viewport pinned at the end, so all
   * rows above the band want dematerialize at once (long-transcript settle).
   *   phased (default) → reflows grow with passes (≤1 per frame: reads are
   *     batched before writes) and maxWritesPerPass ≤ dematPerFrame;
   *   phased:false → the legacy single pass: every write dirties layout and
   *     the next row's read reflows — interleavedReflows ≈ n (the freeze).
   */
  function virtualizePassTrace(opts) {
    const o = opts || {};
    const n = Math.max(0, o.n | 0);
    const h = o.avgHeight > 0 ? o.avgHeight : DEFAULT_ESTIMATE_HEIGHT;
    const ch = o.clientHeight > 0 ? o.clientHeight : 600;
    const buf = typeof o.buffer === 'number' ? o.buffer : DEFAULT_BUFFER;
    const cap = o.dematPerFrame > 0 ? o.dematPerFrame : DEMATERIALIZE_PER_FRAME;
    const phased = o.phased !== false;
    const scrollTop = Math.max(0, n * h - ch);
    const items = [];
    for (let i = 0; i < n; i++) {
      items.push({ index: i, top: i * h, height: h, shell: false, dematOk: true });
    }
    if (!phased) {
      // Legacy interleaved pass: read row i (reflow if a prior write dirtied
      // layout) → maybe write row i → next read reflows again.
      let reflows = 0;
      let writes = 0;
      let dirty = false;
      for (let i = 0; i < items.length; i++) {
        if (dirty) { reflows++; dirty = false; }
        const it = items[i];
        const want = shouldMaterialize(it.top, it.height, scrollTop, ch, buf);
        if (!want && !it.shell) { it.shell = true; writes++; dirty = true; }
      }
      return {
        passes: 1,
        reflows: reflows,
        writes: writes,
        maxWritesPerPass: writes,
        converged: true,
      };
    }
    let passes = 0;
    let reflows = 0;
    let writes = 0;
    let maxWritesPerPass = 0;
    for (let guard = 0; ; guard++) {
      const plan = planVirtualizePass(items, scrollTop, ch, buf, { dematPerFrame: cap });
      if (!plan.dematThisFrame.length && !plan.rematThisFrame.length) break;
      passes++;
      reflows++; // reads batched before writes: at most one layout per frame
      for (let j = 0; j < plan.dematThisFrame.length; j++) {
        plan.dematThisFrame[j].shell = true;
        writes++;
      }
      maxWritesPerPass = Math.max(maxWritesPerPass, plan.dematThisFrame.length);
      if (guard > n + 2) {
        return { passes: passes, reflows: reflows, writes: writes,
                 maxWritesPerPass: maxWritesPerPass, converged: false };
      }
    }
    return { passes: passes, reflows: reflows, writes: writes,
             maxWritesPerPass: maxWritesPerPass, converged: true };
  }

  // Final scrollTop after one pin to the live end.
  // NOTE (🎯T351): this is GATE MATH ONLY (distance-from-end decisions).
  // scrollHeight/clientHeight are integer-rounded by the DOM, so sh − ch can
  // differ from the true fractional max scrollTop by up to 1px. Pin WRITES
  // must use pinWriteScrollTop below, never this value.
  function finalPinScrollTop(scrollHeight, clientHeight) {
    const sh = Math.max(0, Number(scrollHeight) || 0);
    const ch = Math.max(0, Number(clientHeight) || 0);
    return Math.max(0, sh - ch);
  }

  // 🎯T351: the value to ASSIGN when pinning to the live end. Over-assign
  // the full scrollHeight — the browser clamps to the exact FRACTIONAL max
  // (contentHeightF − clientHeightF), so the content bottom sits flush with
  // the viewport bottom regardless of the fractional part of total height.
  // Writing integer sh − ch instead leaves the viewport at a sub-pixel
  // offset from the true bottom that CHANGES whenever frac(total height)
  // drifts (turn-markers 13.1875px, 22.4px lines, hydrate pages) — the
  // owner-visible residual ~1px jiggle that survived T341/T350.
  function pinWriteScrollTop(scrollHeight) {
    return Math.max(0, Number(scrollHeight) || 0);
  }

  // 🎯T351: pinned-at-end check tolerant of fractional clamp. After an
  // over-assign pin, scrollTop = fractional max M while the integer target
  // sh − ch may differ by up to 1px in either direction — |st − target| < 0.5
  // misreads that as "not pinned" and rewrites every pass. eps 1px covers the
  // worst rounding gap; over-assign writes are idempotent anyway.
  const PIN_AT_END_EPS_PX = 1;
  function isPinnedAtEnd(scrollTop, scrollHeight, clientHeight, epsPx) {
    const eps = epsPx == null ? PIN_AT_END_EPS_PX : Number(epsPx);
    const e = Number.isFinite(eps) && eps >= 0 ? eps : PIN_AT_END_EPS_PX;
    const st = Number(scrollTop) || 0;
    return Math.abs(finalPinScrollTop(scrollHeight, clientHeight) - st) <= e;
  }

  // 🎯T351: whole-pixel row lock. Scroll offsets are integer-quantized by
  // the engine (a scrollTop write cannot express a fraction — measured:
  // assigning 100.5 reads back 101; over-assign clamps to an integer), so
  // any row whose border-box height is fractional (22.4px text lines,
  // 13.1875px turn-markers, mermaid/SVG) makes frac(total content height)
  // drift as rows come and go — and every integer pin then lands at a
  // different sub-pixel offset from the true content bottom: the
  // owner-visible ~1px jiggle. Locking each row's min-height at the ceiling
  // of its natural height keeps every row box on the pixel grid (≤1px of
  // invisible slack), so the pinned viewport phase is constant.
  const ROW_SNAP_EPS_PX = 1 / 128; // rects quantize at 1/64px; tolerate float fuzz
  function snappedRowLockPx(height) {
    const h = Number(height);
    if (!Number.isFinite(h) || h <= 0) return 0;
    const snapped = Math.ceil(h - ROW_SNAP_EPS_PX);
    return snapped > 0 ? snapped : 0;
  }

  // ── Viewport anchoring (🎯T351 hydrate, 🎯T363 virtualize) ─────────
  // One rule covers both: when content ABOVE the viewport changes height,
  // scrollTop must move by the same amount or the text under the owner's
  // eyes slides. #messages runs overflow-anchor:none on purpose (browser
  // scroll anchoring fights the follow-scroll pin), so the code that made
  // the height change owns the compensation — nothing else will do it.

  /**
   * Rect-exact compensation: keep the anchor row's screen position fixed.
   * before/after are the anchor's getBoundingClientRect().top measured
   * around the writes; the delta is the exact (fractional) height gained or
   * lost above it.
   */
  function anchorPreservedScrollTop(prevScrollTop, anchorTopBefore, anchorTopAfter) {
    const prev = Number(prevScrollTop) || 0;
    const before = Number(anchorTopBefore);
    const after = Number(anchorTopAfter);
    if (!Number.isFinite(before) || !Number.isFinite(after)) return prev;
    return Math.max(0, prev + (after - before));
  }

  /**
   * 🎯T363: which row to anchor on — the first whose bottom is below the
   * viewport top, i.e. the row the top edge cuts through (the text the owner
   * is reading). Anchoring lower down would let rows between it and the top
   * edge still shift the visible text; anchoring higher would pin content
   * that is already off-screen. -1 for an empty list.
   */
  function pickScrollAnchorIndex(items, scrollTop) {
    const list = Array.isArray(items) ? items : [];
    if (!list.length) return -1;
    const st = Number(scrollTop) || 0;
    for (let i = 0; i < list.length; i++) {
      const it = list[i] || {};
      const top = Number(it.top) || 0;
      const h = Number(it.height) || 0;
      if (top + h > st) return i;
    }
    return list.length - 1;
  }

  // scrollTop is integer-quantized by the engine, so a sub-pixel correction
  // cannot be expressed — writing one only burns a scroll event.
  const ANCHOR_COMPENSATE_MIN_PX = 0.5;
  function shouldCompensateAnchor(prevScrollTop, nextScrollTop, minPx) {
    const m = minPx == null ? ANCHOR_COMPENSATE_MIN_PX : Number(minPx);
    const eps = Number.isFinite(m) && m >= 0 ? m : ANCHOR_COMPENSATE_MIN_PX;
    const prev = Number(prevScrollTop);
    const next = Number(nextScrollTop);
    if (!Number.isFinite(prev) || !Number.isFinite(next)) return false;
    return Math.abs(next - prev) >= eps;
  }

  // 🎯T351: scroll compensation for a history page inserted ABOVE the
  // viewport. The integer scrollHeight delta (nextScrollHeight −
  // prevScrollHeight) loses the inserted page's fractional height, shifting
  // everything below by the remainder on every hydrate page (~420ms cadence
  // for minutes after a hard reload). Rect-exact: the anchor row's
  // getBoundingClientRect().top before/after the insert measures the true
  // fractional height gained. Falls back to the integer delta only when no
  // anchor rect is available (empty list).
  function hydrateCompensatedScrollTop(prevScrollTop, anchorTopBefore, anchorTopAfter,
                                       prevScrollHeight, nextScrollHeight) {
    const prev = Number(prevScrollTop) || 0;
    const before = Number(anchorTopBefore);
    const after = Number(anchorTopAfter);
    if (Number.isFinite(before) && Number.isFinite(after)) {
      return anchorPreservedScrollTop(prev, before, after);
    }
    const prevH = Number(prevScrollHeight) || 0;
    const nextH = Number(nextScrollHeight) || 0;
    return Math.max(0, prev + (nextH - prevH));
  }

  /**
   * 🎯T363 oracle: wheel-scroll up through a virtualized transcript whose
   * older rows are lazy shells frozen at an ESTIMATED height. Each step,
   * shells entering the anticipation band swap estimate → natural height;
   * the ones above the viewport top move everything below them.
   *
   * Reports the anchor row's screen-position drift — how far the text under
   * the owner's eyes moved beyond what the wheel asked for.
   *   compensate:true  → maxAnchorDriftPx === 0 (viewport tracks the wheel)
   *   compensate:false → drift on every step a shell above grew (the stutter)
   */
  function scrollUpAnchorTrace(opts) {
    const o = opts || {};
    const n = o.n > 0 ? o.n | 0 : 200;
    const est = o.estimateHeight > 0 ? Number(o.estimateHeight) : DEFAULT_ESTIMATE_HEIGHT;
    const natural = o.naturalHeight > 0 ? Number(o.naturalHeight) : 260;
    const ch = o.clientHeight > 0 ? Number(o.clientHeight) : 700;
    const buf = typeof o.buffer === 'number' ? o.buffer : DEFAULT_BUFFER;
    const step = o.stepPx > 0 ? Number(o.stepPx) : 240;
    const steps = o.steps > 0 ? o.steps | 0 : 20;
    const compensate = !!o.compensate;

    const heights = new Array(n).fill(est);
    const material = new Array(n).fill(false);
    const topsOf = function () {
      const tops = [];
      let y = 0;
      for (let i = 0; i < n; i++) { tops.push({ top: y, height: heights[i] }); y += heights[i]; }
      return tops;
    };
    const materializeBand = function (tops, scrollTop) {
      const want = visibleIndices(tops, scrollTop, ch, buf);
      for (let k = 0; k < want.length; k++) {
        const i = want[k];
        if (!material[i]) { material[i] = true; heights[i] = natural; }
      }
    };

    // Start pinned at the live end with the end band already painted.
    let tops = topsOf();
    let total = tops.length ? tops[n - 1].top + tops[n - 1].height : 0;
    let scrollTop = Math.max(0, total - ch);
    materializeBand(topsOf(), scrollTop);

    let maxDrift = 0;
    let totalDrift = 0;
    let jumps = 0;
    const drifts = [];
    for (let s = 0; s < steps && scrollTop > 0; s++) {
      scrollTop = Math.max(0, scrollTop - step);
      tops = topsOf();
      const ai = pickScrollAnchorIndex(tops, scrollTop);
      const anchorBefore = tops[ai].top - scrollTop; // screen-relative
      materializeBand(tops, scrollTop);
      const after = topsOf();
      if (compensate) {
        scrollTop = anchorPreservedScrollTop(scrollTop, tops[ai].top, after[ai].top);
      }
      const anchorAfter = after[ai].top - scrollTop;
      const drift = anchorAfter - anchorBefore;
      drifts.push(drift);
      if (Math.abs(drift) > 0) jumps++;
      totalDrift += Math.abs(drift);
      maxDrift = Math.max(maxDrift, Math.abs(drift));
    }
    return {
      maxAnchorDriftPx: maxDrift,
      totalAnchorDriftPx: totalDrift,
      jumpSteps: jumps,
      steps: drifts.length,
      drifts: drifts,
    };
  }

  // Note: shouldPinScroll (above) calls finalPinScrollTop at runtime — both
  // are function declarations in this factory so hoisting is fine.

  return {
    DEFAULT_BUFFER: DEFAULT_BUFFER,
    DEFAULT_ESTIMATE_HEIGHT: DEFAULT_ESTIMATE_HEIGHT,
    SHELL_UNMEASURED: SHELL_UNMEASURED,
    SHELL_DEMATERIALIZED: SHELL_DEMATERIALIZED,
    SHELL_MATERIAL: SHELL_MATERIAL,

    visibleIndices: visibleIndices,
    shouldMaterialize: shouldMaterialize,
    anyPartInViewport: anyPartInViewport,
    visibleOverlapPx: visibleOverlapPx,
    isFullyAboveViewport: isFullyAboveViewport,
    shouldAutoCollapseOffScreen: shouldAutoCollapseOffScreen,
    NEAR_END_SLACK_PX: NEAR_END_SLACK_PX,
    MIN_VISIBLE_PX_FOR_AUTO_EXPAND: MIN_VISIBLE_PX_FOR_AUTO_EXPAND,
    isNearTranscriptEnd: isNearTranscriptEnd,
    shouldAutoExpandInView: shouldAutoExpandInView,
    shouldRunOffScreenCollapse: shouldRunOffScreenCollapse,
    PIN_HEIGHT_THRESHOLD_PX: PIN_HEIGHT_THRESHOLD_PX,
    shouldPinScroll: shouldPinScroll,
    WIDTH_INVALIDATE_EPS_PX: WIDTH_INVALIDATE_EPS_PX,
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
    MIN_ATTACHED_SHELLS: MIN_ATTACHED_SHELLS,
    ATTACHED_BAND_SCREENS: ATTACHED_BAND_SCREENS,
    maxAttachedShells: maxAttachedShells,
    shouldAttachHistoryPage: shouldAttachHistoryPage,
    prependDetachedRecords: prependDetachedRecords,
    spacerPxForRecords: spacerPxForRecords,
    takeNewestDetached: takeNewestDetached,
    recordsFromChunks: recordsFromChunks,

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

    // 🎯T336: progressive rematerialize (Page Up thrash bound).
    REMATERIALIZE_PER_FRAME: REMATERIALIZE_PER_FRAME,
    rematerializePriority: rematerializePriority,
    planRematerializeFrame: planRematerializeFrame,
    pageUpRematerializeBudget: pageUpRematerializeBudget,

    // 🎯T119.1 / reload-reconnect: suppress stick-to-bottom during WS
    // chronological replay; one pin after history_meta (or idle end).
    shouldSuppressPinDuringReplay: shouldSuppressPinDuringReplay,
    finalPinScrollTop: finalPinScrollTop,
    pinWriteScrollTop: pinWriteScrollTop,
    PIN_AT_END_EPS_PX: PIN_AT_END_EPS_PX,
    isPinnedAtEnd: isPinnedAtEnd,
    hydrateCompensatedScrollTop: hydrateCompensatedScrollTop,
    snappedRowLockPx: snappedRowLockPx,

    // 🎯T363: viewport anchoring for height changes above the scroll position.
    anchorPreservedScrollTop: anchorPreservedScrollTop,
    pickScrollAnchorIndex: pickScrollAnchorIndex,
    ANCHOR_COMPENSATE_MIN_PX: ANCHOR_COMPENSATE_MIN_PX,
    shouldCompensateAnchor: shouldCompensateAnchor,
    scrollUpAnchorTrace: scrollUpAnchorTrace,
    // Idle end fallback ms if history_meta never arrives (empty log).
    REPLAY_IDLE_END_MS: REPLAY_IDLE_END_MS,

    // 🎯T347: end-first lazy hydrate; parade-proof pre-meta suppression.
    REPLAY_REENTER_MAX_MS: REPLAY_REENTER_MAX_MS,
    shouldReenterReplay: shouldReenterReplay,
    shouldPaintOnReplayAppend: shouldPaintOnReplayAppend,
    shouldVirtualizeDuringReplay: shouldVirtualizeDuringReplay,
    replayHydrateTrace: replayHydrateTrace,

    // 🎯T119.2: height backfill is windowed around the viewport.
    MEASURE_INDEX_SLACK: MEASURE_INDEX_SLACK,
    measureWindowBounds: measureWindowBounds,
    findIntersectingIndexRange: findIntersectingIndexRange,
    widenMeasureRange: widenMeasureRange,
    offWindowSentinelTop: offWindowSentinelTop,
    measureBackfillTrace: measureBackfillTrace,

    // 🎯T349: phased, budgeted virtualize — composer stays responsive.
    DEMATERIALIZE_PER_FRAME: DEMATERIALIZE_PER_FRAME,
    FRAME_BUDGET_MS: FRAME_BUDGET_MS,
    frameBudgetExceeded: frameBudgetExceeded,
    planVirtualizePass: planVirtualizePass,
    virtualizePassTrace: virtualizePassTrace,
  };
}));
