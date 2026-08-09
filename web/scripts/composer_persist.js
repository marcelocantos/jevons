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
    module.exports = factory(require('./pending_turns.js'));
  } else {
    // Browser: pending_turns.js must load before this script.
    root.ComposerPersist = factory(root.PendingTurns);
  }
}(typeof self !== 'undefined' ? self : this, function (PendingTurns) {
  'use strict';

  const DRAFT_KEY = 'jevons-composer-draft-v1';
  const PENDING_KEY = 'jevons-pending-send-v1';
  // Legacy hot-reload path (T183) — migrate once into localStorage.
  const LEGACY_SESSION_KEY = 'jevons-input';

  function emptyDraft() {
    return { text: '', updatedAt: 0 };
  }

  function emptyPending() {
    return PendingTurns.empty();
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

  // ── Pending (unacked owner turns) ───────────────────
  //
  // 🎯T372: the ALGORITHM lives in PendingTurns, shared with every agent
  // surface; main is just the agent `PendingTurns.MAIN_AGENT`. What stays here
  // is the part that is genuinely main's: localStorage durability and the
  // send-queue restore plan. These wrappers keep main's historical call
  // signatures (agent-free) so index.html and this module's tests are
  // unchanged, while the behaviour underneath is the one shared contract.

  function serializePending(state) {
    return PendingTurns.serialize(state);
  }

  function deserializePending(raw) {
    return PendingTurns.deserialize(raw);
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
    return PendingTurns.stage(state, PendingTurns.MAIN_AGENT, text);
  }

  // Drop pending items whose text appears in chatlog/history user bodies.
  function ackPendingAgainstHistory(state, historyTexts) {
    return PendingTurns.ackTexts(state, PendingTurns.MAIN_AGENT, historyTexts);
  }

  // Texts still unacked — re-queue these (caller enqueues into SendQueue).
  function unackedTexts(state) {
    return PendingTurns.unackedTexts(state, PendingTurns.MAIN_AGENT);
  }

  // True when history already contains text (merge / no-dupe helper).
  function historyHasText(historyTexts, text) {
    return PendingTurns.hasText(historyTexts, text);
  }

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
