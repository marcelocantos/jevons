// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T372 — THE pending-owner-turn contract. One implementation, agent-keyed.
//
// An "owner turn" is a message the owner sent. Between the moment the owner
// hits send and the moment the server seals that turn into a transcript, the
// turn exists only in the client. Any code that REPLACES the line model in
// that window (history frame, reconnect replay, rehydrate) will delete the
// owner's own words unless the unsealed turn is re-applied. That deletion is
// the 🎯T371 vanish, and 🎯T239/🎯T279 are main chat's cure for the same
// disease.
//
// Before this module the cure existed TWICE:
//   - main:    ComposerPersist.stagePending / ackPendingAgainstHistory,
//              replayed by retainPendingOwnerTurnsVisible (localStorage).
//   - sidebar: ConversationWidget.stagePendingOwnerTurn / ack… / apply…
//              (in-memory).
// Two implementations of one contract is precisely the fork 🎯T372 forbids:
// main and every agent must run the same code path, differing only by role.
// So this module is the single algorithm, and both surfaces delegate to it.
//
// Keying: every turn is keyed by AGENT, and main is simply the agent
// `jevons`. That is not a convenience — it is locked principle 3 (root is not
// a special technical class) expressed in the data model. Main's store holds
// `agent: 'jevons'` items; a sidebar pane holds `agent: 'jv-t371-…'` items;
// the algorithm cannot tell them apart, which is the point.
//
// DURABILITY IS NOT DECIDED HERE. Main persists its store to localStorage;
// the sidebar currently keeps its store in memory. That difference is EC-5 in
// docs/design/one-chat-widget-fork-inventory.md and is an OWNER ruling, not an
// implementer's call. This module is deliberately storage-free and provides
// serialize/deserialize so the ruling is a one-line choice of store per
// surface, not another rewrite.
//
// DOM-free so Node hermetic tests can require() it.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.PendingTurns = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // The main chat surface addresses the overseer as an ordinary agent id.
  // Main's pending items carry this agent; nothing else distinguishes them.
  const MAIN_AGENT = 'jevons';

  // Appended-line marker: this turn is the client's, not the server's.
  const PENDING_FLAG = '_pending';
  const FAILED_FLAG = '_failed';

  // Monotonic within a page load — ids need only be unique inside one
  // pending set, and (agent, text) is already the dedupe key.
  let _seq = 0;

  function empty() {
    return { items: [] };
  }

  function normalizeText(t) {
    return String(t == null ? '' : t);
  }

  // Dedupe/ack key: exact match on trimmed body. One history line consumes
  // one pending item.
  function key(text) {
    return normalizeText(text).trim();
  }

  function agentName(agent) {
    return String(agent == null ? '' : agent).trim();
  }

  function stateOf(state) {
    return state && Array.isArray(state.items) ? state : empty();
  }

  function makeItem(agent, text, when, id, failed) {
    return {
      id: id,
      agent: agent,
      text: text,
      when: when,
      failed: !!failed,
    };
  }

  /**
   * Stage an owner turn as pending for `agent`. Idempotent per (agent, body)
   * while unacked, so a retry cannot double-paint.
   * @param {{items?: Array}} state
   * @param {string} agent Agent id the turn was addressed to (main = 'jevons').
   * @param {string} text
   * @param {{ now?: number, id?: string }} [opts]
   * @returns {{items: Array}} new state (input untouched)
   */
  function stage(state, agent, text, opts) {
    opts = opts || {};
    const s = stateOf(state);
    const name = agentName(agent);
    const body = key(text);
    if (!name || !body) return s;
    for (let i = 0; i < s.items.length; i++) {
      if (s.items[i].agent === name && s.items[i].text === body) return s;
    }
    const when = opts.now !== undefined ? opts.now : Date.now();
    const id = opts.id || ('p' + when.toString(36) + '-' + (_seq++).toString(36));
    return { items: s.items.concat([makeItem(name, body, when, id, false)]) };
  }

  /**
   * Drop `agent`'s pending turns that now appear as user lines. Every other
   * agent's pending set is untouched — pane selection churn cannot
   * cross-contaminate.
   * @param {{items?: Array}} state
   * @param {string} agent
   * @param {Array<{role?: string, text?: string}>} lines
   * @returns {{ state: {items: Array}, acked: Array }}
   */
  function ack(state, agent, lines) {
    const texts = [];
    (lines || []).forEach(function (l) {
      if (l && l.role === 'user') texts.push(l.text);
    });
    return ackTexts(state, agent, texts);
  }

  /**
   * Same as ack(), for callers that already hold owner-turn bodies rather than
   * role-tagged lines (main's chatlog echo / history_meta / send-queue paths).
   * @returns {{ state: {items: Array}, acked: Array }}
   */
  function ackTexts(state, agent, texts) {
    const s = stateOf(state);
    const name = agentName(agent);
    if (!s.items.length || !name) return { state: s, acked: [] };
    const seen = (texts || []).map(key).filter(function (t) { return t.length > 0; });
    const used = {};
    const remaining = [];
    const acked = [];
    for (let i = 0; i < s.items.length; i++) {
      const it = s.items[i];
      if (it.agent !== name) {
        remaining.push(it);
        continue;
      }
      let found = false;
      for (let h = 0; h < seen.length; h++) {
        if (used[h] || seen[h] !== it.text) continue;
        used[h] = true;
        found = true;
        break;
      }
      if (found) acked.push(it);
      else remaining.push(it);
    }
    return { state: { items: remaining }, acked: acked };
  }

  /**
   * Re-apply `agent`'s still-unacked owner turns onto a line set. This is what
   * makes a wholesale history replace non-destructive: the server's sealed
   * turns win, and anything the owner sent that the server has not sealed yet
   * is appended back rather than silently dropped.
   *
   * Lines are shallow-copied whole (🎯T308: hand-rolled {role, text} copies are
   * what dropped turn timestamps before the renderer ever saw them — never
   * narrow a line to the fields this module happens to care about).
   * @returns {Array} copy of lines with unacked owner turns appended
   */
  function apply(lines, state, agent) {
    const s = stateOf(state);
    const name = agentName(agent);
    const out = (lines || []).map(function (l) {
      return l ? Object.assign({}, l) : l;
    });
    if (!name || !s.items.length) return out;
    // Count sealed owner lines so a turn already present is not doubled.
    const present = {};
    out.forEach(function (l) {
      if (l && l.role === 'user') {
        const k = key(l.text);
        present[k] = (present[k] || 0) + 1;
      }
    });
    for (let i = 0; i < s.items.length; i++) {
      const it = s.items[i];
      if (it.agent !== name) continue;
      if (present[it.text]) {
        present[it.text] -= 1;
        continue;
      }
      const line = { role: 'user', text: it.text, when: it.when };
      line[PENDING_FLAG] = true;
      // A failed send keeps its bubble, marked — never a vanish (🎯T275 loud).
      if (it.failed) line[FAILED_FLAG] = true;
      out.push(line);
    }
    return out;
  }

  /** Mark a staged turn failed. It stays pending, and stays visible. */
  function markFailed(state, id) {
    const s = stateOf(state);
    const want = String(id == null ? '' : id);
    if (!want) return s;
    return {
      items: s.items.map(function (it) {
        return it.id === want
          ? makeItem(it.agent, it.text, it.when, it.id, true)
          : it;
      }),
    };
  }

  /** Unacked turns for `agent` (product: retry / diagnostics). */
  function forAgent(state, agent) {
    const name = agentName(agent);
    return stateOf(state).items.filter(function (it) { return it.agent === name; });
  }

  /** Unacked bodies for `agent` — main re-enqueues these after a reload. */
  function unackedTexts(state, agent) {
    return forAgent(state, agent).map(function (it) { return it.text; });
  }

  /** True when `texts` already contains `text` (merge / no-dupe helper). */
  function hasText(texts, text) {
    const t = key(text);
    if (!t) return false;
    const list = texts || [];
    for (let i = 0; i < list.length; i++) {
      if (key(list[i]) === t) return true;
    }
    return false;
  }

  // ── Serialization (storage-agnostic; see EC-5 note at top) ─────────

  function serialize(state) {
    const items = stateOf(state).items.map(function (it) {
      return {
        id: String(it && it.id != null ? it.id : ''),
        agent: agentName(it && it.agent) || MAIN_AGENT,
        text: normalizeText(it && it.text),
        when: typeof (it && it.when) === 'number' ? it.when : 0,
        failed: !!(it && it.failed),
      };
    }).filter(function (it) {
      return it.id.length > 0 && it.text.trim().length > 0;
    });
    return JSON.stringify({ items: items });
  }

  /**
   * Parse a serialized pending set. Fails loud on unreadable input (never a
   * silent reset), and migrates the pre-🎯T372 main shape
   * ({id, text, stagedAt}, no agent) onto the agent-keyed model.
   * @returns {{ok: boolean, state: {items: Array}, present: boolean, error?: string}}
   */
  function deserialize(raw) {
    if (raw == null || raw === '') {
      return { ok: true, state: empty(), present: false };
    }
    let parsed;
    try {
      parsed = typeof raw === 'string' ? JSON.parse(raw) : raw;
    } catch (e) {
      return {
        ok: false,
        state: empty(),
        present: true,
        error: 'pending: parse failed — ' + (e && e.message ? e.message : String(e)),
      };
    }
    if (!parsed || typeof parsed !== 'object') {
      return { ok: false, state: empty(), present: true, error: 'pending: not an object' };
    }
    const items = [];
    if (Array.isArray(parsed.items)) {
      for (let i = 0; i < parsed.items.length; i++) {
        const it = parsed.items[i];
        if (!it || it.id == null || it.id === '') continue;
        const text = normalizeText(it.text);
        if (!text.trim()) continue;
        // Legacy main items predate both fields: they are 'jevons' turns
        // stamped with stagedAt.
        const when = typeof it.when === 'number'
          ? it.when
          : (typeof it.stagedAt === 'number' ? it.stagedAt : 0);
        items.push(makeItem(
          agentName(it.agent) || MAIN_AGENT,
          text,
          when,
          String(it.id),
          !!it.failed,
        ));
      }
    }
    return { ok: true, state: { items: items }, present: true };
  }

  return {
    MAIN_AGENT: MAIN_AGENT,
    PENDING_FLAG: PENDING_FLAG,
    FAILED_FLAG: FAILED_FLAG,
    empty: empty,
    key: key,
    stage: stage,
    ack: ack,
    ackTexts: ackTexts,
    apply: apply,
    markFailed: markFailed,
    forAgent: forAgent,
    unackedTexts: unackedTexts,
    hasText: hasText,
    serialize: serialize,
    deserialize: deserialize,
  };
}));
