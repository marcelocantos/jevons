// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Pure attention-thread model for human↔overseer chat (🎯T65).
// DOM-free so Node hermetic tests can require() it.
//
// Prefix-first / voice-first: aside:, capture:, park:, main:, pursue:, target:
// Case-insensitive; strip prefix before routing. No button-primary API.
// target: opens a short-lived filing aside (🎯T93/T95).

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.AttentionThreads = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  const MAIN_ID = 'main';
  const STORAGE_KEY = 'jevons-attention-threads-v1';

  // Thread lifecycle (🎯T95.1):
  //   open    — active workstream / filing aside (default chrome)
  //   parked  — owner deliberately parked; still shown as quiet chip
  //   done    — completed/dismissed; archive only, not default chrome
  // Legacy alias "archived" normalizes to done on clone/load.

  // Recognized composer prefixes (voice or type). Order irrelevant.
  const COMMANDS = new Set(['aside', 'capture', 'park', 'main', 'pursue', 'target']);

  // Leading command: optional whitespace, word, optional space, colon, rest.
  // Matches "aside: foo", "ASIDE:foo", "capture : bar", "target: file this".
  const PREFIX_RE = /^\s*(aside|capture|park|main|pursue|target)\s*:\s*/i;

  function now() {
    return Date.now();
  }

  function newId() {
    return 'att-' + now().toString(36) + '-' + Math.random().toString(36).slice(2, 8);
  }

  // Normalize status for clone/serialize. Unknown → open.
  function normalizeStatus(status) {
    if (status === 'parked') return 'parked';
    if (status === 'done' || status === 'archived') return 'done';
    return 'open';
  }

  // 🎯T95.1 migrate: legacy auto-close used park() for file-target asides.
  // Those are completed filings, not intentional T65 parks — treat as done so
  // they leave default chrome after refresh (localStorage still has parked).
  function migrateThreadStatus(purpose, status) {
    const st = normalizeStatus(status);
    if (purpose === 'file-target' && st === 'parked') return 'done';
    return st;
  }

  function isChromeVisible(status) {
    // Default attention bar: open + intentionally parked only.
    return status === 'open' || status === 'parked';
  }

  function titleFromBody(body) {
    const line = String(body || '').trim().split(/\r?\n/).find(function (l) {
      return l.trim().length > 0;
    }) || '';
    const t = line.trim();
    if (!t) return 'Untitled';
    return t.length > 48 ? t.slice(0, 45) + '…' : t;
  }

  // shortTitle for composer placeholder: [aside: short-title] …
  function shortTitle(title, maxLen) {
    const n = typeof maxLen === 'number' && maxLen > 0 ? maxLen : 28;
    const t = String(title || 'aside').replace(/[\[\]]/g, '').trim() || 'aside';
    return t.length > n ? t.slice(0, Math.max(1, n - 1)) + '…' : t;
  }

  // Composer placeholder for current focus (UI may call this).
  // Main: clean base. Side: "[aside: short-title] Write a message to Jevons"
  function composerPlaceholder(state, mainPlaceholder) {
    const base = mainPlaceholder || 'Write a message to Jevons';
    if (isMainFocus(state)) return base;
    const t = findThread(state, state && state.focusId);
    return '[aside: ' + shortTitle(t && t.title) + '] Write a message to Jevons';
  }

  function emptyState() {
    return {
      focusId: MAIN_ID,
      threads: [],
    };
  }

  function clone(state) {
    return {
      focusId: state && state.focusId ? state.focusId : MAIN_ID,
      threads: ((state && state.threads) || []).map(function (t) {
        return {
          id: t.id,
          title: t.title,
          body: t.body,
          status: normalizeStatus(t.status),
          purpose: t.purpose === 'file-target' ? 'file-target' : (t.purpose || ''),
          createdAt: t.createdAt,
          updatedAt: t.updatedAt,
        };
      }),
    };
  }

  function findThread(state, id) {
    if (!state || !id || id === MAIN_ID) return null;
    return (state.threads || []).find(function (t) { return t.id === id; }) || null;
  }

  function isMainFocus(state) {
    return !state || !state.focusId || state.focusId === MAIN_ID;
  }

  // parsePrefix: { command|null, body, raw }
  function parsePrefix(draft) {
    const raw = String(draft || '');
    const m = raw.match(PREFIX_RE);
    if (!m) {
      return { command: null, body: raw.replace(/^\s+/, ''), raw: raw };
    }
    const command = m[1].toLowerCase();
    if (!COMMANDS.has(command)) {
      return { command: null, body: raw.replace(/^\s+/, ''), raw: raw };
    }
    return {
      command: command,
      body: raw.slice(m[0].length),
      raw: raw,
    };
  }

  // capture: arrest body into a new open side thread; focus stays main.
  // Returns { state, id } or null if body empty.
  function capture(state, body) {
    const text = String(body || '').trim();
    if (!text) return null;
    const s = clone(state || emptyState());
    const id = newId();
    const ts = now();
    s.threads.unshift({
      id: id,
      title: titleFromBody(text),
      body: text,
      status: 'open',
      purpose: '',
      createdAt: ts,
      updatedAt: ts,
    });
    s.focusId = MAIN_ID;
    return { state: s, id: id };
  }

  // openTargetAside: purpose-bound filing thread (🎯T95). Focus stays main
  // so the owner is not forced into a multi-day attention workstream.
  function openTargetAside(state, body) {
    const text = String(body || '').trim();
    if (!text) return null;
    const s = clone(state || emptyState());
    const id = newId();
    const ts = now();
    s.threads.unshift({
      id: id,
      title: titleFromBody(text),
      body: text,
      status: 'open',
      purpose: 'file-target',
      createdAt: ts,
      updatedAt: ts,
    });
    s.focusId = MAIN_ID;
    return { state: s, id: id };
  }

  // dismiss: mark aside done — leaves default chrome, stays in archive (🎯T95.1).
  // Focus returns to main if the dismissed thread was focused.
  function dismiss(state, id) {
    const s = clone(state || emptyState());
    const t = findThread(s, id);
    if (!t) return s;
    t.status = 'done';
    t.updatedAt = now();
    if (s.focusId === id) s.focusId = MAIN_ID;
    return s;
  }

  // closeTargetAside: auto-close after successful 🎯 file (or explicit).
  // Close = dismiss from chrome + focus main — not park-and-still-show (🎯T95/T95.1).
  // If id omitted, closes the most recent open file-target aside.
  function closeTargetAside(state, id) {
    const s = clone(state || emptyState());
    let target = null;
    if (id) {
      target = findThread(s, id);
    } else {
      target = (s.threads || []).find(function (t) {
        return t.purpose === 'file-target' && t.status === 'open';
      }) || null;
    }
    if (!target || target.purpose !== 'file-target') return s;
    return dismiss(s, target.id);
  }

  // Wire text for target: asides — overseer must clarify if needed, file
  // via jevons_target_file, then emit __TARGET_FILED__:Tn confirmation.
  function formatTargetWire(id, title, body) {
    const t = String(title || 'target').replace(/[\[\]]/g, '');
    return '[target-aside: ' + id + ' | ' + t + ']\n' + String(body || '').trim() +
      '\n\n(Ceremony: short-lived target filing aside. Clarify acceptance if needed, ' +
      'file with jevons_target_file, confirm 🎯 id. Include __TARGET_FILED__:Tn on success.)';
  }

  // detectTargetFiled: parse overseer/tool text for __TARGET_FILED__:Tn.
  function detectTargetFiled(text) {
    const m = String(text || '').match(/__TARGET_FILED__\s*:\s*(T[0-9]+(?:\.[0-9]+)?)/i);
    if (!m) return null;
    return m[1].toUpperCase().replace(/^T/, 'T'); // normalize T prefix
  }

  function park(state, id) {
    const s = clone(state || emptyState());
    const t = findThread(s, id);
    if (!t) return s;
    t.status = 'parked';
    t.updatedAt = now();
    if (s.focusId === id) s.focusId = MAIN_ID;
    return s;
  }

  function parkByQuery(state, query) {
    const s = clone(state || emptyState());
    const q = String(query || '').trim().toLowerCase();
    let target = null;
    if (q) {
      // Prefer chrome-visible matches; do not re-park archive entries by title.
      target = (s.threads || []).find(function (t) {
        if (!isChromeVisible(t.status)) return false;
        return (t.title || '').toLowerCase().indexOf(q) !== -1 ||
          (t.body || '').toLowerCase().indexOf(q) !== -1;
      }) || null;
    } else if (!isMainFocus(s)) {
      target = findThread(s, s.focusId);
    } else {
      // No query + main focus: park most recent open thread.
      target = (s.threads || []).find(function (t) { return t.status === 'open'; }) || null;
    }
    if (!target || target.status === 'done') return s;
    return park(s, target.id);
  }

  function pursue(state, id) {
    const s = clone(state || emptyState());
    const t = findThread(s, id);
    if (!t) return s;
    // Re-open parked or archived workstreams when the owner pursues them.
    t.status = 'open';
    t.updatedAt = now();
    s.focusId = id;
    return s;
  }

  function pursueByQuery(state, query) {
    const s = clone(state || emptyState());
    const q = String(query || '').trim().toLowerCase();
    if (!q) return s;
    const target = (s.threads || []).find(function (t) {
      return (t.title || '').toLowerCase().indexOf(q) !== -1 ||
        (t.body || '').toLowerCase().indexOf(q) !== -1;
    });
    if (!target) return s;
    return pursue(s, target.id);
  }

  function focusMain(state) {
    const s = clone(state || emptyState());
    s.focusId = MAIN_ID;
    return s;
  }

  function updateActiveBody(state, body) {
    const s = clone(state || emptyState());
    if (isMainFocus(s)) return s;
    const t = findThread(s, s.focusId);
    if (!t) return s;
    t.body = String(body || '');
    t.title = titleFromBody(t.body) || t.title;
    t.updatedAt = now();
    return s;
  }

  // stack: default attention chrome — open first, then intentionally parked.
  // Completed (done) asides are excluded (🎯T95.1); use archive() for those.
  function stack(state) {
    const s = state || emptyState();
    const open = [];
    const parked = [];
    (s.threads || []).forEach(function (t) {
      if (t.status === 'done') return;
      if (t.status === 'parked') parked.push(t);
      else open.push(t);
    });
    return open.concat(parked);
  }

  // archive / archivedStack: discoverable history of completed asides.
  function archive(state) {
    const s = state || emptyState();
    return (s.threads || []).filter(function (t) {
      return t.status === 'done';
    });
  }

  function archivedStack(state) {
    return archive(state);
  }

  function formatAsideWire(id, title, body) {
    const safeTitle = String(title || 'Untitled').replace(/[\r\n\]]/g, ' ').trim();
    return '[attention:' + id + '|' + safeTitle + ']\n' + body;
  }

  // handleComposer: single entry for send / local commands.
  // Returns:
  //   {
  //     kind: 'send' | 'local' | 'empty',
  //     text: wire text (send only),
  //     state,
  //     clearComposer: bool,
  //     composerBody: string|null  // body to place in composer after local pursue
  //     threadId: optional
  //   }
  function handleComposer(state, draft) {
    const s0 = clone(state || emptyState());
    const parsed = parsePrefix(draft);
    const body = parsed.body;
    const bodyTrim = String(body || '').trim();

    if (parsed.command === 'capture') {
      if (!bodyTrim) {
        return { kind: 'empty', text: '', state: s0, clearComposer: false, composerBody: null };
      }
      const cap = capture(s0, bodyTrim);
      return {
        kind: 'local',
        text: '',
        state: cap.state,
        clearComposer: true,
        composerBody: '',
        threadId: cap.id,
      };
    }

    if (parsed.command === 'park') {
      const next = parkByQuery(s0, bodyTrim);
      return {
        kind: 'local',
        text: '',
        state: next,
        clearComposer: true,
        composerBody: '',
      };
    }

    if (parsed.command === 'pursue') {
      const next = pursueByQuery(s0, bodyTrim);
      const t = !isMainFocus(next) ? findThread(next, next.focusId) : null;
      return {
        kind: 'local',
        text: '',
        state: next,
        clearComposer: true,
        composerBody: t ? (t.body || '') : '',
      };
    }

    if (parsed.command === 'main') {
      const next = focusMain(s0);
      if (!bodyTrim) {
        return {
          kind: 'local',
          text: '',
          state: next,
          clearComposer: true,
          composerBody: '',
        };
      }
      return {
        kind: 'send',
        text: bodyTrim,
        state: next,
        clearComposer: true,
        composerBody: '',
        routed: false,
      };
    }

    if (parsed.command === 'aside') {
      if (!bodyTrim) {
        return { kind: 'empty', text: '', state: s0, clearComposer: false, composerBody: null };
      }
      const cap = capture(s0, bodyTrim);
      const t = findThread(cap.state, cap.id);
      const wire = formatAsideWire(cap.id, t ? t.title : titleFromBody(bodyTrim), bodyTrim);
      return {
        kind: 'send',
        text: wire,
        state: cap.state, // focus remains main
        clearComposer: true,
        composerBody: '',
        routed: true,
        threadId: cap.id,
      };
    }

    // target: short-lived filing aside (🎯T93/T95) — not a general T65 workstream.
    if (parsed.command === 'target') {
      if (!bodyTrim) {
        return { kind: 'empty', text: '', state: s0, clearComposer: false, composerBody: null };
      }
      const cap = openTargetAside(s0, bodyTrim);
      const t = findThread(cap.state, cap.id);
      const wire = formatTargetWire(cap.id, t ? t.title : titleFromBody(bodyTrim), bodyTrim);
      return {
        kind: 'send',
        text: wire,
        state: cap.state,
        clearComposer: true,
        composerBody: '',
        routed: true,
        threadId: cap.id,
        purpose: 'file-target',
      };
    }

    // No command prefix.
    const text = bodyTrim;
    if (!text) {
      return { kind: 'empty', text: '', state: s0, clearComposer: false, composerBody: null };
    }

    if (!isMainFocus(s0)) {
      const t = findThread(s0, s0.focusId);
      if (t) {
        t.body = text;
        t.title = titleFromBody(text) || t.title;
        t.status = 'open';
        t.updatedAt = now();
        const wire = formatAsideWire(t.id, t.title, text);
        return {
          kind: 'send',
          text: wire,
          state: s0,
          clearComposer: true,
          composerBody: '',
          routed: true,
          threadId: t.id,
        };
      }
      s0.focusId = MAIN_ID;
    }

    return {
      kind: 'send',
      text: text,
      state: s0,
      clearComposer: true,
      composerBody: '',
      routed: false,
    };
  }

  // prepareSend: thin wrapper — only produces wire text for kind==='send'.
  // Prefer handleComposer for full command surface.
  function prepareSend(state, draft) {
    const r = handleComposer(state, draft);
    if (r.kind !== 'send') {
      return {
        text: '',
        state: r.state,
        routed: false,
        kind: r.kind,
        clearComposer: r.clearComposer,
        composerBody: r.composerBody,
        threadId: r.threadId,
      };
    }
    return {
      text: r.text,
      state: r.state,
      routed: !!r.routed,
      kind: 'send',
      clearComposer: r.clearComposer,
      composerBody: r.composerBody,
      threadId: r.threadId,
    };
  }

  function serialize(state) {
    return JSON.stringify(clone(state || emptyState()));
  }

  function deserialize(raw) {
    if (!raw) return emptyState();
    try {
      const parsed = typeof raw === 'string' ? JSON.parse(raw) : raw;
      if (!parsed || typeof parsed !== 'object') return emptyState();
      const s = emptyState();
      s.focusId = parsed.focusId === MAIN_ID || !parsed.focusId ? MAIN_ID : String(parsed.focusId);
      if (Array.isArray(parsed.threads)) {
        s.threads = parsed.threads
          .filter(function (t) { return t && t.id; })
          .map(function (t) {
            const purpose = t.purpose === 'file-target' ? 'file-target' : '';
            return {
              id: String(t.id),
              title: String(t.title || 'Untitled'),
              body: String(t.body || ''),
              status: migrateThreadStatus(purpose, t.status),
              purpose: purpose,
              createdAt: Number(t.createdAt) || now(),
              updatedAt: Number(t.updatedAt) || now(),
            };
          });
      }
      if (s.focusId !== MAIN_ID && !findThread(s, s.focusId)) {
        s.focusId = MAIN_ID;
      }
      return s;
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

  return {
    MAIN_ID: MAIN_ID,
    STORAGE_KEY: STORAGE_KEY,
    COMMANDS: COMMANDS,
    emptyState: emptyState,
    clone: clone,
    findThread: findThread,
    isMainFocus: isMainFocus,
    parsePrefix: parsePrefix,
    capture: capture,
    openTargetAside: openTargetAside,
    dismiss: dismiss,
    closeTargetAside: closeTargetAside,
    formatTargetWire: formatTargetWire,
    detectTargetFiled: detectTargetFiled,
    park: park,
    parkByQuery: parkByQuery,
    pursue: pursue,
    pursueByQuery: pursueByQuery,
    focusMain: focusMain,
    updateActiveBody: updateActiveBody,
    stack: stack,
    archive: archive,
    archivedStack: archivedStack,
    normalizeStatus: normalizeStatus,
    isChromeVisible: isChromeVisible,
    handleComposer: handleComposer,
    prepareSend: prepareSend,
    formatAsideWire: formatAsideWire,
    serialize: serialize,
    deserialize: deserialize,
    load: load,
    save: save,
    titleFromBody: titleFromBody,
    shortTitle: shortTitle,
    composerPlaceholder: composerPlaceholder,
  };
}));
