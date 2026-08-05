// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Pure attention/aside model for human↔overseer chat (🎯T65 / 🎯T136).
// DOM-free so Node hermetic tests can require() it.
//
// Prefix-first / voice-first: aside:, capture:, park:, main:, pursue:, target:
// Case-insensitive; strip prefix before routing. No button-primary API.
// target: opens a short-lived filing aside (🎯T93/T95).
//
// 🎯T136: owner-facing chrome for asides lives in the RHS fleet tree, not
// the top attention chip bar. chromeStack() is always empty; stack() remains
// for internal route/park model + wire. Create paths dual-write purpose=aside
// agents into /api/agents.

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
  // 🎯T134: cap default bar chips; overflow is "+N more", not multi-row wall.
  const MAX_VISIBLE_CHIPS = 6;

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

  // 🎯T134: fingerprint for near-dup capture (normalize first line / title).
  function normalizeFingerprint(text) {
    return String(text || '')
      .toLowerCase()
      .replace(/\s+/g, ' ')
      .trim()
      .replace(/[….]+\s*$/, '')
      .trim();
  }

  function fingerprintForThread(title, body) {
    const first = String(body || '').trim().split(/\r?\n/).find(function (l) {
      return l.trim().length > 0;
    }) || '';
    const raw = first.trim() || String(title || '').trim();
    return normalizeFingerprint(raw);
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

  // Find open chrome thread with same fingerprint (🎯T134 dedupe).
  // Does not match done/parked — only live open workstreams.
  function findOpenDup(state, text, purpose) {
    const fp = fingerprintForThread(titleFromBody(text), text);
    if (!fp) return null;
    const wantPurpose = purpose === 'file-target' ? 'file-target' : '';
    return ((state && state.threads) || []).find(function (t) {
      if (t.status !== 'open') return false;
      const tPurpose = t.purpose === 'file-target' ? 'file-target' : '';
      if (tPurpose !== wantPurpose) return false;
      return fingerprintForThread(t.title, t.body) === fp;
    }) || null;
  }

  // capture: arrest body into a new open side thread; focus stays main.
  // Near-dup open thread → update existing instead of stacking (🎯T134).
  // Returns { state, id, merged? } or null if body empty.
  function capture(state, body) {
    const text = String(body || '').trim();
    if (!text) return null;
    const s = clone(state || emptyState());
    const ts = now();
    const existing = findOpenDup(s, text, '');
    if (existing) {
      existing.body = text;
      existing.title = titleFromBody(text) || existing.title;
      existing.updatedAt = ts;
      // Bump to front of list so chrome order reflects latest capture.
      s.threads = [existing].concat(s.threads.filter(function (t) {
        return t.id !== existing.id;
      }));
      s.focusId = MAIN_ID;
      return { state: s, id: existing.id, merged: true };
    }
    const id = newId();
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
    return { state: s, id: id, merged: false };
  }

  // openTargetAside: purpose-bound filing thread (🎯T95). Focus stays main
  // so the owner is not forced into a multi-day attention workstream.
  // Dedupe open file-target asides by fingerprint (🎯T134).
  function openTargetAside(state, body) {
    const text = String(body || '').trim();
    if (!text) return null;
    const s = clone(state || emptyState());
    const ts = now();
    const existing = findOpenDup(s, text, 'file-target');
    if (existing) {
      existing.body = text;
      existing.title = titleFromBody(text) || existing.title;
      existing.updatedAt = ts;
      s.threads = [existing].concat(s.threads.filter(function (t) {
        return t.id !== existing.id;
      }));
      s.focusId = MAIN_ID;
      return { state: s, id: existing.id, merged: true };
    }
    const id = newId();
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
    return { state: s, id: id, merged: false };
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

  // resolveTargetAsideIdsToDismiss: fleet dual-write ids to DELETE after a
  // live __TARGET_FILED__ paint (🎯T164). Prefer open file-target; also any
  // file-target thread still present in the fleet list (stopped zombies after
  // local attention already closed). Does not invent ids for non-filing asides.
  function resolveTargetAsideIdsToDismiss(state, fleetAgents, selectedAgent) {
    const s = state || emptyState();
    const fleet = Array.isArray(fleetAgents) ? fleetAgents : [];
    const threads = s.threads || [];
    const ids = [];
    function push(id) {
      if (!id) return;
      if (ids.indexOf(id) >= 0) return;
      ids.push(id);
    }
    const open = threads.find(function (t) {
      return t && t.purpose === 'file-target' && t.status === 'open' && t.id;
    });
    if (open) push(open.id);
    threads.forEach(function (t) {
      if (!t || t.purpose !== 'file-target' || !t.id) return;
      const inFleet = fleet.some(function (a) {
        return a && a.name === t.id;
      });
      if (inFleet) push(t.id);
    });
    // Selected RHS row if it is this filing aside (still dual-written).
    if (selectedAgent) {
      const th = threads.find(function (t) {
        return t && t.purpose === 'file-target' && t.id === selectedAgent;
      });
      if (th) {
        const row = fleet.find(function (a) {
          return a && a.name === selectedAgent;
        });
        if (row) push(selectedAgent);
      }
    }
    return ids;
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

  // stack: open first, then intentionally parked (model / route helpers).
  // Completed (done) asides are excluded (🎯T95.1); use archive() for those.
  // 🎯T136: do NOT use stack for top-bar chrome — use chromeStack() (always empty).
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

  // chromeStack (🎯T136): top #attention-bar never shows open/parked aside chips.
  // Asides live in the RHS fleet tree. Model stack() still exists for T99 route.
  function chromeStack(/* state */) {
    return [];
  }

  // routeCandidates (🎯T134 / T99): only open chrome threads the owner would
  // expect. Never done/archive ghosts; never parked (auto-route is open-only).
  // Shape ready for ThreadRoute.route (id, title, digest, body, status).
  function routeCandidates(state) {
    const s = state || emptyState();
    return (s.threads || [])
      .filter(function (t) {
        return t && t.id && t.id !== MAIN_ID && t.status === 'open';
      })
      .map(function (t) {
        return {
          id: t.id,
          title: t.title,
          digest: t.body,
          body: t.body,
          status: t.status,
          updatedAt: t.updatedAt,
        };
      });
  }

  // visibleStack: cap for any residual chrome consumers. 🎯T136: chrome is empty.
  // opts.max defaults to MAX_VISIBLE_CHIPS. Uses chromeStack (not model stack).
  function visibleStack(state, opts) {
    opts = opts || {};
    const max = typeof opts.max === 'number' && opts.max > 0
      ? Math.floor(opts.max)
      : MAX_VISIBLE_CHIPS;
    const full = chromeStack(state);
    if (full.length <= max) {
      return { shown: full.slice(), overflowCount: 0, overflow: [] };
    }
    return {
      shown: full.slice(0, max),
      overflowCount: full.length - max,
      overflow: full.slice(max),
    };
  }

  // clearDone: purge archive (done) threads from state (🎯T134 bulk clear).
  function clearDone(state) {
    const s = clone(state || emptyState());
    s.threads = (s.threads || []).filter(function (t) {
      return t.status !== 'done';
    });
    if (s.focusId !== MAIN_ID && !findThread(s, s.focusId)) {
      s.focusId = MAIN_ID;
    }
    return s;
  }

  // dismissAllParked: parked → done (leave open workstreams) (🎯T134).
  function dismissAllParked(state) {
    const s = clone(state || emptyState());
    const ts = now();
    (s.threads || []).forEach(function (t) {
      if (t.status === 'parked') {
        t.status = 'done';
        t.updatedAt = ts;
      }
    });
    if (s.focusId !== MAIN_ID) {
      const f = findThread(s, s.focusId);
      if (!f || f.status === 'done') s.focusId = MAIN_ID;
    }
    return s;
  }

  // dismissAllChrome: open + parked → done (full bar dismiss) (🎯T134).
  function dismissAllChrome(state) {
    const s = clone(state || emptyState());
    const ts = now();
    (s.threads || []).forEach(function (t) {
      if (t.status === 'open' || t.status === 'parked') {
        t.status = 'done';
        t.updatedAt = ts;
      }
    });
    s.focusId = MAIN_ID;
    return s;
  }

  // clearChromeNoise: dismiss all chrome-visible + purge done archive (🎯T134).
  // Owner bulk reset without one-by-one chip dismiss.
  function clearChromeNoise(state) {
    return clearDone(dismissAllChrome(state));
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

  // ── 🎯T250: aside turns stay off main transcript ────────────────────
  // Owner-visible bubbles for attention/target-aside wire text belong in the
  // RHS Transcript tab only (fleet aside chrome). Main #messages must not
  // paint them. Wire may still reach the overseer for processing.

  /**
   * Parse a main-chat user body that is an aside wire marker.
   * Formats:
   *   [attention:<id>|<title>]\n<body>
   *   [target-aside: <id> | <title>]\n<body>\n\n(Ceremony: …)
   * @returns {{ kind: string, id: string, title: string, body: string, displayText: string }|null}
   */
  function parseAsideWireUserText(text) {
    const raw = String(text == null ? '' : text);
    if (!raw) return null;
    // attention: compact id|title, no spaces required around |
    const att = raw.match(
      /^\s*\[attention\s*:\s*([^|\]\r\n]+)\|([^\]]*)\]\s*(?:\r?\n([\s\S]*))?$/i,
    );
    if (att) {
      const id = String(att[1] || '').trim();
      const title = String(att[2] || '').trim();
      const body = String(att[3] != null ? att[3] : '').replace(/^\r?\n/, '');
      if (!id) return null;
      const displayText = body.trim() || title || id;
      return {
        kind: 'attention',
        id: id,
        title: title,
        body: body,
        displayText: displayText,
      };
    }
    // target-aside: id | title (spaces around | optional)
    const tgt = raw.match(
      /^\s*\[target-aside\s*:\s*([^|\]]+?)\s*\|\s*([^\]]*)\]\s*(?:\r?\n([\s\S]*))?$/i,
    );
    if (tgt) {
      const id = String(tgt[1] || '').trim();
      const title = String(tgt[2] || '').trim();
      let body = String(tgt[3] != null ? tgt[3] : '').replace(/^\r?\n/, '');
      // Strip filing ceremony for owner-facing sidebar display.
      body = body.replace(/\n\n\(Ceremony:[\s\S]*$/i, '').trim();
      if (!id) return null;
      const displayText = body || title || id;
      return {
        kind: 'target-aside',
        id: id,
        title: title,
        body: body,
        displayText: displayText,
      };
    }
    return null;
  }

  function isAsideWireUserText(text) {
    return parseAsideWireUserText(text) != null;
  }

  /** Main transcript should paint this user body (false for aside wires). */
  function shouldPaintMainUserText(text) {
    return !isAsideWireUserText(text);
  }

  /**
   * Record an aside wire user turn into a cache keyed by aside id.
   * Dedupes consecutive identical user lines. Mutates cache.
   * @param {Object} cache map id → [{role,text}]
   * @param {string} text full wire user content
   * @returns {{ parsed: object|null, added: boolean }}
   */
  function recordAsideWireUserTurn(cache, text) {
    const p = parseAsideWireUserText(text);
    if (!p || !p.id || !cache || typeof cache !== 'object') {
      return { parsed: p, added: false };
    }
    const display = p.displayText || p.body || p.title || '';
    if (!cache[p.id]) cache[p.id] = [];
    const list = cache[p.id];
    if (list.length) {
      const last = list[list.length - 1];
      if (last && last.role === 'user' && last.text === display) {
        return { parsed: p, added: false };
      }
    }
    list.push({ role: 'user', text: display });
    return { parsed: p, added: true };
  }

  /**
   * Build aside-id → lines map from chatlog frames (history fixture).
   * Only user frames with aside wire markers are included.
   */
  function extractAsideWireTurnsFromFrames(frames) {
    const cache = Object.create(null);
    const list = Array.isArray(frames) ? frames : [];
    for (let i = 0; i < list.length; i++) {
      let m = list[i];
      if (typeof m === 'string') {
        try { m = JSON.parse(m); } catch (_) { continue; }
      }
      if (!m || m.type !== 'user') continue;
      const content = m.message && m.message.content;
      if (typeof content !== 'string' || !content) continue;
      recordAsideWireUserTurn(cache, content);
    }
    return cache;
  }

  /**
   * Merge process-session inspect lines with main-wire aside cache for an id.
   * Wire turns first (owner open body), then process turns; dedupe role+text.
   */
  function mergeInspectLinesWithAsideWire(processLines, wireLines) {
    const out = [];
    const seen = Object.create(null);
    function push(l) {
      if (!l) return;
      const role = l.role === 'user' ? 'user' : (l.role === 'assistant' || l.role === 'jevons' ? 'assistant' : (l.role || 'other'));
      const text = l.text == null ? '' : String(l.text);
      if (!text.trim() && role === 'other') return;
      const k = role + '\0' + text;
      if (seen[k]) return;
      seen[k] = true;
      out.push({ role: role, text: text });
    }
    (wireLines || []).forEach(push);
    (processLines || []).forEach(push);
    return out;
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
    MAX_VISIBLE_CHIPS: MAX_VISIBLE_CHIPS,
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
    resolveTargetAsideIdsToDismiss: resolveTargetAsideIdsToDismiss,
    park: park,
    parkByQuery: parkByQuery,
    pursue: pursue,
    pursueByQuery: pursueByQuery,
    focusMain: focusMain,
    updateActiveBody: updateActiveBody,
    stack: stack,
    chromeStack: chromeStack,
    routeCandidates: routeCandidates,
    visibleStack: visibleStack,
    clearDone: clearDone,
    dismissAllParked: dismissAllParked,
    dismissAllChrome: dismissAllChrome,
    clearChromeNoise: clearChromeNoise,
    archive: archive,
    archivedStack: archivedStack,
    normalizeStatus: normalizeStatus,
    isChromeVisible: isChromeVisible,
    normalizeFingerprint: normalizeFingerprint,
    handleComposer: handleComposer,
    prepareSend: prepareSend,
    formatAsideWire: formatAsideWire,
    // 🎯T250
    parseAsideWireUserText: parseAsideWireUserText,
    isAsideWireUserText: isAsideWireUserText,
    shouldPaintMainUserText: shouldPaintMainUserText,
    recordAsideWireUserTurn: recordAsideWireUserTurn,
    extractAsideWireTurnsFromFrames: extractAsideWireTurnsFromFrames,
    mergeInspectLinesWithAsideWire: mergeInspectLinesWithAsideWire,
    serialize: serialize,
    deserialize: deserialize,
    load: load,
    save: save,
    titleFromBody: titleFromBody,
    shortTitle: shortTitle,
    composerPlaceholder: composerPlaceholder,
  };
}));
