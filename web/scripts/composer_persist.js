// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T239 — durable composer draft + pending (unacked) send in localStorage.
// DOM-free so Node hermetic tests can require().
//
// Gap vs T154/T183:
//   - T154 already persists the busy/offline send queue (localStorage).
//   - T183 restored sessionStorage draft for visibility only, and only
//     saved on hot-reload WS — not continuous, not across daemon restart
//     that reloads without that path (e.g. transport version change).
//   - Wire accept then daemon death lost the submitted body (cleared
//     composer with no durable staging until chatlog echo).
//
// Design:
//   - Draft: continuous localStorage snapshot of seed-stripped live text.
//   - Pending: text accepted by transport.send but not yet seen as a user
//     echo / chatlog line — re-queue on restore if still missing.
//   - Merge: if server history already contains the pending body, clear
//     pending (no duplicate re-send). Queue items stay under SendQueue.
//   - Fail loud: load reports error when key present but unreadable.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ComposerPersist = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  const DRAFT_KEY = 'jevons-composer-draft-v1';
  const PENDING_KEY = 'jevons-pending-send-v1';
  // Legacy hot-reload path (T183) — migrate once into localStorage.
  const LEGACY_SESSION_KEY = 'jevons-input';

  function emptyDraft() {
    return { text: '', updatedAt: 0 };
  }

  function emptyPending() {
    return { items: [] };
  }

  function normalizeText(t) {
    return String(t == null ? '' : t);
  }

  // ── Draft ────────────────────────────────────────────────────────

  function serializeDraft(state) {
    const s = state || emptyDraft();
    return JSON.stringify({
      text: normalizeText(s.text),
      updatedAt: typeof s.updatedAt === 'number' ? s.updatedAt : Date.now(),
    });
  }

  function deserializeDraft(raw) {
    if (raw == null || raw === '') {
      return { ok: true, state: emptyDraft(), present: false };
    }
    // Plain string (legacy sessionStorage) is valid non-empty draft.
    if (typeof raw === 'string' && raw.charAt(0) !== '{' && raw.charAt(0) !== '[') {
      return {
        ok: true,
        state: { text: raw, updatedAt: 0 },
        present: true,
      };
    }
    try {
      const parsed = typeof raw === 'string' ? JSON.parse(raw) : raw;
      if (typeof parsed === 'string') {
        return {
          ok: true,
          state: { text: parsed, updatedAt: 0 },
          present: true,
        };
      }
      if (!parsed || typeof parsed !== 'object') {
        return {
          ok: false,
          state: emptyDraft(),
          present: true,
          error: 'draft: not an object',
        };
      }
      return {
        ok: true,
        state: {
          text: normalizeText(parsed.text),
          updatedAt: typeof parsed.updatedAt === 'number' ? parsed.updatedAt : 0,
        },
        present: true,
      };
    } catch (e) {
      return {
        ok: false,
        state: emptyDraft(),
        present: true,
        error: 'draft: parse failed — ' + (e && e.message ? e.message : String(e)),
      };
    }
  }

  function loadDraft(storage) {
    if (!storage || typeof storage.getItem !== 'function') {
      return { ok: true, state: emptyDraft(), present: false };
    }
    let raw;
    try {
      raw = storage.getItem(DRAFT_KEY);
    } catch (e) {
      return {
        ok: false,
        state: emptyDraft(),
        present: true,
        error: 'draft: getItem failed — ' + (e && e.message ? e.message : String(e)),
      };
    }
    if (raw == null || raw === '') {
      return { ok: true, state: emptyDraft(), present: false };
    }
    return deserializeDraft(raw);
  }

  function saveDraft(storage, text) {
    if (!storage || typeof storage.setItem !== 'function') return { ok: false, error: 'no storage' };
    const t = normalizeText(text);
    try {
      if (!t.trim()) {
        try { storage.removeItem(DRAFT_KEY); } catch (_) { /* ignore */ }
        return { ok: true, cleared: true };
      }
      storage.setItem(DRAFT_KEY, serializeDraft({ text: t, updatedAt: Date.now() }));
      return { ok: true };
    } catch (e) {
      return {
        ok: false,
        error: 'draft: setItem failed — ' + (e && e.message ? e.message : String(e)),
      };
    }
  }

  function clearDraft(storage) {
    if (!storage || typeof storage.removeItem !== 'function') return;
    try { storage.removeItem(DRAFT_KEY); } catch (_) { /* ignore */ }
  }

  // Migrate legacy sessionStorage draft into localStorage once.
  // Prefer localStorage if already non-empty. Returns restore result
  // suitable for boot: { ok, text, error?, migrated? }.
  function restoreDraft(localStorage, sessionStorage) {
    const primary = loadDraft(localStorage);
    if (!primary.ok && primary.present) {
      return {
        ok: false,
        text: '',
        error: primary.error || 'draft restore failed',
        present: true,
      };
    }
    let text = primary.state && primary.state.text ? String(primary.state.text) : '';
    let migrated = false;
    if (!text.trim() && sessionStorage && typeof sessionStorage.getItem === 'function') {
      let legacy = null;
      try { legacy = sessionStorage.getItem(LEGACY_SESSION_KEY); } catch (_) { legacy = null; }
      if (legacy != null && String(legacy).length > 0) {
        text = String(legacy);
        migrated = true;
        saveDraft(localStorage, text);
        try { sessionStorage.removeItem(LEGACY_SESSION_KEY); } catch (_) { /* ignore */ }
      }
    }
    return { ok: true, text: text, present: !!text.trim() || primary.present, migrated: migrated };
  }

  // ── Pending (unacked wire sends) ─────────────────────────────────

  function serializePending(state) {
    const s = state || emptyPending();
    const items = (s.items || []).map(function (it) {
      return {
        id: String(it && it.id != null ? it.id : ''),
        text: normalizeText(it && it.text),
        stagedAt: typeof (it && it.stagedAt) === 'number' ? it.stagedAt : 0,
      };
    }).filter(function (it) { return it.id.length > 0 && it.text.trim().length > 0; });
    return JSON.stringify({ items: items });
  }

  function deserializePending(raw) {
    if (raw == null || raw === '') {
      return { ok: true, state: emptyPending(), present: false };
    }
    try {
      const parsed = typeof raw === 'string' ? JSON.parse(raw) : raw;
      if (!parsed || typeof parsed !== 'object') {
        return {
          ok: false,
          state: emptyPending(),
          present: true,
          error: 'pending: not an object',
        };
      }
      const items = [];
      if (Array.isArray(parsed.items)) {
        for (let i = 0; i < parsed.items.length; i++) {
          const it = parsed.items[i];
          if (!it || it.id == null || it.id === '') continue;
          const text = normalizeText(it.text);
          if (!text.trim()) continue;
          items.push({
            id: String(it.id),
            text: text,
            stagedAt: typeof it.stagedAt === 'number' ? it.stagedAt : 0,
          });
        }
      }
      return { ok: true, state: { items: items }, present: true };
    } catch (e) {
      return {
        ok: false,
        state: emptyPending(),
        present: true,
        error: 'pending: parse failed — ' + (e && e.message ? e.message : String(e)),
      };
    }
  }

  function loadPending(storage) {
    if (!storage || typeof storage.getItem !== 'function') {
      return { ok: true, state: emptyPending(), present: false };
    }
    let raw;
    try {
      raw = storage.getItem(PENDING_KEY);
    } catch (e) {
      return {
        ok: false,
        state: emptyPending(),
        present: true,
        error: 'pending: getItem failed — ' + (e && e.message ? e.message : String(e)),
      };
    }
    if (raw == null || raw === '') {
      return { ok: true, state: emptyPending(), present: false };
    }
    return deserializePending(raw);
  }

  function savePending(storage, state) {
    if (!storage || typeof storage.setItem !== 'function') return { ok: false, error: 'no storage' };
    const s = state || emptyPending();
    try {
      if (!s.items || !s.items.length) {
        try { storage.removeItem(PENDING_KEY); } catch (_) { /* ignore */ }
        return { ok: true, cleared: true };
      }
      storage.setItem(PENDING_KEY, serializePending(s));
      return { ok: true };
    } catch (e) {
      return {
        ok: false,
        error: 'pending: setItem failed — ' + (e && e.message ? e.message : String(e)),
      };
    }
  }

  function stagePending(state, text) {
    const s = state || emptyPending();
    const t = normalizeText(text).trim();
    if (!t) return s;
    // Avoid staging the same body twice while waiting for ack.
    for (let i = 0; i < (s.items || []).length; i++) {
      if (s.items[i].text === t) return s;
    }
    const id = 'p' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 8);
    return {
      items: (s.items || []).concat([{ id: id, text: t, stagedAt: Date.now() }]),
    };
  }

  // Drop pending items whose text appears in chatlog/history user bodies.
  // Exact match on trimmed text; first occurrence consumes one pending.
  function ackPendingAgainstHistory(state, historyTexts) {
    const s = state || emptyPending();
    const hist = (historyTexts || []).map(function (t) {
      return normalizeText(t).trim();
    }).filter(function (t) { return t.length > 0; });
    if (!s.items || !s.items.length) return { state: emptyPending(), acked: [] };
    const remaining = [];
    const acked = [];
    const used = {};
    for (let i = 0; i < s.items.length; i++) {
      const it = s.items[i];
      let found = false;
      for (let h = 0; h < hist.length; h++) {
        if (used[h]) continue;
        if (hist[h] === it.text) {
          used[h] = true;
          found = true;
          acked.push(it);
          break;
        }
      }
      if (!found) remaining.push(it);
    }
    return { state: { items: remaining }, acked: acked };
  }

  // Texts still unacked — re-queue these (caller enqueues into SendQueue).
  function unackedTexts(state) {
    const s = state || emptyPending();
    return (s.items || []).map(function (it) { return it.text; });
  }

  // True when history already contains text (merge / no-dupe helper).
  function historyHasText(historyTexts, text) {
    const t = normalizeText(text).trim();
    if (!t) return false;
    const hist = historyTexts || [];
    for (let i = 0; i < hist.length; i++) {
      if (normalizeText(hist[i]).trim() === t) return true;
    }
    return false;
  }

  // Plan restore after reload: draft text + which pending bodies to
  // re-enqueue (not already in history and not already in queue).
  function planRestore(opts) {
    const o = opts || {};
    const draftText = normalizeText(o.draftText);
    const pending = o.pendingState || emptyPending();
    const historyTexts = o.historyTexts || [];
    const queueTexts = o.queueTexts || [];
    const ack = ackPendingAgainstHistory(pending, historyTexts);
    const requeue = [];
    const queueSet = {};
    for (let i = 0; i < queueTexts.length; i++) {
      queueSet[normalizeText(queueTexts[i]).trim()] = true;
    }
    for (let j = 0; j < ack.state.items.length; j++) {
      const t = ack.state.items[j].text;
      if (queueSet[t]) continue; // already in durable queue
      if (historyHasText(historyTexts, t)) continue;
      requeue.push(t);
      queueSet[t] = true;
    }
    return {
      draftText: draftText,
      pendingAfterAck: ack.state,
      requeueTexts: requeue,
      acked: ack.acked,
    };
  }

  return {
    DRAFT_KEY: DRAFT_KEY,
    PENDING_KEY: PENDING_KEY,
    LEGACY_SESSION_KEY: LEGACY_SESSION_KEY,
    emptyDraft: emptyDraft,
    emptyPending: emptyPending,
    serializeDraft: serializeDraft,
    deserializeDraft: deserializeDraft,
    loadDraft: loadDraft,
    saveDraft: saveDraft,
    clearDraft: clearDraft,
    restoreDraft: restoreDraft,
    serializePending: serializePending,
    deserializePending: deserializePending,
    loadPending: loadPending,
    savePending: savePending,
    stagePending: stagePending,
    ackPendingAgainstHistory: ackPendingAgainstHistory,
    unackedTexts: unackedTexts,
    historyHasText: historyHasText,
    planRestore: planRestore,
  };
}));
