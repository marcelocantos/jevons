// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Owner-chat send queue policy (🎯T113 / 🎯T154). DOM-free so Node can require().
//
// While an overseer turn is in flight, plain Enter enqueues a follow-up.
// Only Control+Enter interjects (interrupt + send). Queued items drain
// FIFO when the in-flight turn seals. Alt+Up/Down cycles queue before
// submitted request history.
//
// 🎯T154: queue state persists to localStorage (text-only bodies) so full
// page reload restores FIFO order; soft reconnect must not reset it.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.SendQueue = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  const STORAGE_KEY = 'jevons-send-queue-v1';

  function emptyState() {
    return { items: [], nextId: 1 };
  }

  // Pure send decision before wire I/O.
  // busy: turn in flight; interrupt: Control+Enter chord.
  // Returns:
  //   { action: 'noop' }
  //   { action: 'enqueue', text }
  //   { action: 'send', text, interrupt: boolean }
  function decideSend(opts) {
    const o = opts || {};
    const busy = !!o.busy;
    const interrupt = !!o.interrupt;
    const raw = o.text == null ? '' : String(o.text);
    const hasPayload = raw.trim().length > 0 || !!o.hasImages;
    if (!hasPayload) return { action: 'noop' };
    if (busy && !interrupt) {
      return { action: 'enqueue', text: raw };
    }
    return { action: 'send', text: raw, interrupt: busy && interrupt };
  }

  // True only for Control+Enter while busy — plain Enter must never interrupt.
  function shouldInterrupt(busy, interruptChord) {
    return !!(busy && interruptChord);
  }

  function shouldEnqueue(busy, interruptChord) {
    return !!(busy && !interruptChord);
  }

  function enqueue(state, text) {
    const s = state || emptyState();
    const id = 'q' + s.nextId;
    return {
      items: s.items.concat([{ id: id, text: String(text == null ? '' : text) }]),
      nextId: (s.nextId || 1) + 1,
    };
  }

  function cancel(state, id) {
    const s = state || emptyState();
    return {
      items: s.items.filter(function (it) { return it.id !== id; }),
      nextId: s.nextId || 1,
    };
  }

  function updateItem(state, id, text) {
    const s = state || emptyState();
    return {
      items: s.items.map(function (it) {
        return it.id === id ? { id: it.id, text: String(text == null ? '' : text) } : it;
      }),
      nextId: s.nextId || 1,
    };
  }

  // Remove by id and return { state, item } (item null if missing).
  function takeById(state, id) {
    const s = state || emptyState();
    let found = null;
    const items = [];
    for (let i = 0; i < s.items.length; i++) {
      if (s.items[i].id === id && !found) found = s.items[i];
      else items.push(s.items[i]);
    }
    return {
      state: { items: items, nextId: s.nextId || 1 },
      item: found,
    };
  }

  // FIFO drain: head of queue.
  function shiftNext(state) {
    const s = state || emptyState();
    if (!s.items.length) return { state: s, item: null };
    return {
      state: { items: s.items.slice(1), nextId: s.nextId || 1 },
      item: s.items[0],
    };
  }

  // Navigation stack (newer → older on Alt+Up):
  //   live | q[n-1] … q[0] | h[m-1] … h[0]
  // focus: { zone:'live' } | { zone:'queue', index } | { zone:'history', index }
  // dir: -1 = Up (older), +1 = Down (newer).
  // Returns { handled, focus } — handled false when nothing to cycle into.
  function cycleNav(focus, dir, queueLen, historyLen) {
    const d = dir < 0 ? -1 : 1;
    const qn = Math.max(0, queueLen | 0);
    const hn = Math.max(0, historyLen | 0);
    const f = focus && focus.zone ? focus : { zone: 'live' };

    if (f.zone === 'live') {
      if (d > 0) return { handled: false, focus: f };
      if (qn > 0) return { handled: true, focus: { zone: 'queue', index: qn - 1 } };
      if (hn > 0) return { handled: true, focus: { zone: 'history', index: hn - 1 } };
      return { handled: false, focus: f };
    }

    if (f.zone === 'queue') {
      let i = f.index | 0;
      if (i < 0) i = 0;
      if (i >= qn) {
        // Queue emptied under us — fall through sensibly.
        if (d < 0 && hn > 0) return { handled: true, focus: { zone: 'history', index: hn - 1 } };
        return { handled: true, focus: { zone: 'live' } };
      }
      if (d < 0) {
        if (i > 0) return { handled: true, focus: { zone: 'queue', index: i - 1 } };
        if (hn > 0) return { handled: true, focus: { zone: 'history', index: hn - 1 } };
        return { handled: true, focus: { zone: 'queue', index: 0 } }; // clamp
      }
      // Down toward live
      if (i < qn - 1) return { handled: true, focus: { zone: 'queue', index: i + 1 } };
      return { handled: true, focus: { zone: 'live' } };
    }

    if (f.zone === 'history') {
      let i = f.index | 0;
      if (i < 0) i = 0;
      if (i >= hn) {
        if (d > 0) {
          if (qn > 0) return { handled: true, focus: { zone: 'queue', index: 0 } };
          return { handled: true, focus: { zone: 'live' } };
        }
        if (hn > 0) return { handled: true, focus: { zone: 'history', index: 0 } };
        return { handled: true, focus: { zone: 'live' } };
      }
      if (d < 0) {
        if (i > 0) return { handled: true, focus: { zone: 'history', index: i - 1 } };
        return { handled: true, focus: { zone: 'history', index: 0 } }; // clamp oldest
      }
      // Down: newer history → oldest queue → … → live
      if (i < hn - 1) return { handled: true, focus: { zone: 'history', index: i + 1 } };
      if (qn > 0) return { handled: true, focus: { zone: 'queue', index: 0 } };
      return { handled: true, focus: { zone: 'live' } };
    }

    return { handled: false, focus: { zone: 'live' } };
  }

  // Text to show when focus lands on a zone.
  function textForFocus(focus, queueItems, historyTexts, liveDraft) {
    const f = focus && focus.zone ? focus : { zone: 'live' };
    if (f.zone === 'live') return liveDraft == null ? '' : String(liveDraft);
    if (f.zone === 'queue') {
      const it = (queueItems || [])[f.index | 0];
      return it ? String(it.text) : '';
    }
    if (f.zone === 'history') {
      const t = (historyTexts || [])[f.index | 0];
      return t == null ? '' : String(t);
    }
    return '';
  }

  // ── Persistence (🎯T154) — same pattern as AttentionThreads.load/save ──

  function serialize(state) {
    const s = state || emptyState();
    const items = (s.items || []).map(function (it) {
      return {
        id: String(it && it.id != null ? it.id : ''),
        text: String(it && it.text != null ? it.text : ''),
      };
    }).filter(function (it) { return it.id.length > 0; });
    let nextId = Math.max(1, Number(s.nextId) || 1);
    // Keep nextId strictly above any restored qN suffix so ids stay unique.
    for (let i = 0; i < items.length; i++) {
      const m = /^q(\d+)$/.exec(items[i].id);
      if (m) {
        const n = parseInt(m[1], 10);
        if (n >= nextId) nextId = n + 1;
      }
    }
    return JSON.stringify({ items: items, nextId: nextId });
  }

  function deserialize(raw) {
    if (!raw) return emptyState();
    try {
      const parsed = typeof raw === 'string' ? JSON.parse(raw) : raw;
      if (!parsed || typeof parsed !== 'object') return emptyState();
      const items = [];
      let maxQ = 0;
      if (Array.isArray(parsed.items)) {
        for (let i = 0; i < parsed.items.length; i++) {
          const it = parsed.items[i];
          if (!it || it.id == null || it.id === '') continue;
          const id = String(it.id);
          items.push({ id: id, text: String(it.text == null ? '' : it.text) });
          const m = /^q(\d+)$/.exec(id);
          if (m) {
            const n = parseInt(m[1], 10);
            if (n > maxQ) maxQ = n;
          }
        }
      }
      let nextId = Math.max(1, Number(parsed.nextId) || 1);
      if (nextId <= maxQ) nextId = maxQ + 1;
      return { items: items, nextId: nextId };
    } catch (e) {
      return emptyState();
    }
  }

  function load(storage) {
    if (!storage || typeof storage.getItem !== 'function') return emptyState();
    try {
      return deserialize(storage.getItem(STORAGE_KEY));
    } catch (e) {
      return emptyState();
    }
  }

  function save(storage, state) {
    if (!storage || typeof storage.setItem !== 'function') return;
    try {
      storage.setItem(STORAGE_KEY, serialize(state));
    } catch (e) {
      // Quota / private mode — ignore; in-memory state still works.
    }
  }

  // Grok-class mismatch notes (documented for agents / tests).
  const GROK_MISMATCHES = [
    'Esc still interrupts the in-flight turn (Claude-Code parity); it does not cancel the focused queue item.',
    'Cmd+Enter is not an interject chord — only Control+Enter interrupts while busy (plain/meta Enter enqueues).',
    'No dedicated keyboard cancel for the focused queue row beyond strip cancel / take; Grok may bind extra chords.',
    // 🎯T154 residual: queue items are text-only; composer images are not persisted.
    'Send-queue persistence is text-only: images at enqueue are not stored (pendingImages cleared); reload restores text bodies only. In-flight draft in #input stays separate from the queue.',
  ];

  return {
    STORAGE_KEY: STORAGE_KEY,
    emptyState: emptyState,
    decideSend: decideSend,
    shouldInterrupt: shouldInterrupt,
    shouldEnqueue: shouldEnqueue,
    enqueue: enqueue,
    cancel: cancel,
    updateItem: updateItem,
    takeById: takeById,
    shiftNext: shiftNext,
    cycleNav: cycleNav,
    textForFocus: textForFocus,
    serialize: serialize,
    deserialize: deserialize,
    load: load,
    save: save,
    GROK_MISMATCHES: GROK_MISMATCHES,
  };
}));
