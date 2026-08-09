// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// RHS agent/aside transcript inspect policy (🎯T124 / 🎯T205). DOM-free pure
// helpers for hermetic tests: selection transitions, auto-select on new aside,
// pane model, shared .msg body paint + scroll stickiness. Main chat is never
// the sink for fleet monologue.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.AgentTranscript = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Overseer root uses main chat — never open RHS inspect for it (T124 residual).
  function isOverseer(name, purpose) {
    const n = String(name || '').toLowerCase();
    if (n === 'jevons') return true;
    const p = String(purpose || '').toLowerCase();
    return p === 'overseer';
  }

  // True when a fleet row click should open the transcript pane.
  function shouldOpenTranscript(name, purpose) {
    if (!name) return false;
    return !isOverseer(name, purpose);
  }

  // nextSelection(prevName, clickName, opts) — toggle off if same name.
  // opts.purpose optional; overseer clicks never select for inspect.
  function nextSelection(prevName, clickName, opts) {
    if (!clickName) return null;
    const purpose = opts && opts.purpose;
    if (!shouldOpenTranscript(clickName, purpose)) {
      // Clear selection if re-clicking overseer or any overseer click.
      return null;
    }
    if (prevName && prevName === clickName) return null;
    return String(clickName);
  }

  // detectNewAsides(prevList, nextList) → names of new purpose=aside agents
  // (order: stable name-sort of newly appeared asides).
  function detectNewAsides(prevList, nextList) {
    const prev = {};
    (prevList || []).forEach(function (a) {
      if (a && a.name) prev[a.name] = true;
    });
    const out = [];
    (nextList || []).forEach(function (a) {
      if (!a || !a.name) return;
      if (prev[a.name]) return;
      const p = String(a.purpose || '').toLowerCase();
      if (p === 'aside') out.push(a.name);
    });
    out.sort();
    return out;
  }

  // pickAutoSelect(prevList, nextList, currentSelection) → name|null
  // Prefer the last newly appeared aside (name-sorted: last = z-order); if
  // none, keep currentSelection when still present. Never auto-select overseer.
  function pickAutoSelect(prevList, nextList, currentSelection) {
    const news = detectNewAsides(prevList, nextList).filter(function (n) {
      return shouldOpenTranscript(n, 'aside');
    });
    if (news.length) return news[news.length - 1];
    if (currentSelection) {
      const row = (nextList || []).find(function (a) {
        return a && a.name === currentSelection;
      });
      if (row && shouldOpenTranscript(row.name, row.purpose)) return currentSelection;
    }
    return null;
  }

  // ── 🎯T252: auto-activate attention asides; sticky draft; next after send ──
  //
  // When an aside needs the owner's turn (assistant just finished / needs-owner),
  // and the sidebar composer draft is empty, auto-select that aside in the
  // Transcript view. Non-empty draft holds focus (no mid-compose steal). After
  // send or clear draft, pick the next attention aside if any. Residual: empty
  // attention queue never forces a switch.

  function isAsidePurpose(purpose) {
    const p = String(purpose == null ? '' : purpose).trim().toLowerCase();
    return p === 'aside' || p === 'side' || p === 'side-chat' || p === 'file-target';
  }

  function isAsideAgent(agent) {
    if (!agent) return false;
    if (typeof agent === 'string') return isAsidePurpose(agent);
    return isAsidePurpose(agent.purpose || agent.role);
  }

  /** True when sidebar draft has no real text (whitespace-only = empty). */
  function sidebarDraftIsEmpty(draft) {
    return !String(draft == null ? '' : draft).trim();
  }

  /**
   * Last user|assistant role in inspect lines (skips status/other).
   * Assistant last ⇒ owner's turn (needs attention).
   */
  function lastConversationalRole(lines) {
    const arr = lines || [];
    for (let i = arr.length - 1; i >= 0; i--) {
      const r = arr[i] && arr[i].role;
      if (r === 'user' || r === 'assistant') return r;
    }
    return '';
  }

  /**
   * Aside requires owner attention when tagged needs-owner / needsAttention, or
   * when the last conversational role is assistant (user's turn).
   * Work agents and overseer never qualify.
   *
   * @param {object} agent fleet row {name, purpose, needs_owner?, …}
   * @param {{ lastRole?: string, lines?: Array, needsOwner?: boolean, purpose?: string }} [opts]
   */
  function asideRequiresAttention(agent, opts) {
    opts = opts || {};
    if (!agent || !agent.name) return false;
    const purpose = agent.purpose || agent.role || opts.purpose;
    if (!isAsidePurpose(purpose) && !isAsideAgent(opts.purpose)) return false;
    if (isOverseer(agent.name, purpose)) return false;
    if (opts.needsOwner === true) return true;
    if (agent.needs_owner || agent.needsOwner || agent.needsAttention || agent.attention === true) {
      return true;
    }
    const last = opts.lastRole != null
      ? opts.lastRole
      : lastConversationalRole(opts.lines);
    return last === 'assistant';
  }

  /** Turn-busy phase heuristic for fleet rows (working progress). */
  function isBusyPhase(agent) {
    if (!agent) return false;
    if (typeof agent.busy === 'boolean') return agent.busy;
    const phase = String(agent.phase || '').toLowerCase();
    if (phase === 'working') return true;
    if (phase === 'idle' || phase === 'parked') return false;
    const prog = String(agent.progress || agent.summary || '').toLowerCase().trim();
    if (!prog) return false;
    if (prog === 'working' || prog.indexOf('working') === 0) return true;
    return false;
  }

  /**
   * Names of asides that newly need attention between two fleet snapshots.
   * busy→idle on purpose=aside, or explicit needs_owner flag rising.
   */
  function detectNewAttentionAsides(prevList, nextList) {
    const prevBy = {};
    (prevList || []).forEach(function (a) {
      if (a && a.name) prevBy[a.name] = a;
    });
    const out = [];
    (nextList || []).forEach(function (a) {
      if (!a || !a.name || !isAsideAgent(a)) return;
      if (isOverseer(a.name, a.purpose)) return;
      const prev = prevBy[a.name];
      const nowFlag = !!(a.needs_owner || a.needsOwner || a.needsAttention || a.attention === true);
      const wasFlag = !!(prev && (prev.needs_owner || prev.needsOwner || prev.needsAttention || prev.attention === true));
      if (nowFlag && !wasFlag) {
        out.push(a.name);
        return;
      }
      if (prev && isBusyPhase(prev) && !isBusyPhase(a)) {
        out.push(a.name);
      }
    });
    return out;
  }

  /** Immutable enqueue (dedupe, append). */
  function enqueueAttention(queue, name) {
    const q = (queue || []).slice();
    const n = String(name || '');
    if (!n) return q;
    if (q.indexOf(n) >= 0) return q;
    q.push(n);
    return q;
  }

  /** Immutable dequeue by name. */
  function dequeueAttention(queue, name) {
    const n = String(name || '');
    return (queue || []).filter(function (x) { return x !== n; });
  }

  /** Drop queue entries not present in the live agent name set. */
  function pruneAttentionQueue(queue, agents) {
    const live = {};
    (agents || []).forEach(function (a) {
      if (a && a.name) live[a.name] = true;
    });
    return (queue || []).filter(function (n) { return live[n]; });
  }

  /**
   * pickAttentionAsideSelection(opts) → next selected name | current
   *
   * opts.attentionNames — ordered queue of asides needing owner
   * opts.currentSelection — current selected agent (may be null)
   * opts.draftEmpty — boolean; or opts.draft string
   * opts.reason — 'new-attention' | 'after-send' | 'draft-cleared' | 'poll'
   * opts.newName — preferred when reason=new-attention
   *
   * Residual: empty attention list → keep current (no forced switch).
   * Sticky: non-empty draft → keep current (never auto-steal mid-compose).
   * Sticky (🎯T371): reason='after-send' NEVER switches away from a current
   * selection. The owner just sent a turn to that aside and must watch their
   * own bubble land; 🎯T252's "advance to the next attention aside" stole the
   * pane on the very send that produced the message, so the bubble was painted
   * into a pane already rebound to another agent and vanished on its next
   * history frame. Advancing still happens — on poll / new-attention, once the
   * owner is no longer looking at the turn they just sent.
   */
  function pickAttentionAsideSelection(opts) {
    opts = opts || {};
    const names = [];
    (opts.attentionNames || []).forEach(function (n) {
      if (!n) return;
      if (!shouldOpenTranscript(n, 'aside')) return;
      if (names.indexOf(n) < 0) names.push(String(n));
    });
    const cur = opts.currentSelection || null;

    let empty;
    if (typeof opts.draftEmpty === 'boolean') {
      empty = opts.draftEmpty;
    } else if (opts.draft != null) {
      empty = sidebarDraftIsEmpty(opts.draft);
    } else {
      empty = true;
    }

    if (!names.length) return cur;
    if (!empty) return cur;

    const reason = opts.reason || 'poll';

    if (reason === 'after-send' || reason === 'draft-cleared') {
      // 🎯T371: dequeue-and-stay. A send is not permission to move the pane
      // off the conversation the owner is having; only an empty selection is
      // filled from the queue here.
      if (cur) return cur;
      return names[0];
    }

    if (reason === 'new-attention') {
      const neu = opts.newName ? String(opts.newName) : '';
      if (neu && names.indexOf(neu) >= 0) return neu;
      return names[names.length - 1];
    }

    // poll: stay if current still needs attention; else first in queue
    if (cur && names.indexOf(cur) >= 0) return cur;
    return names[0];
  }

  /**
   * Live inspect assistant frame with terminal stop_reason ⇒ owner attention.
   * Pure — used when multiplex is subscribed to an aside.
   */
  function liveFrameSignalsOwnerAttention(event) {
    if (!event || event.type !== 'assistant') return false;
    const sr = inspectEventStopReason(event);
    return !!(sr && INSPECT_TERMINAL_STOPS[sr]);
  }

  /**
   * 🎯T308: normalize a wire/HTTP turn timestamp to epoch ms, or undefined
   * when the turn carries none. Accepts ms, seconds (10-digit), and ISO
   * strings — the sidebar must never invent "now" for a sealed turn.
   * @param {*} v
   * @returns {number|undefined}
   */
  function normalizeWhen(v) {
    if (v == null || v === '') return undefined;
    if (typeof v === 'number') {
      if (!isFinite(v) || v <= 0) return undefined;
      return v < 1e11 ? Math.round(v * 1000) : Math.round(v);
    }
    const s = String(v).trim();
    if (!s) return undefined;
    if (/^\d+$/.test(s)) return normalizeWhen(Number(s));
    const ms = Date.parse(s);
    return isFinite(ms) ? ms : undefined;
  }

  /** First defined turn timestamp under any of the wire spellings (🎯T308). */
  function turnWhen(t) {
    if (!t) return undefined;
    const keys = ['when', 'ts', 'timestamp', 'time', 'created_at'];
    for (let i = 0; i < keys.length; i++) {
      const w = normalizeWhen(t[keys[i]]);
      if (w !== undefined) return w;
    }
    return undefined;
  }

  /**
   * 🎯T308: copy inspect lines without dropping `when`. Every rebuild of the
   * inspect line model goes through here — the old hand-rolled
   * `{role, text}` copies are what stripped timestamps before the renderer
   * ever saw them, leaving the sidebar with no .msg-time to paint.
   * @param {Array<object>} lines
   * @returns {Array<object>}
   */
  function copyInspectLines(lines) {
    return (lines || []).map(function (l) {
      if (!l) return l;
      const out = { role: l.role, text: l.text };
      if (l.when !== undefined) out.when = l.when;
      return out;
    });
  }

  // turnsToLines(turns) → [{role, text, when?}] for pane render (filters empty).
  // 🎯T308: `when` survives from wire/HTTP so inspect bubbles get main's
  // .msg-time chrome from the same constructor.
  function turnsToLines(turns) {
    const out = [];
    (turns || []).forEach(function (t) {
      if (!t) return;
      const role = t.role === 'user' ? 'user' : (t.role === 'assistant' ? 'assistant' : (t.role || 'other'));
      const text = t.text == null ? '' : String(t.text);
      if (!text.trim() && role === 'other') return;
      const line = { role: role, text: text };
      const when = turnWhen(t);
      if (when !== undefined) line.when = when;
      out.push(line);
    });
    return out;
  }

  // paneModel(name, payload) → { title, empty, lines, error }
  function paneModel(name, payload, err) {
    if (err) {
      return {
        title: name || '',
        empty: true,
        error: String(err.message || err),
        lines: [],
      };
    }
    const lines = turnsToLines(payload && payload.turns);
    return {
      title: (payload && payload.name) || name || '',
      empty: lines.length === 0,
      error: '',
      lines: lines,
      sessionId: (payload && payload.session_id) || '',
    };
  }

  function escapeHtml(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  // Map inspect turn roles → main chat .msg role classes (🎯T205).
  function inspectToMsgRole(role) {
    if (role === 'user') return 'user';
    if (role === 'assistant') return 'jevons';
    return 'status';
  }

  // 🎯T221: unwrap fleet inject wrappers for inspect display.
  // Owner repro: role=user text often arrives as <user_query>…</user_query>
  // (event push / overseer inject). Returns { text, wasWrapped }.
  function unwrapInspectUserText(text) {
    let t = text == null ? '' : String(text);
    let wasWrapped = false;
    // Repeated unwrap for nested identical wrappers (defensive).
    for (let i = 0; i < 3; i++) {
      const m = t.match(
        /^\s*<user_query(?:\s[^>]*)?>\s*([\s\S]*?)\s*<\/user_query>\s*$/i,
      );
      if (!m) break;
      t = m[1];
      wasWrapped = true;
    }
    return { text: t, wasWrapped: wasWrapped };
  }

  // Escape raw HTML so marked cannot emit live tags (XSS), while MD syntax
  // (**bold**, lists, fences) still parses. Inspect-user path only.
  function escapeHtmlForInspectMarkdown(text) {
    return String(text == null ? '' : text)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;');
  }

  // True when inspect user body should use the marked path (not plain/renderUserText).
  // Fleet injects (wasWrapped) always; MD-shaped text (bold/lists/fences/headings).
  // Residual: pure system-reminder walls without MD markers stay plain.
  // 🎯T233: harness inject walls are handled as nuggets before this path.
  function inspectUserShouldMarkdown(text, wasWrapped) {
    if (wasWrapped) return true;
    const s = String(text == null ? '' : text);
    if (!s) return false;
    if (/\*\*[^*\n]+\*\*/.test(s)) return true;
    if (/__[^_\n]+__/.test(s)) return true;
    if (/^#{1,6}\s+\S/m.test(s)) return true;
    if (/```/.test(s)) return true;
    if (/^\s*[-*+]\s+\S/m.test(s)) return true;
    if (/^\s*\d+\.\s+\S/m.test(s)) return true;
    return false;
  }

  // 🎯T233: extract inner body of a <system-reminder>…</system-reminder> wall.
  // Falls back to the original text when tags are partial/absent.
  function extractSystemReminderBody(text) {
    const s = String(text == null ? '' : text);
    const m = s.match(
      /<system-reminder(?:\s[^>]*)?>\s*([\s\S]*?)\s*<\/system-reminder>/i,
    );
    if (m) return m[1];
    return s.replace(/<\/?system-reminder(?:\s[^>]*)?>/gi, '').trim() || s;
  }

  // 🎯T233: classify harness / fleet injects that must not paint as full user bubbles.
  // Owner prose (including MD-shaped <user_query> design pins) stays kind=owner.
  // Returns:
  //   { kind:'inject', injectKind, label, detail }
  //   { kind:'owner', detail, wasWrapped }
  function classifyInspectUserLine(text) {
    const raw = text == null ? '' : String(text);
    const unwrapped = unwrapInspectUserText(raw);
    const display = unwrapped.text;
    const trimmed = String(display).replace(/^\s+/, '');

    // system-reminder tags on raw or unwrapped (harness walls).
    if (
      /<system-reminder[\s>]/i.test(raw) ||
      /<\/system-reminder>/i.test(raw) ||
      /<system-reminder[\s>]/i.test(display)
    ) {
      return {
        kind: 'inject',
        injectKind: 'system-reminder',
        label: '⋯ system',
        detail: extractSystemReminderBody(display),
      };
    }
    // Fleet standing brief (first-send inject; may include PO brief after).
    if (
      trimmed.indexOf('[Jevons fleet standing brief') === 0 ||
      /Jevons fleet standing brief/.test(display)
    ) {
      return {
        kind: 'inject',
        injectKind: 'standing-brief',
        label: '⋯ brief',
        detail: display,
      };
    }
    // Event push wrappers: [event: worker-finished] …
    if (/^\[event:\s*[^\]]+\]/i.test(trimmed)) {
      const em = trimmed.match(/^\[event:\s*([^\]]+)\]/i);
      const src = em ? String(em[1]).trim() : 'event';
      return {
        kind: 'inject',
        injectKind: 'event',
        label: '⋯ ' + (src || 'event'),
        detail: display,
      };
    }
    if (trimmed.indexOf('[Daemon restart') === 0) {
      return {
        kind: 'inject',
        injectKind: 'daemon',
        label: '⋯ system',
        detail: display,
      };
    }
    // Residual: pure owner prose (and MD-shaped user_query design pins).
    return {
      kind: 'owner',
      detail: display,
      wasWrapped: unwrapped.wasWrapped,
    };
  }

  // 🎯T233: compacted activity nugget HTML — same family as main-chat ⋯ n steps
  // (.turn-marker + .turn-tip hover). Detail is escaped plain text in the tip.
  function paintInjectNuggetHTML(label, detail, injectKind) {
    const kind = injectKind || 'system';
    const lab = label || '⋯ system';
    const tip = detail == null ? '' : String(detail);
    return (
      '<div class="turn-marker inject-nugget" data-inject="' +
      escapeHtml(kind) +
      '">' +
      '<span class="inject-label">' +
      escapeHtml(lab) +
      '</span>' +
      '<div class="turn-tip">' +
      '<div class="turn-item inject-detail">' +
      escapeHtml(tip) +
      '</div>' +
      '</div>' +
      '</div>'
    );
  }

  // 🎯T205/T221/T233: body paint policy for one inspect turn.
  // Assistant → HTML via parseAssistantMarkdown (sealed main-chat path).
  // User → 🎯T233: harness injects → mode=nugget (compact ⋯ chrome + hover);
  //         else 🎯T221 MD HTML for fleet injects / MD-shaped text (inspect-only);
  //         else renderUserText (quotes/images) or plain. Main chat paintBody
  //         is unchanged.
  // Other → plain text. msgRole is the .msg class role for shared chrome.
  // deps: { parseAssistantMarkdown?, renderUserText? }
  // mode: 'html' | 'text' | 'nugget' (nugget content is full outer HTML).
  function paintInspectLineBody(role, text, deps) {
    deps = deps || {};
    const t = text == null ? '' : String(text);
    const msgRole = inspectToMsgRole(role);
    if (role === 'assistant') {
      const parse = deps.parseAssistantMarkdown;
      if (typeof parse === 'function') {
        return { mode: 'html', content: parse(t), msgRole: msgRole };
      }
      return { mode: 'text', content: t, msgRole: msgRole };
    }
    if (role === 'user') {
      const cls = classifyInspectUserLine(t);
      if (cls.kind === 'inject') {
        return {
          mode: 'nugget',
          content: paintInjectNuggetHTML(cls.label, cls.detail, cls.injectKind),
          msgRole: 'status',
          injectKind: cls.injectKind,
          label: cls.label,
          detail: cls.detail,
        };
      }
      const display = cls.detail;
      const parse = deps.parseAssistantMarkdown;
      if (
        typeof parse === 'function' &&
        inspectUserShouldMarkdown(display, cls.wasWrapped)
      ) {
        // Escape raw HTML first so injects cannot XSS via <script> etc.
        return {
          mode: 'html',
          content: parse(escapeHtmlForInspectMarkdown(display)),
          msgRole: msgRole,
        };
      }
      const renderUser = deps.renderUserText;
      if (typeof renderUser === 'function') {
        return { mode: 'html', content: renderUser(display), msgRole: msgRole };
      }
      return { mode: 'text', content: display, msgRole: msgRole };
    }
    return { mode: 'text', content: t, msgRole: msgRole };
  }

  /**
   * 🎯T308: the .msg-time chrome every bubble gets when its turn has a
   * timestamp — mirrors index.html buildMsg (data-ts + relative label +
   * absolute hover title, 🎯T91). Returns '' when the turn has no `when`,
   * so sealed turns never display a fabricated "now".
   * deps.relTime / deps.absTimeTitle mirror the index.html helpers.
   */
  function msgTimeHTML(when, deps) {
    deps = deps || {};
    const ms = normalizeWhen(when);
    if (ms === undefined) return '';
    const rel = typeof deps.relTime === 'function' ? deps.relTime(ms) : String(ms);
    const abs = typeof deps.absTimeTitle === 'function' ? deps.absTimeTitle(ms) : String(ms);
    return '<div class="msg-time" data-ts="' + escapeHtml(String(ms))
      + '" title="' + escapeHtml(abs) + '">' + escapeHtml(rel) + '</div>';
  }

  /**
   * 🎯T308: the whole per-turn policy of the inspect pane, as a pure value.
   * renderAgentInspect decides nothing about a turn beyond what this returns
   * — it is a host that appends a nugget or feeds a bubble spec to buildMsg,
   * the one constructor main #messages also uses.
   *
   * Returns either
   *   {kind:'nugget', html}                          🎯T233 compact ⋯ inject
   *   {kind:'bubble', role, text, when, painted}     → buildMsg(role, text, when)
   *
   * `painted` is computed for user turns only: assistant turns must take the
   * live paintBody path (mermaid / hljs / link decoration), which no pure
   * string function can reproduce (🎯T217). `when` is undefined for a turn
   * that carries no timestamp — the caller must then omit .msg-time rather
   * than stamp "now" on a sealed turn.
   * deps: { parseAssistantMarkdown?, renderUserText? }
   */
  function inspectBubbleSpec(line, deps) {
    const role = (line && line.role) || 'other';
    const text = line && line.text != null ? String(line.text) : '';
    let painted = null;
    if (role === 'user') {
      try {
        painted = paintInspectLineBody('user', text, deps || {});
      } catch (_) {
        painted = null;
      }
    }
    if (painted && painted.mode === 'nugget' && painted.content) {
      return { kind: 'nugget', html: painted.content };
    }
    return {
      kind: 'bubble',
      role: (painted && painted.msgRole) || inspectToMsgRole(role),
      text: text,
      when: normalizeWhen(line && line.when),
      painted: painted,
    };
  }

  // Hermetic HTML fixture for #agent-inspect-body: main .msg bubble chrome (🎯T205)
  // plus 🎯T233 inject nuggets (not full user bubbles).
  // deps.parseAssistantMarkdown / deps.renderUserText mirror index.html paths.
  // 🎯T308: the fixture takes its role / nugget / timestamp decisions from
  // inspectBubbleSpec — the same policy the product renderer reads — and emits
  // buildMsg's chrome, .msg-time included. A fixture that renders chrome the
  // product lacks is how "shared paint" claims passed while dual construction
  // survived; it stays a fixture, never a product renderer.
  function paintInspectLinesHTML(lines, deps) {
    deps = deps || {};
    let html = '';
    (lines || []).forEach(function (line) {
      if (!line) return;
      const spec = inspectBubbleSpec(line, deps);
      if (spec.kind === 'nugget') {
        // Full outer chrome already (turn-marker); no .msg.user wrapper.
        html += spec.html;
        return;
      }
      const body = spec.painted || paintInspectLineBody(line.role || 'other', spec.text, deps);
      const bodyInner = body.mode === 'html'
        ? body.content
        : escapeHtml(body.content);
      html += '<div class="msg ' + escapeHtml(spec.role) + '">'
        + '<div class="msg-body">' + bodyInner + '</div>'
        + msgTimeHTML(spec.when, deps)
        + '</div>';
    });
    return html;
  }

  // Stable fingerprint for poll no-op (skip full replace when content unchanged).
  function linesFingerprint(lines) {
    let s = '';
    (lines || []).forEach(function (l, i) {
      if (!l) return;
      s += i + '\0' + (l.role || '') + '\0' + (l.text == null ? '' : String(l.text)) + '\n';
    });
    return s;
  }

  // 🎯T205: latched stick-to-bottom policy (Track | Free) — pure, reusable for
  // #agent-inspect-body. Mirrors main #messages: free-scroll is never yanked
  // to bottom on content growth; near-bottom / Track keeps following.
  //
  // Mode is latched, not re-derived from distance every frame:
  //   track — pin scrollTop to scrollHeight after updates
  //   free  — preserve scrollTop; growth never pins
  // Enter: boot / explicit enterTrack / arrive at bottom (ε entry only).
  // Leave: intentional scroll up (wheel / scroll metrics).
  function createScrollFollow(opts) {
    opts = opts || {};
    const eps = opts.eps != null ? Number(opts.eps) : 16;
    let mode = 'track'; // 'track' | 'free'
    let mayEnterFromGeometry = true;
    let lastScrollTop = 0;
    let lastScrollHeight = 0;
    let bookkeeping = 0;

    function distFromBottom(el) {
      if (!el) return 0;
      return el.scrollHeight - el.clientHeight - el.scrollTop;
    }
    function atBottom(el) {
      if (!el) return true;
      const room = el.scrollHeight - el.clientHeight;
      if (room <= 0) return true;
      return distFromBottom(el) <= eps;
    }
    function isTracking() { return mode === 'track'; }
    function getMode() { return mode; }
    function setMode(m) { mode = (m === 'free') ? 'free' : 'track'; }
    function enterTrack() {
      mode = 'track';
      mayEnterFromGeometry = true;
    }
    function leaveTrack(el) {
      mode = 'free';
      mayEnterFromGeometry = el ? distFromBottom(el) > eps : false;
    }
    function noteAwayFromBottom(el) {
      if (el && distFromBottom(el) > eps) mayEnterFromGeometry = true;
    }
    function tryEnterFromGeometry(el) {
      noteAwayFromBottom(el);
      if (mayEnterFromGeometry && atBottom(el)) enterTrack();
    }
    function noteMetrics(el) {
      if (!el) return;
      lastScrollTop = el.scrollTop;
      lastScrollHeight = el.scrollHeight;
    }
    function beginBookkeeping() { bookkeeping++; }
    function endBookkeeping() { bookkeeping = Math.max(0, bookkeeping - 1); }
    function onWheel(deltaY, el) {
      if (deltaY < 0) leaveTrack(el);
      else if (deltaY > 0) tryEnterFromGeometry(el);
    }
    function onScroll(el) {
      if (!el) return;
      if (bookkeeping > 0) {
        noteMetrics(el);
        return;
      }
      const top = el.scrollTop;
      const h = el.scrollHeight;
      if (top + 1 < lastScrollTop && h + 1 >= lastScrollHeight) {
        leaveTrack(el);
      } else {
        tryEnterFromGeometry(el);
      }
      noteMetrics(el);
    }
    function shouldPin() { return mode === 'track'; }
    // After content mutation: pin if tracking; else restore prevTop (free read).
    function applyAfterUpdate(el, prevTop) {
      if (!el) return;
      beginBookkeeping();
      try {
        if (mode === 'track') {
          el.scrollTop = el.scrollHeight;
        } else if (typeof prevTop === 'number' && isFinite(prevTop)) {
          el.scrollTop = prevTop;
        }
        noteMetrics(el);
      } finally {
        endBookkeeping();
      }
    }
    // Pure policy (no DOM): next scrollTop after update given metrics.
    function nextScrollTop(args) {
      args = args || {};
      const scrollHeight = Number(args.scrollHeight) || 0;
      const prevTop = args.prevTop;
      if (mode === 'track') return scrollHeight;
      if (typeof prevTop === 'number' && isFinite(prevTop)) return prevTop;
      return 0;
    }

    return {
      eps: eps,
      isTracking: isTracking,
      getMode: getMode,
      setMode: setMode,
      enterTrack: enterTrack,
      leaveTrack: leaveTrack,
      tryEnterFromGeometry: tryEnterFromGeometry,
      atBottom: atBottom,
      distFromBottom: distFromBottom,
      onWheel: onWheel,
      onScroll: onScroll,
      shouldPin: shouldPin,
      applyAfterUpdate: applyAfterUpdate,
      nextScrollTop: nextScrollTop,
      noteMetrics: noteMetrics,
      beginBookkeeping: beginBookkeeping,
      endBookkeeping: endBookkeeping,
    };
  }

  // mainChatMustNotContainFleetTraffic — oracle marker: product rule string.
  const MAIN_CHAT_IS_OWNER_OVERSEER_ONLY = true;

  // ── 🎯T209: inspect multiplex over /ws/chat (same wire class as main) ──

  /** Client → server control frame to start inspect history+live for name. */
  function inspectSubscribeFrame(name) {
    return JSON.stringify({ type: 'inspect_subscribe', name: String(name || '') });
  }

  /** Client → server control frame to stop inspect multiplex (name optional). */
  function inspectUnsubscribeFrame(name) {
    const o = { type: 'inspect_unsubscribe' };
    if (name) o.name = String(name);
    return JSON.stringify(o);
  }

  /** True when a WS frame is the agent inspect multiplex envelope. */
  function isAgentTranscriptFrame(m) {
    return !!(m && m.type === 'agent_transcript');
  }

  const INSPECT_TERMINAL_STOPS = { end_turn: 1, stop_sequence: 1, max_tokens: 1 };

  function inspectEventStopReason(ev) {
    const msg = ev && ev.message;
    if (!msg) return '';
    return msg.stop_reason || msg.stopReason || '';
  }

  /**
   * Owner-echo equality key for inspect user lines (🎯T281).
   * Unwraps <user_query> fleet inject wrappers so optimistic plain text
   * matches the WS echo when ACP wraps the same body.
   */
  function inspectUserDedupeKey(text) {
    const raw = text == null ? '' : String(text);
    if (!raw) return '';
    const unwrapped = unwrapInspectUserText(raw);
    return String(unwrapped.text != null ? unwrapped.text : raw).trim();
  }

  /**
   * True when a new inspect user frame is the same owner submit as the
   * last painted user line (optimistic + WS double-append guard, 🎯T281).
   * Consecutive only — intentional resend after an assistant turn paints again.
   */
  function isDuplicateInspectUserLine(lastLine, nextUserText) {
    if (!lastLine || lastLine.role !== 'user') return false;
    const a = inspectUserDedupeKey(lastLine.text);
    const b = inspectUserDedupeKey(nextUserText);
    return !!(a && b && a === b);
  }

  // 🎯T329: ONE coalesce model — ChatEvents.applyLiveDisplayFrame.
  // RHS inspect must not keep a second adjacency/_stream coalescer.
  function loadChatEvents() {
    if (typeof ChatEvents !== 'undefined') return ChatEvents;
    if (typeof module === 'object' && module.exports) {
      try { return require('./chat_events.js'); } catch (_) { return null; }
    }
    return null;
  }

  /**
   * Apply a progressive live event onto inspect lines.
   * Thin wrapper over ChatEvents.applyLiveDisplayFrame (🎯T329): stream_id
   * join, terminal-stop seal only, tool_use never seals, non-boundary user
   * injects (system-reminder / standing brief / …) never seal open assistants.
   * Pure — no DOM. 🎯T281 dedupe lives in the shared path.
   *
   * @param {Array<{role:string,text:string,_stream?:boolean}>|null} lines
   * @param {object|null} event
   * @param {{now?: number}} [opts]
   * @returns {Array<{role:string,text:string,_stream?:boolean}>}
   */
  function applyInspectLiveFrame(lines, event, opts) {
    const CE = loadChatEvents();
    if (CE && typeof CE.applyLiveDisplayFrame === 'function') {
      return CE.applyLiveDisplayFrame(lines, event, opts || {});
    }
    // Fail closed only if ChatEvents is missing (broken bundle) — never a
    // second product coalescer. Identity copy so callers do not crash.
    return (lines || []).map(function (l) {
      if (!l) return l;
      const c = { role: l.role, text: l.text };
      if (l.when !== undefined) c.when = l.when;
      if (l._stream) c._stream = true;
      if (l._streamId != null) c._streamId = l._streamId;
      return c;
    });
  }

  /**
   * paneModel from a wire agent_transcript history frame (kind=history).
   * Same shape as HTTP payload for renderAgentInspect.
   */
  function paneModelFromWire(frame, err) {
    if (err) return paneModel(frame && frame.name, null, err);
    if (!frame) return paneModel('', null, new Error('empty inspect frame'));
    return paneModel(frame.name, frame, frame.error && frame.empty ? null : (frame.error ? new Error(frame.error) : null));
  }

  // ── 🎯T251: sidebar Transcript composer (independent of main #input) ──

  /**
   * True when the RHS Transcript tab should show a dedicated message input + send.
   * Requires transcript tab active and a selectable fleet participant (not overseer).
   * @param {{ tab?: string, selectedAgent?: string|null, purpose?: string }} opts
   */
  function sidebarComposerVisible(opts) {
    const CW = loadConversationWidget();
    if (CW && typeof CW.composerVisible === 'function') {
      return CW.composerVisible({
        tab: opts && opts.tab,
        selectedAgent: opts && opts.selectedAgent,
        purpose: opts && opts.purpose,
        shouldOpen: shouldOpenTranscript,
      });
    }
    const o = opts || {};
    const tab = String(o.tab || '');
    if (tab !== 'transcript') return false;
    const name = o.selectedAgent == null ? '' : String(o.selectedAgent).trim();
    if (!name) return false;
    return shouldOpenTranscript(name, o.purpose);
  }

  /**
   * True when sidebar draft is empty/whitespace-only (🎯T252 attention focus).
   * @param {string} text
   */
  function isSidebarDraftEmpty(text) {
    const CW = loadConversationWidget();
    if (CW && typeof CW.isDraftEmpty === 'function') {
      return CW.isDraftEmpty(text);
    }
    return !String(text == null ? '' : text).trim();
  }

  /**
   * Product HTTP path for delivering to a named fleet agent/aside (same as T182).
   * @param {string} name
   */
  function agentSendPath(name) {
    const CW = loadConversationWidget();
    if (CW && typeof CW.agentSendPath === 'function') {
      return CW.agentSendPath(name);
    }
    const n = String(name || '').trim();
    if (!n) return '';
    return '/api/agents/' + encodeURIComponent(n) + '/send';
  }

  /**
   * 🎯T263: JSON body for POST /api/asides.
   * Freeform opening text is optional; when present the server starts and
   * delivers in the same request (not register-only empty transcript).
   * 🎯T270: optional kind (side|capture|target) for closed-history type.
   * @param {string} id
   * @param {string} title
   * @param {string} [text] opening owner prompt
   * @param {string} [kind] side | capture | target
   */
  function createAsideRequestBody(id, title, text, kind) {
    const body = {
      id: String(id || ''),
      title: String(title || 'aside'),
    };
    const opening = text == null ? '' : String(text).trim();
    if (opening) body.text = opening;
    const k = kind == null ? '' : String(kind).trim();
    if (k) body.kind = k;
    return body;
  }

  /**
   * 🎯T263: opts for ensureFleetAside on explicit prefix create.
   * Only freeform aside: passes opening text + expectDeliver.
   * capture:/target: stay register-only (no text).
   * 🎯T270: always includes kind for durable history type.
   * @param {string} command parsePrefix command
   * @param {string} openingBody
   */
  function freeformAsideCreateOpts(command, openingBody) {
    const cmd = String(command || '').toLowerCase();
    const kind = cmd === 'target' ? 'target' : (cmd === 'capture' ? 'capture' : 'side');
    if (cmd !== 'aside') {
      return { kind: kind, command: cmd };
    }
    const t = String(openingBody == null ? '' : openingBody).trim();
    if (!t) return { kind: kind, command: cmd };
    return { text: t, expectDeliver: true, kind: kind, command: cmd };
  }

  // 🎯T309.1: sidebar composer pure helpers collapse into ConversationWidget.
  // AgentTranscript keeps the T251/T265 export names as thin delegates so
  // existing hermetic tests and residual call sites keep working.
  function loadConversationWidget() {
    if (typeof ConversationWidget !== 'undefined') return ConversationWidget;
    if (typeof module === 'object' && module.exports) {
      try { return require('./conversation_widget.js'); } catch (_) { return null; }
    }
    return null;
  }

  /**
   * Build the sidebar send request for the selected transcript participant.
   * Returns { ok:true, url, method, body:{text}, name } or { ok:false, reason }.
   * Does not touch the main chat wire / overseer composer.
   * @param {string|null|undefined} selectedAgent
   * @param {string} text
   * @param {{ purpose?: string }} [opts]
   */
  function sidebarSendRequest(selectedAgent, text, opts) {
    const CW = loadConversationWidget();
    if (CW && typeof CW.buildSendRequest === 'function') {
      return CW.buildSendRequest(selectedAgent, text, {
        purpose: opts && opts.purpose,
        isOverseer: function (n, p) { return isOverseer(n, p); },
      });
    }
    const name = selectedAgent == null ? '' : String(selectedAgent).trim();
    if (!name) {
      return { ok: false, reason: 'no-selection' };
    }
    if (!shouldOpenTranscript(name, opts && opts.purpose)) {
      return { ok: false, reason: 'overseer-main-only' };
    }
    const body = String(text == null ? '' : text).trim();
    if (!body) {
      return { ok: false, reason: 'empty' };
    }
    const url = agentSendPath(name);
    if (!url) {
      return { ok: false, reason: 'no-selection' };
    }
    return {
      ok: true,
      name: name,
      url: url,
      method: 'POST',
      body: { text: body },
    };
  }

  /**
   * Owner-facing copy when sidebar send is blocked before HTTP (🎯T275).
   * Never silent — every reason maps to a loud string.
   * @param {string|null|undefined} reason
   */
  function sidebarSendBlockMessage(reason) {
    const CW = loadConversationWidget();
    if (CW && typeof CW.sendBlockMessage === 'function') {
      return CW.sendBlockMessage(reason);
    }
    const r = String(reason == null ? '' : reason).trim();
    if (r === 'no-selection') {
      return 'No fleet agent selected — pick a worker/PO in the RHS tree first.';
    }
    if (r === 'overseer-main-only') {
      return 'Overseer uses main chat, not the RHS Transcript composer.';
    }
    if (r === 'empty') {
      return 'Message is empty — type something before Send.';
    }
    if (r === 'observe-only') {
      return 'This thread is observe-only — cannot send (read-only residual).';
    }
    if (!r) {
      return 'Send blocked — unknown reason (not a silent drop).';
    }
    return 'Send blocked: ' + r;
  }

  /**
   * Classify Enter chords for the sidebar composer (🎯T309.1 → ConversationWidget
   * compact density). Enter → send; Shift+Enter → newline.
   * @returns {'send'|'newline'|null}
   */
  function classifySidebarComposerKey(e) {
    const CW = loadConversationWidget();
    if (CW && typeof CW.classifyComposerKey === 'function') {
      return CW.classifyComposerKey(e, { density: 'compact' });
    }
    if (!e || (e.key !== 'Enter' && e.code !== 'Enter')) return null;
    if (e.shiftKey) return 'newline';
    if (e.isComposing || e.keyCode === 229) return null;
    return 'send';
  }

  // ── 🎯T265: microcosm conversation surface (preserve working, send chrome) ──

  /**
   * Merge inspect pane model after wire overlay without dropping microcosm fields.
   * T205 residual: rebuilds that only copy title/lines/error drop `working`,
   * so in-flight chrome vanishes after the first live frame / wire merge.
   */
  function mergePaneModelWithLines(model, lines) {
    const m = model && typeof model === 'object' ? model : {};
    const nextLines = Array.isArray(lines) ? lines : [];
    return {
      title: m.title != null ? m.title : '',
      empty: nextLines.length === 0,
      error: m.error || '',
      lines: nextLines,
      sessionId: m.sessionId || m.session_id || '',
      working: !!m.working,
    };
  }

  /** Owner-facing working label for RHS inspect (not overseer "Jevons is working"). */
  function inspectWorkingLabel(agentName) {
    const n = String(agentName == null ? '' : agentName).trim();
    if (!n || isOverseer(n)) return 'Working';
    const shown = n.length > 28 ? n.slice(0, 25) + '…' : n;
    return shown + ' is working';
  }

  /**
   * After successful sidebar send: optimistic owner turn + open working chrome.
   * 🎯T309.1: delegates to ConversationWidget.afterSendOptimistic (same shape).
   */
  function afterSidebarSendOptimistic(lines, text, opts) {
    opts = opts || {};
    const CW = loadConversationWidget();
    if (CW && typeof CW.afterSendOptimistic === 'function') {
      return CW.afterSendOptimistic(lines, text, {
        title: opts.title,
        now: opts.now,
        // 🎯T281: unwrap-aware consecutive dedupe (same as live echo path).
        isDuplicate: isDuplicateInspectUserLine,
        normalizeWhen: normalizeWhen,
      });
    }
    const body = String(text == null ? '' : text).trim();
    const next = copyInspectLines(lines);
    if (body) {
      const last = next[next.length - 1];
      if (!isDuplicateInspectUserLine(last, body)) {
        const when = opts.now !== undefined ? normalizeWhen(opts.now) : Date.now();
        next.push({ role: 'user', text: body, when: when });
      }
    }
    return {
      lines: next,
      model: {
        title: opts.title != null ? opts.title : '',
        empty: next.length === 0,
        error: '',
        lines: next,
        working: true,
      },
    };
  }

  /**
   * Display text for inspect user line: strip raw [attention:]/[target-aside:] headers.
   */
  function inspectDisplayUserText(text, deps) {
    const raw = text == null ? '' : String(text);
    if (!raw) return '';
    deps = deps || {};
    if (typeof deps.parseAsideWireUserText === 'function') {
      const p = deps.parseAsideWireUserText(raw);
      if (p && (p.displayText || p.body || p.title)) {
        return String(p.displayText || p.body || p.title || '').trim() || raw;
      }
    }
    const stripped = raw.replace(
      /^\s*\[(?:attention|target-aside)\s*:[^\]]*\]\s*(?:\r?\n)?/i,
      '',
    );
    if (stripped !== raw) {
      const body = stripped.replace(/\n\n\(Ceremony:[\s\S]*$/i, '').trim();
      return body || raw;
    }
    return raw;
  }

  /**
   * Structural oracle: inspect pane is conversation-only (no nested fleet/frontier).
   */
  function inspectPaneIsConversationOnly(html) {
    const s = String(html || '');
    if (s.indexOf('agent-inspect-body') < 0) return false;
    if (s.indexOf('agent-inspect-composer') < 0) return false;
    const start = s.indexOf('id="agent-inspect"');
    if (start < 0) return false;
    const window = s.slice(start, start + 3500);
    if (/id="agents"/i.test(window) && window.indexOf('agent-inspect-body') > window.indexOf('id="agents"')) {
      return false;
    }
    if (/id="frontier-body"/i.test(window) || /id="frontier-table"/i.test(window)) {
      return false;
    }
    return true;
  }

  return {
    isOverseer: isOverseer,
    shouldOpenTranscript: shouldOpenTranscript,
    nextSelection: nextSelection,
    detectNewAsides: detectNewAsides,
    pickAutoSelect: pickAutoSelect,
    // 🎯T252 attention auto-activate + sticky draft
    isAsidePurpose: isAsidePurpose,
    isAsideAgent: isAsideAgent,
    sidebarDraftIsEmpty: sidebarDraftIsEmpty,
    lastConversationalRole: lastConversationalRole,
    asideRequiresAttention: asideRequiresAttention,
    isBusyPhase: isBusyPhase,
    detectNewAttentionAsides: detectNewAttentionAsides,
    enqueueAttention: enqueueAttention,
    dequeueAttention: dequeueAttention,
    pruneAttentionQueue: pruneAttentionQueue,
    pickAttentionAsideSelection: pickAttentionAsideSelection,
    liveFrameSignalsOwnerAttention: liveFrameSignalsOwnerAttention,
    turnsToLines: turnsToLines,
    // 🎯T308 one-widget line model: timestamps survive to the renderer
    normalizeWhen: normalizeWhen,
    turnWhen: turnWhen,
    copyInspectLines: copyInspectLines,
    msgTimeHTML: msgTimeHTML,
    paneModel: paneModel,
    escapeHtml: escapeHtml,
    inspectToMsgRole: inspectToMsgRole,
    unwrapInspectUserText: unwrapInspectUserText,
    escapeHtmlForInspectMarkdown: escapeHtmlForInspectMarkdown,
    inspectUserShouldMarkdown: inspectUserShouldMarkdown,
    extractSystemReminderBody: extractSystemReminderBody,
    classifyInspectUserLine: classifyInspectUserLine,
    paintInjectNuggetHTML: paintInjectNuggetHTML,
    inspectBubbleSpec: inspectBubbleSpec,
    paintInspectLineBody: paintInspectLineBody,
    paintInspectLinesHTML: paintInspectLinesHTML,
    linesFingerprint: linesFingerprint,
    createScrollFollow: createScrollFollow,
    MAIN_CHAT_IS_OWNER_OVERSEER_ONLY: MAIN_CHAT_IS_OWNER_OVERSEER_ONLY,
    inspectSubscribeFrame: inspectSubscribeFrame,
    inspectUnsubscribeFrame: inspectUnsubscribeFrame,
    isAgentTranscriptFrame: isAgentTranscriptFrame,
    applyInspectLiveFrame: applyInspectLiveFrame,
    inspectUserDedupeKey: inspectUserDedupeKey,
    isDuplicateInspectUserLine: isDuplicateInspectUserLine,
    paneModelFromWire: paneModelFromWire,
    sidebarComposerVisible: sidebarComposerVisible,
    isSidebarDraftEmpty: isSidebarDraftEmpty,
    agentSendPath: agentSendPath,
    sidebarSendRequest: sidebarSendRequest,
    sidebarSendBlockMessage: sidebarSendBlockMessage,
    createAsideRequestBody: createAsideRequestBody,
    freeformAsideCreateOpts: freeformAsideCreateOpts,
    classifySidebarComposerKey: classifySidebarComposerKey,
    // 🎯T265 microcosm
    mergePaneModelWithLines: mergePaneModelWithLines,
    inspectWorkingLabel: inspectWorkingLabel,
    afterSidebarSendOptimistic: afterSidebarSendOptimistic,
    inspectDisplayUserText: inspectDisplayUserText,
    inspectPaneIsConversationOnly: inspectPaneIsConversationOnly,
  };
}));
