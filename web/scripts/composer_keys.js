// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Composer key policy (🎯T126 / 🎯T149 / 🎯T132).
// DOM-free so Node hermetic tests can require(); index.html wires caret
// apply, jump gate, and Enter-chord handling.
//
// 🎯T149: operate on *effective* (Wispr seed-stripped) content bounds so
// seed-only EMPTY_SEED does not look like a Home/End no-op, and seed+text
// lands on the visible draft — not inside the invisible seed prefix.
//
// 🎯T307: Home/End own *field* start/end; Meta|Ctrl+ArrowLeft/Right are
// left to the browser/OS as *line* start/end. The two chords are
// deliberately distinct — do not re-alias Cmd/Ctrl+Arrow onto field ends.
//
// Enter chords (composer focused):
//   Ctrl+Enter  → immediate send / interject
//   🎯T241 Alt+Enter = force-send only (never pop_last / never history):
//     real draft (!isEffectivelyEmpty) → force_send (interject if busy)
//     empty/seed-only + queue≥1 → send_queue_now (strip Send now)
//     empty/seed-only + no queue → noop
//   plain Enter → send (enqueue while busy per SendQueue)
//   Shift+Enter → newline
// History recall remains Alt+Up/Down only (not Alt+Enter).

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ComposerKeys = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Field-content policy (owner: start/end of the *message box*, not
  // line-local and not transcript scroll):
  //   Home → start of effective content
  //   End  → end of effective content
  // Meta|Ctrl+ArrowLeft/Right are NOT ours (🎯T307) — the platform moves
  // the caret to the current line's start/end.
  // Seed-only (effective empty): both collapse to the insert point
  // (after seed / value.length) so caret never sits *before* the seed
  // where typed characters would embed inside EMPTY_SEED.

  function isComposerTextControl(tagName) {
    const t = String(tagName || '').toUpperCase();
    return t === 'TEXTAREA' || t === 'INPUT';
  }

  /**
   * Effective caret bounds for field Home/End.
   * @param {string} value raw composer value (may include EMPTY_SEED prefix)
   * @param {{
   *   seedPrefixLen?: number,
   *   effectiveLength?: number,
   *   isSeedOnly?: boolean
   * }} [opts]
   * @returns {{ home: number, end: number }}
   */
  function contentCaretBounds(value, opts) {
    const s = String(value == null ? '' : value);
    const o = opts || {};
    let seedLen = 0;
    if (typeof o.seedPrefixLen === 'number' && o.seedPrefixLen > 0) {
      seedLen = Math.min(Math.floor(o.seedPrefixLen), s.length);
    }
    let effectiveLen;
    if (typeof o.effectiveLength === 'number' && o.effectiveLength >= 0) {
      effectiveLen = o.effectiveLength;
    } else {
      effectiveLen = Math.max(0, s.length - seedLen);
    }
    const seedOnly = o.isSeedOnly === true
      || (seedLen > 0 && effectiveLen === 0)
      || (s.length > 0 && seedLen > 0 && s.length === seedLen);

    if (!s.length || seedOnly || effectiveLen === 0) {
      // Empty effective content: one resting insert point (after seed).
      return { home: s.length, end: s.length };
    }
    // Real draft: Home at first effective char (skip seed prefix if any).
    return { home: seedLen, end: s.length };
  }

  function caretAfterHome(value, _selectionStart, opts) {
    return contentCaretBounds(value, opts).home;
  }

  function caretAfterEnd(value, _selectionStart, opts) {
    return contentCaretBounds(value, opts).end;
  }

  /**
   * Pure selection result for Home/End (and Shift+ variants). Returns null
   * when the key is not a composer caret key we own — plain ArrowLeft,
   * Meta+ArrowDown (jump), and, since 🎯T307, Meta|Ctrl+ArrowLeft/Right,
   * which the browser handles as line start/end.
   *
   * @param {string} key
   * @param {string} value
   * @param {number} selStart
   * @param {number} selEnd
   * @param {{ metaKey?: boolean, ctrlKey?: boolean, altKey?: boolean, shiftKey?: boolean }} mods
   * @param {{ seedPrefixLen?: number, effectiveLength?: number, isSeedOnly?: boolean }} [opts]
   */
  function selectionAfterHomeEnd(key, value, selStart, selEnd, mods, opts) {
    const m = mods || {};
    // Alt: leave Option+Arrow paragraph / browser defaults alone.
    if (m.altKey) return null;

    let goHome = false;
    let goEnd = false;

    if (key === 'Home') {
      goHome = true;
    } else if (key === 'End') {
      goEnd = true;
    } else {
      // 🎯T307: Meta|Ctrl+ArrowLeft/Right belong to the browser/OS as
      // *line* start/end. Returning null keeps index.html from
      // preventDefault-ing them, so the two chords stay distinct:
      // Home/End = field-wide, Cmd/Ctrl+Arrow = current line.
      return null;
    }

    const start = typeof selStart === 'number' ? selStart : 0;
    const end = typeof selEnd === 'number' ? selEnd : start;
    const bounds = contentCaretBounds(value, opts);
    const home = bounds.home;
    const fieldEnd = bounds.end;

    if (goHome) {
      if (m.shiftKey) {
        // Extend toward start of effective content; clamp anchor into bounds.
        const anchor = Math.max(end, home);
        return { start: home, end: anchor };
      }
      return { start: home, end: home };
    }
    if (goEnd) {
      if (m.shiftKey) {
        const anchor = Math.min(start, fieldEnd);
        return { start: anchor, end: fieldEnd };
      }
      return { start: fieldEnd, end: fieldEnd };
    }
    return null;
  }

  // Document-level jump-to-bottom gate (🎯T119 + 🎯T126).
  // isJumpHotkey(key, mods) is typically VirtualList.isJumpToBottomHotkey.
  // When the main composer (or any text field) is focused, plain End must
  // not jump — only move the caret. Explicit jump chords
  // (Meta/Ctrl+ArrowDown) still jump.
  function shouldAllowJumpToBottom(key, mods, opts) {
    const m = mods || {};
    const o = opts || {};
    const isJump = typeof o.isJumpHotkey === 'function'
      ? o.isJumpHotkey(key, m)
      : defaultIsJumpToBottomHotkey(key, m);
    if (!isJump) return false;
    const textFocused = !!(o.composerFocused || o.inTextField);
    if (textFocused && key === 'End' && !m.metaKey && !m.ctrlKey) {
      return false;
    }
    return true;
  }

  function defaultIsJumpToBottomHotkey(key, mods) {
    const m = mods || {};
    if (key === 'End' && !m.altKey && !m.shiftKey) return true;
    if (key === 'ArrowDown' && (m.metaKey || m.ctrlKey) && !m.altKey && !m.shiftKey) {
      return true;
    }
    return false;
  }

  // True when active element is the main composer control (by id or ref).
  function isComposerFocused(activeEl, composerEl) {
    if (!activeEl || !composerEl) return false;
    return activeEl === composerEl;
  }

  // ── 🎯T132 Enter-chord policy ─────────────────────────────────────
  // Pure classification for the composer keydown path.
  //
  // Returns:
  //   'newline'         — Shift+Enter (leave browser default)
  //   'send'            — plain Enter (or Meta+Enter): send / enqueue
  //   'interrupt'       — Ctrl+Enter: immediate send / interject while busy
  //   'force_send'      — Alt+Enter with real draft (🎯T241 interject draft)
  //   'send_queue_now'  — Alt+Enter empty/seed-only + queue≥1 (Send now)
  //   'noop'            — Alt+Enter empty/seed-only + no queue
  //   null              — not Enter (caller ignores)
  //
  // 🎯T241: Alt+Enter never returns 'pop_last'. History is Alt+Up/Down only.
  //
  // composerEmpty: true when there is no real draft ('' / whitespace /
  // Wispr seed-only EMPTY_SEED / seed-shaped residue). Caller MUST pass
  // WisprContext.isEffectivelyEmpty — bare !trim() treats hidden seed as draft.
  // queueLen: send-queue item count for empty-composer send-now branch.

  /**
   * True when this keydown is Enter (incl. NumpadEnter / code fallback).
   * 🎯T235: macOS Option+Enter sometimes leaves key non-"Enter" while
   * code stays Enter — pure `key === 'Enter'` then never classified.
   * @param {string} key
   * @param {{ code?: string }} [opts]
   */
  function isEnterKey(key, opts) {
    if (key === 'Enter') return true;
    const code = opts && opts.code != null ? String(opts.code) : '';
    return code === 'Enter' || code === 'NumpadEnter';
  }

  /**
   * @param {string} key
   * @param {{ metaKey?: boolean, ctrlKey?: boolean, altKey?: boolean, shiftKey?: boolean, code?: string }} mods
   * @param {{ composerEmpty?: boolean, code?: string, queueLen?: number }} [opts]
   * @returns {'newline'|'send'|'interrupt'|'force_send'|'send_queue_now'|'noop'|null}
   */
  function classifyEnterAction(key, mods, opts) {
    const m = mods || {};
    const o = opts || {};
    // Prefer explicit opts.code; fall back to mods.code (KeyboardEvent).
    const code = o.code != null ? o.code : m.code;
    if (!isEnterKey(key, { code: code })) return null;
    if (m.shiftKey) return 'newline';

    // Ctrl+Enter wins as immediate-send even if other modifiers are held
    // (except Shift, already handled).
    if (m.ctrlKey) return 'interrupt';

    // 🎯T241 Alt+Enter = force-send only (never pop_last / history):
    //   real draft → force_send (caller sends draft with interrupt if busy)
    //   empty/seed-only + queue≥1 → send_queue_now
    //   empty/seed-only + no queue → noop
    // 🎯T235: callers may set altKey from e.altKey || getModifierState('Alt').
    if (m.altKey) {
      if (!o.composerEmpty) return 'force_send';
      const qLen = o.queueLen | 0;
      if (qLen > 0) return 'send_queue_now';
      return 'noop';
    }

    // Plain Enter or Meta+Enter: normal send path (T113 enqueues when busy).
    return 'send';
  }

  /**
   * Most recent owner message from chronological history.
   * @param {Array<{text?: string, el?: *}>|null|undefined} history
   * @returns {{ text: string, index: number, el: * }|null}
   */
  function lastOwnerHistoryEntry(history) {
    if (!history || !history.length) return null;
    // Prefer last non-empty text (trailing empty stubs must not win).
    for (let i = history.length - 1; i >= 0; i--) {
      const entry = history[i];
      if (!entry) continue;
      const text = entry.text == null ? '' : String(entry.text);
      if (!text) continue;
      return { text: text, index: i, el: entry.el };
    }
    return null;
  }

  /**
   * 🎯T227 / 🎯T235: product-path last-owner resolve for Alt+Enter pop.
   *
   * Prefer in-memory msgHistory (WS replay + live user turns). When that is
   * empty or has no text (progressive hydrate paints `.msg.user` into the DOM
   * without pushing msgHistory; soft-reconnect / desync can leave the same
   * hole), fall back to chronological DOM-derived entries so empty Alt+Enter
   * still seeds the last owner bubble.
   *
   * 🎯T235: when DOM has *more* owner entries than history (hydrate ahead of
   * memory), prefer DOM last — history may hold a stale tail that is not the
   * painted most-recent owner. Pure classifyEnterAction alone never saw this.
   *
   * @param {Array<{text?: string, el?: *}>|null|undefined} history
   * @param {Array<{text?: string, el?: *}>|null|undefined} domEntries chronological owner bubbles
   * @returns {{ text: string, index: number, el: *, source: 'history'|'dom' }|null}
   */
  function resolveLastOwnerEntry(history, domEntries) {
    const hist = Array.isArray(history) ? history : [];
    const dom = Array.isArray(domEntries) ? domEntries : [];

    // Hydrate-ahead: DOM is the durable product view when longer than memory.
    if (dom.length > hist.length) {
      const fromDomFirst = lastOwnerHistoryEntry(dom);
      if (fromDomFirst && fromDomFirst.text) {
        return {
          text: fromDomFirst.text,
          index: fromDomFirst.index,
          el: fromDomFirst.el,
          source: 'dom',
        };
      }
    }

    const fromHist = lastOwnerHistoryEntry(hist);
    if (fromHist && fromHist.text) {
      return {
        text: fromHist.text,
        index: fromHist.index,
        el: fromHist.el,
        source: 'history',
      };
    }
    const fromDom = lastOwnerHistoryEntry(dom);
    if (fromDom && fromDom.text) {
      return {
        text: fromDom.text,
        index: fromDom.index,
        el: fromDom.el,
        source: 'dom',
      };
    }
    return null;
  }

  /**
   * Build [{text, el}] from owner bubble nodes (product: `.msg.user` with
   * `_layoutText`). Filters empty text. Order preserved (chronological DOM).
   * 🎯T235: empty-string `_layoutText` must fall through to `_body` / content
   * (prior code treated '' as set and skipped visible body text → silent miss).
   * @param {Array<{_layoutText?: *, textContent?: *, _body?: {textContent?: *}}>|null|undefined} nodes
   * @returns {Array<{text: string, el: *}>}
   */
  function ownerEntriesFromUserNodes(nodes) {
    const list = Array.isArray(nodes) ? nodes : [];
    const out = [];
    for (let i = 0; i < list.length; i++) {
      const el = list[i];
      if (!el) continue;
      let text = '';
      if (el._layoutText != null && String(el._layoutText).length) {
        text = String(el._layoutText);
      } else if (el._body && el._body.textContent != null && String(el._body.textContent).length) {
        // Prefer body over outer textContent (timestamp chrome lives outside body).
        text = String(el._body.textContent);
      } else if (el._layoutText == null && el.textContent != null) {
        text = String(el.textContent);
      }
      if (!text || !String(text).trim()) continue;
      out.push({ text: text, el: el });
    }
    return out;
  }

  /**
   * 🎯T235: full product-path plan for empty Alt+Enter pop.
   *
   * Resolves last owner text, resyncs history from DOM when needed, and
   * **never clobbers** a good resolved entry with an empty hist tail after a
   * no-op rebuild (prior greenwash: resolve found DOM text → rebuild skipped
   * because hist.length >= dom.length with empty stubs → overwrite → false).
   *
   * @param {Array<{text?: string, el?: *}>|null|undefined} history
   * @param {Array<{text?: string, el?: *}>|null|undefined} domEntries
   * @returns {{
   *   ok: boolean,
   *   text?: string,
   *   el?: *,
   *   index?: number,
   *   source?: 'history'|'dom',
   *   history?: Array<{text: string, el: *}>,
   *   failLoud?: boolean,
   *   reason?: string,
   *   bubbleCount?: number
   * }}
   */
  function planPopLastOwner(history, domEntries) {
    const hist = Array.isArray(history) ? history : [];
    const dom = Array.isArray(domEntries) ? domEntries : [];
    let bubbleCount = 0;
    for (let i = 0; i < hist.length; i++) {
      if (hist[i] && hist[i].text) bubbleCount++;
    }
    if (dom.length > bubbleCount) bubbleCount = dom.length;

    const entry = resolveLastOwnerEntry(hist, dom);
    if (!entry || !entry.text) {
      return {
        ok: false,
        failLoud: bubbleCount > 0,
        reason: bubbleCount > 0 ? 'resolve_miss' : 'no_owner',
        bubbleCount: bubbleCount,
      };
    }

    // Resync memory: DOM wins when longer or when we resolved from DOM.
    // Always copy DOM when it is the longer/equal source of truth so Alt+Up
    // shares the same stack — but never replace entry.text with empty tail.
    let nextHist = hist.slice();
    if (dom.length && (entry.source === 'dom' || !hist.length || hist.length < dom.length)) {
      nextHist = dom.map(function (e) {
        return { text: e.text, el: e.el };
      });
    }

    let index = entry.index;
    let el = entry.el;
    // Re-point index/el at nextHist tail when it still carries the same text.
    if (nextHist.length) {
      const last = nextHist[nextHist.length - 1];
      if (last && last.text === entry.text) {
        index = nextHist.length - 1;
        el = last.el;
      } else {
        // Keep resolved text; find last matching entry for highlight.
        for (let i = nextHist.length - 1; i >= 0; i--) {
          if (nextHist[i] && nextHist[i].text === entry.text) {
            index = i;
            el = nextHist[i].el;
            break;
          }
        }
      }
    }

    // Guard: if nextHist last is empty/wrong, still return the resolved text.
    return {
      ok: true,
      text: entry.text,
      el: el,
      index: index,
      source: entry.source,
      history: nextHist,
      bubbleCount: bubbleCount,
    };
  }

  return {
    isComposerTextControl: isComposerTextControl,
    contentCaretBounds: contentCaretBounds,
    caretAfterHome: caretAfterHome,
    caretAfterEnd: caretAfterEnd,
    selectionAfterHomeEnd: selectionAfterHomeEnd,
    shouldAllowJumpToBottom: shouldAllowJumpToBottom,
    isComposerFocused: isComposerFocused,
    // Exported for hermetic parity with VirtualList jump keys.
    defaultIsJumpToBottomHotkey: defaultIsJumpToBottomHotkey,
    // 🎯T132
    classifyEnterAction: classifyEnterAction,
    isEnterKey: isEnterKey,
    lastOwnerHistoryEntry: lastOwnerHistoryEntry,
    // 🎯T227 / 🎯T235
    resolveLastOwnerEntry: resolveLastOwnerEntry,
    ownerEntriesFromUserNodes: ownerEntriesFromUserNodes,
    planPopLastOwner: planPopLastOwner,
  };
}));
