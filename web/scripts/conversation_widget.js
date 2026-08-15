// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Conversation widget (🎯T309.1 / 🎯T372): ONE surface for bubble list +
// message box + send. Main and sidebar Transcript are this module — same
// grow-one-bubble, same send entry, same rehydrate. Density is CSS/param
// only; role chrome is presentation only. wireComposer:false is gone.
//
// Pure helpers are DOM-free so Node hermetic tests can require(); mount() is
// the browser path that binds host nodes and wires input/send/keydown.
//
// Residual (ledger-allowed): VirtualList / history-scale stay main-host params
// and may be supplied by the main adopter rather than reimplemented here.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory(require('./pending_turns.js'));
  } else {
    // Browser: pending_turns.js must load before this script.
    root.ConversationWidget = factory(root.PendingTurns);
  }
}(typeof self !== 'undefined' ? self : this, function (PendingTurns) {
  'use strict';

  var DENSITY_COMPACT = 'compact';
  var DENSITY_COMFORTABLE = 'comfortable';

  /**
   * @param {string|null|undefined} d
   * @returns {'compact'|'comfortable'}
   */
  function normalizeDensity(d) {
    return String(d == null ? '' : d).toLowerCase() === DENSITY_COMPACT
      ? DENSITY_COMPACT
      : DENSITY_COMFORTABLE;
  }

  /**
   * Default element ids for each density (product hosts adopt these).
   * Compact = RHS Transcript; comfortable = main #chat-pane.
   * @param {{ density?: string, ids?: object }} [opts]
   */
  function defaultIds(opts) {
    opts = opts || {};
    if (opts.ids && typeof opts.ids === 'object') {
      return {
        root: opts.ids.root || '',
        messages: opts.ids.messages || 'messages',
        composer: opts.ids.composer || 'input-bar',
        input: opts.ids.input || 'input',
        send: opts.ids.send || 'send',
      };
    }
    if (normalizeDensity(opts.density) === DENSITY_COMPACT) {
      return {
        root: 'agent-inspect',
        messages: 'agent-inspect-body',
        composer: 'agent-inspect-composer',
        input: 'agent-inspect-input',
        send: 'agent-inspect-send',
      };
    }
    return {
      root: 'chat-pane',
      messages: 'messages',
      composer: 'input-bar',
      input: 'input',
      send: 'send',
    };
  }

  /** True when draft is empty / whitespace-only. */
  function isDraftEmpty(text) {
    return !String(text == null ? '' : text).trim();
  }

  /**
   * Per-agent draft stash so selection switches do not lose text.
   * Optional `backing` object is shared (product: one map for residual sticky-draft
   * call sites and the widget mount).
   * @param {object} [backing]
   * @returns {{ get: function, set: function, clear: function, all: function }}
   */
  function createDraftStore(backing) {
    var map = (backing && typeof backing === 'object') ? backing : Object.create(null);
    return {
      get: function (agentId) {
        var k = String(agentId == null ? '' : agentId);
        return Object.prototype.hasOwnProperty.call(map, k) ? map[k] : '';
      },
      set: function (agentId, text) {
        var k = String(agentId == null ? '' : agentId);
        if (!k) return;
        map[k] = String(text == null ? '' : text);
      },
      clear: function (agentId) {
        var k = String(agentId == null ? '' : agentId);
        if (!k) return;
        map[k] = '';
      },
      all: function () {
        return map;
      },
    };
  }

  /**
   * Enter-chord classification for the widget composer (🎯T372: one chord
   * table, both surfaces). Density does not fork the chord set — ComposerKeys
   * when present; otherwise Enter / Shift+Enter.
   *
   * @param {{ key?: string, code?: string, shiftKey?: boolean, ctrlKey?: boolean,
   *           metaKey?: boolean, altKey?: boolean, isComposing?: boolean, keyCode?: number }} e
   * @param {{ density?: string, composerEmpty?: boolean, queueLen?: number,
   *           ComposerKeys?: { classifyEnterAction?: function } }} [opts]
   * @returns {'send'|'newline'|'interrupt'|'force_send'|'send_queue_now'|'noop'|null}
   */
  function classifyComposerKey(e, opts) {
    opts = opts || {};
    if (!e) return null;
    var key = e.key;
    var code = e.code;
    var isEnter = key === 'Enter' || code === 'Enter' || code === 'NumpadEnter';
    if (!isEnter) return null;
    if (e.isComposing || e.keyCode === 229) return null;

    var CK = opts.ComposerKeys;
    if (CK && typeof CK.classifyEnterAction === 'function') {
      var altHeld = !!(e.altKey || (typeof e.getModifierState === 'function' && e.getModifierState('Alt')));
      return CK.classifyEnterAction(key, {
        shiftKey: !!e.shiftKey,
        ctrlKey: !!e.ctrlKey,
        metaKey: !!e.metaKey,
        altKey: altHeld,
        code: code,
      }, {
        composerEmpty: !!opts.composerEmpty,
        queueLen: opts.queueLen | 0,
        code: code,
      });
    }
    if (e.shiftKey) return 'newline';
    return 'send';
  }

  /** Product HTTP path for named-agent send (same shape as T182 / T309.2). */
  function agentSendPath(name) {
    var n = String(name == null ? '' : name).trim();
    if (!n) return '';
    return '/api/agents/' + encodeURIComponent(n) + '/send';
  }

  /**
   * Build send request for an agent-addressed composer mount.
   * Main overseer may use a different transport (WS); this is the HTTP family.
   * @param {string|null|undefined} agentId
   * @param {string} text
   * @param {{ purpose?: string, allowOverseer?: boolean,
   *           isOverseer?: function(string, string=): boolean }} [opts]
   * @returns {{ ok:true, name:string, url:string, method:string, body:{text:string} }
   *          |{ ok:false, reason:string }}
   */
  function buildSendRequest(agentId, text, opts) {
    opts = opts || {};
    var name = agentId == null ? '' : String(agentId).trim();
    if (!name) {
      return { ok: false, reason: 'no-selection' };
    }
    var isOv = typeof opts.isOverseer === 'function'
      ? opts.isOverseer
      : function (n) { return String(n || '').toLowerCase() === 'jevons'; };
    if (!opts.allowOverseer && isOv(name, opts.purpose)) {
      return { ok: false, reason: 'overseer-main-only' };
    }
    var body = String(text == null ? '' : text).trim();
    if (!body) {
      return { ok: false, reason: 'empty' };
    }
    var url = agentSendPath(name);
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

  /** Loud owner-facing copy when send is blocked before HTTP. */
  function sendBlockMessage(reason) {
    var r = String(reason == null ? '' : reason).trim();
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
   * After successful send: optimistic owner turn + open working chrome.
   * @param {Array} lines
   * @param {string} text
   * @param {{ title?: string, now?: number,
   *           isDuplicate?: function, normalizeWhen?: function }} [opts]
   */
  function afterSendOptimistic(lines, text, opts) {
    opts = opts || {};
    var body = String(text == null ? '' : text).trim();
    var next = Array.isArray(lines)
      ? lines.map(function (l) {
          return l ? { role: l.role, text: l.text, when: l.when } : l;
        })
      : [];
    if (body) {
      var last = next[next.length - 1];
      var dup = typeof opts.isDuplicate === 'function'
        ? opts.isDuplicate(last, body)
        : !!(last && last.role === 'user' && String(last.text || '').trim() === body);
      if (!dup) {
        var when;
        if (opts.now !== undefined) {
          when = typeof opts.normalizeWhen === 'function'
            ? opts.normalizeWhen(opts.now)
            : opts.now;
        } else {
          when = Date.now();
        }
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

  // ── Pending owner turns: THE shared contract (🎯T372) ──────────────
  //
  // 🎯T371 fixed the sidebar vanish by giving agent panes the staging +
  // repaint that main has had since 🎯T239/🎯T279 — but as a SECOND
  // implementation of it. 🎯T372 collapsed both into web/scripts/pending_turns.js,
  // which is agent-keyed (main is simply the agent `PendingTurns.MAIN_AGENT`),
  // so the widget and main chat now run the identical algorithm rather than two
  // that merely agree. The names below stay the widget's public surface; they
  // are bindings, not definitions. Re-defining any of them here re-forks the
  // contract and fails pending_turns_test.js §4.
  //
  // Durability (main localStorage vs agent in-memory) is EC-5 in
  // docs/design/one-chat-widget-fork-inventory.md — an OWNER ruling, and a
  // choice of store, not of algorithm.

  /**
   * Whether the compact sidebar composer should be visible.
   * @param {{ tab?: string, selectedAgent?: string|null, purpose?: string,
   *           shouldOpen?: function(string, string=): boolean }} opts
   */
  function composerVisible(opts) {
    opts = opts || {};
    var tab = String(opts.tab || '');
    if (tab !== 'transcript') return false;
    var name = opts.selectedAgent == null ? '' : String(opts.selectedAgent).trim();
    if (!name) return false;
    if (typeof opts.shouldOpen === 'function') {
      return !!opts.shouldOpen(name, opts.purpose);
    }
    return String(name).toLowerCase() !== 'jevons';
  }

  /**
   * CSS class list for a widget root given density.
   * @param {string} density
   * @returns {string}
   */
  function rootClassName(density) {
    return 'conversation-widget density-' + normalizeDensity(density);
  }

  /**
   * Lines fingerprint for no-op repaint — the one definition (🎯T372).
   * AgentTranscript.linesFingerprint is an alias onto this; it used to be a
   * separate copy that omitted `when`, so host and widget could disagree about
   * whether a re-timestamped line set had changed. Do not re-mirror it.
   * @param {Array} lines
   * @param {boolean} [working]
   */
  function linesFingerprint(lines, working) {
    var parts = (lines || []).map(function (l) {
      return (l && l.role) + '\0' + (l && l.text) + '\0' + (l && l.when);
    });
    return parts.join('\n') + (working ? '|w' : '');
  }

  function loadChatEvents() {
    if (typeof ChatEvents !== 'undefined') return ChatEvents;
    if (typeof module === 'object' && module.exports) {
      try { return require('./chat_events.js'); } catch (_) { return null; }
    }
    return null;
  }

  function nextFrame(fn, raf) {
    var impl = raf;
    if (typeof impl !== 'function') {
      impl = typeof requestAnimationFrame === 'function' ? requestAnimationFrame : null;
    }
    if (typeof impl === 'function') return impl(fn);
    fn();
    return 0;
  }

  function cancelFrame(id, caf) {
    var impl = caf;
    if (typeof impl !== 'function') {
      impl = typeof cancelAnimationFrame === 'function' ? cancelAnimationFrame : null;
    }
    if (typeof impl === 'function' && id) impl(id);
  }

  /**
   * One grow-one-bubble join (🎯T372). Both surfaces call this — not a
   * helper shared by two painters. Join identity is stream_id / openEl +
   * _streamRaw. Grok word-chunks (Plan / remaining / is / …) stay one bubble.
   *
   * @param {{
   *   messagesEl?: Element,
   *   document?: Document,
   *   buildMsg?: function,
   *   addMsg?: function,
   *   paintBody?: function,
   *   onStreamFrame?: function,
   *   onSeal?: function,
   *   onUser?: function,
   *   timeIfKnown?: boolean,
   *   requestAnimationFrame?: function,
   *   cancelAnimationFrame?: function,
   *   isDuplicateUser?: function
   * }} [opts]
   */
  function createStreamJoin(opts) {
    opts = opts || {};
    var openEl = null;
    var byId = Object.create(null);
    var silentById = Object.create(null);
    var segmentEdge = false;
    var lines = [];
    var messagesEl = opts.messagesEl || null;
    var doc = opts.document || (typeof document !== 'undefined' ? document : null);

    function rehome(el) {
      if (!el || typeof el._streamRaw !== 'string') return null;
      // Node hermetic fakes omit isConnected — treat missing as connected.
      if (el.isConnected === false && messagesEl) {
        try { messagesEl.appendChild(el); } catch (_) { /* ignore */ }
      }
      return el.isConnected === false ? null : el;
    }

    function resolveOpen(streamId) {
      var sid = streamId ? String(streamId) : '';
      if (sid && byId[sid]) {
        var mapped = rehome(byId[sid]);
        if (mapped) return mapped;
        delete byId[sid];
      }
      if (openEl && typeof openEl._streamRaw === 'string') {
        var unlabeled = !openEl._streamId;
        var sameId = sid && openEl._streamId === sid;
        if (!sid || unlabeled || sameId) {
          var adopted = rehome(openEl);
          if (adopted) {
            if (sid) {
              adopted._streamId = sid;
              byId[sid] = adopted;
            }
            return adopted;
          }
          openEl = null;
        }
      }
      if (sid && messagesEl && messagesEl.querySelectorAll) {
        var nodes = messagesEl.querySelectorAll('.msg.jevons');
        for (var i = 0; i < nodes.length; i++) {
          var el = nodes[i];
          if (el && el._streamId === sid && typeof el._streamRaw === 'string') {
            byId[sid] = el;
            openEl = el;
            return el;
          }
        }
      }
      return null;
    }

    function clearHandles(streamId) {
      if (streamId) {
        var el = byId[streamId];
        delete byId[streamId];
        delete silentById[streamId];
        if (openEl === el) openEl = null;
      } else {
        openEl = null;
        Object.keys(byId).forEach(function (k) { delete byId[k]; });
        Object.keys(silentById).forEach(function (k) { delete silentById[k]; });
      }
      segmentEdge = false;
    }

    function markLineSealed(streamId) {
      var sid = streamId ? String(streamId) : '';
      for (var i = lines.length - 1; i >= 0; i--) {
        var l = lines[i];
        if (!l || (l.role !== 'assistant' && l.role !== 'jevons')) continue;
        if (!l._stream) continue;
        if (!sid || l._streamId === sid) {
          delete l._stream;
          return;
        }
      }
    }

    function growLine(text, streamId, edge) {
      var sid = streamId ? String(streamId) : '';
      for (var i = lines.length - 1; i >= 0; i--) {
        var l = lines[i];
        if (!l || (l.role !== 'assistant' && l.role !== 'jevons')) continue;
        if (!l._stream) continue;
        if (sid && l._streamId && l._streamId !== sid) continue;
        var CE = loadChatEvents();
        l.text = edge
          ? (CE ? CE.joinAssistantSegments(l.text, text) : (l.text + '\n\n' + text))
          : (CE ? CE.appendAssistantStream(l.text, text) : (l.text + text));
        if (sid) l._streamId = sid;
        return;
      }
      var row = { role: 'assistant', text: text, _stream: true };
      if (sid) row._streamId = sid;
      lines.push(row);
    }

    function scheduleRender(el) {
      if (!el || el._renderRaf) return;
      el._renderRaf = nextFrame(function () {
        el._renderRaf = 0;
        if (typeof opts.onStreamFrame === 'function') {
          opts.onStreamFrame(el);
          return;
        }
        if (el._body) {
          if (typeof opts.paintBody === 'function') {
            opts.paintBody(el, 'jevons', el._streamRaw);
          } else {
            el._body.textContent = el._streamRaw || '';
          }
        }
      }, opts.requestAnimationFrame);
    }

    function mintBubble(text, ts) {
      var el = null;
      if (typeof opts.addMsg === 'function') {
        el = opts.addMsg('jevons', text, ts, { streamOpen: true });
      } else if (typeof opts.buildMsg === 'function') {
        el = opts.buildMsg('jevons', text, ts, {
          streamOpen: true,
          timeIfKnown: !!opts.timeIfKnown,
        });
        if (el && messagesEl && typeof messagesEl.appendChild === 'function') {
          messagesEl.appendChild(el);
        }
      } else if (doc && typeof doc.createElement === 'function') {
        el = doc.createElement('div');
        if (el.classList && el.classList.add) {
          el.classList.add('msg');
          el.classList.add('jevons');
        }
        var body = doc.createElement('div');
        if (body.classList && body.classList.add) body.classList.add('msg-body');
        body.textContent = text;
        el._body = body;
        el.appendChild(body);
        if (messagesEl && typeof messagesEl.appendChild === 'function') {
          messagesEl.appendChild(el);
        }
      }
      if (el) {
        el._streamRaw = typeof el._streamRaw === 'string' ? el._streamRaw : text;
        if (el.isConnected === undefined) el.isConnected = true;
      }
      return el;
    }

    function appendAssistant(text, ts, appendOpts) {
      appendOpts = appendOpts || {};
      var chunk = text == null ? '' : String(text);
      if (!chunk) return null;
      var streamId = appendOpts.streamId ? String(appendOpts.streamId) : '';
      var edge = !!(appendOpts.segmentEdge);
      var CE = loadChatEvents();
      var target = resolveOpen(streamId);
      if (target) {
        target._streamRaw = edge
          ? (CE ? CE.joinAssistantSegments(target._streamRaw, chunk) : (target._streamRaw + '\n\n' + chunk))
          : (CE ? CE.appendAssistantStream(target._streamRaw, chunk) : (target._streamRaw + chunk));
        growLine(chunk, streamId, edge);
        scheduleRender(target);
        return target;
      }
      var el = mintBubble(chunk, ts);
      if (!el) return null;
      if (streamId) {
        el._streamId = streamId;
        byId[streamId] = el;
      }
      openEl = el;
      segmentEdge = false;
      growLine(chunk, streamId, false);
      return el;
    }

    function appendUser(text, ts, userOpts) {
      userOpts = userOpts || {};
      var body = text == null ? '' : String(text);
      if (!body) return null;
      var last = lines[lines.length - 1];
      var dup = typeof opts.isDuplicateUser === 'function'
        ? opts.isDuplicateUser(last, body)
        : !!(last && last.role === 'user' && String(last.text || '').trim() === body.trim());
      if (dup) return null;
      var row = { role: 'user', text: body };
      if (ts != null) row.when = ts;
      if (userOpts.origin) row.origin = userOpts.origin;
      lines.push(row);
      if (typeof opts.onUser === 'function') {
        return opts.onUser(body, ts, userOpts);
      }
      if (typeof opts.buildMsg === 'function' && messagesEl) {
        var el = opts.buildMsg('user', body, ts, { timeIfKnown: !!opts.timeIfKnown });
        if (el) messagesEl.appendChild(el);
        return el;
      }
      if (doc && messagesEl) {
        var d = doc.createElement('div');
        if (d.classList && d.classList.add) {
          d.classList.add('msg');
          d.classList.add('user');
        }
        var b = doc.createElement('div');
        if (b.classList && b.classList.add) b.classList.add('msg-body');
        b.textContent = body;
        d._body = b;
        d.appendChild(b);
        messagesEl.appendChild(d);
        return d;
      }
      return null;
    }

    function sealAssistant(streamId) {
      var sid = streamId ? String(streamId) : '';
      var el = null;
      if (sid && byId[sid] && typeof byId[sid]._streamRaw === 'string') {
        el = byId[sid];
      } else if (!sid && openEl && typeof openEl._streamRaw === 'string') {
        el = openEl;
      } else {
        el = resolveOpen(sid);
      }
      if (el && typeof el._streamRaw === 'string') {
        cancelFrame(el._renderRaf, opts.cancelAnimationFrame);
        el._renderRaf = 0;
        var raw = el._streamRaw;
        delete el._streamRaw;
        el._layoutText = raw;
        el._layoutRole = 'jevons';
        if (typeof opts.onSeal === 'function') {
          opts.onSeal(el, raw, sid);
        } else if (el._body) {
          if (typeof opts.paintBody === 'function') {
            opts.paintBody(el, 'jevons', raw);
          } else {
            el._body.textContent = raw;
          }
        }
      }
      markLineSealed(sid);
      clearHandles(sid || undefined);
      return el;
    }

    function applyWireEvent(event) {
      var CE = loadChatEvents();
      if (!event || !CE) return;
      var ts = event.when != null ? event.when
        : (event.timestamp ? new Date(event.timestamp).getTime() : Date.now());
      if (event.type === 'user') {
        var utext = CE.userContentText ? CE.userContentText(event) : '';
        if (!utext) return;
        if (CE.isProtocolControlFrameText && CE.isProtocolControlFrameText(utext)) return;
        appendUser(utext, ts, {
          origin: CE.turnOriginOf ? CE.turnOriginOf(event) : undefined,
        });
        return;
      }
      if (event.type === 'tool_result' || event.type === 'result') {
        segmentEdge = true;
        return;
      }
      if (event.type === 'system') {
        if (CE.shouldClearWorking && CE.shouldClearWorking(event)) sealAssistant();
        return;
      }
      if (event.type !== 'assistant') return;
      var sid = CE.streamIdOf ? CE.streamIdOf(event) : String(event.stream_id || event.streamId || '');
      var content = event.message && event.message.content;
      if (!Array.isArray(content)) return;
      var emitted = false;
      for (var i = 0; i < content.length; i++) {
        var c = content[i];
        if (!c) continue;
        if (c.type === 'tool_use') {
          segmentEdge = true;
          continue;
        }
        if (c.type !== 'text' || !c.text) continue;
        var alreadySilent = !!(sid && silentById[sid]);
        var thisSilent = CE.isSilentAssistantText && CE.isSilentAssistantText(c.text);
        if (alreadySilent || thisSilent) {
          if (sid) silentById[sid] = true;
          continue;
        }
        var edge = segmentEdge || emitted;
        appendAssistant(c.text, ts, { streamId: sid, segmentEdge: edge });
        segmentEdge = false;
        emitted = true;
      }
      if (CE.shouldClearWorking && CE.shouldClearWorking(event)) {
        sealAssistant(sid);
      }
    }

    function setLines(next) {
      lines = Array.isArray(next) ? next.slice() : [];
    }

    function getLines() {
      return lines.slice();
    }

    function setMessagesEl(el) {
      messagesEl = el || null;
    }

    return {
      appendAssistant: appendAssistant,
      appendUser: appendUser,
      sealAssistant: sealAssistant,
      applyWireEvent: applyWireEvent,
      resolveOpen: resolveOpen,
      clearHandles: clearHandles,
      getOpenEl: function () { return openEl; },
      getLines: getLines,
      setLines: setLines,
      setMessagesEl: setMessagesEl,
    };
  }

  /**
   * Mount (or adopt) a conversation widget into a host element.
   *
   * opts:
   *   agentId, density ('compact'|'comfortable')
   *   adopt: true → use existing #ids under host/document (product default)
   *   ids: override defaultIds
   *   draftStore: shared createDraftStore() instance
   *   timeIfKnown: true for inspect (omit .msg-time when no timestamp)
   *   buildMsg(role, text, when, paintOpts) — bubble constructor (index buildMsg)
   *   lineSpec(line) → {kind:'nugget',html}|{kind:'bubble',role,text,when,painted}
   *   paintBody(el, role, text, painted) — optional body policy
   *   workingLabel(agentId) → string
   *   escapeHtml(s)
   *   onSend(req) → Promise  (product transport; required for send())
   *   onDraftChange(agentId, text)
   *   onAfterOptimistic(opt)
   *   sendEnabled: function() → boolean extra gate
   *   document: Document (for tests)
   *
   * @param {Element|null} host
   * @param {object} [opts]
   * @returns {object|null} controller or null if host missing / no document
   */
  function mount(host, opts) {
    opts = opts || {};
    var doc = opts.document || (typeof document !== 'undefined' ? document : null);
    if (!host || !doc) return null;

    var density = normalizeDensity(opts.density);
    var ids = defaultIds({ density: density, ids: opts.ids });
    var agentId = opts.agentId != null ? String(opts.agentId) : '';
    var draftStore = opts.draftStore || createDraftStore();
    var sending = false;
    var _fp = '';
    var _lines = [];
    var _working = false;
    var scrollFollow = opts.scrollFollow || null;

    // Resolve or create structural nodes.
    var messagesEl = null;
    var composerEl = null;
    var inputEl = null;
    var sendBtn = null;

    if (opts.adopt !== false) {
      messagesEl = (ids.messages && (host.querySelector
        ? host.querySelector('#' + cssEscapeId(ids.messages)) || doc.getElementById(ids.messages)
        : doc.getElementById(ids.messages))) || null;
      composerEl = ids.composer
        ? (host.querySelector
          ? host.querySelector('#' + cssEscapeId(ids.composer)) || doc.getElementById(ids.composer)
          : doc.getElementById(ids.composer))
        : null;
      inputEl = ids.input
        ? (host.querySelector
          ? host.querySelector('#' + cssEscapeId(ids.input)) || doc.getElementById(ids.input)
          : doc.getElementById(ids.input))
        : null;
      sendBtn = ids.send
        ? (host.querySelector
          ? host.querySelector('#' + cssEscapeId(ids.send)) || doc.getElementById(ids.send)
          : doc.getElementById(ids.send))
        : null;
    }

    var stream = createStreamJoin({
      messagesEl: messagesEl,
      document: doc,
      buildMsg: opts.buildMsg,
      addMsg: opts.addMsg,
      paintBody: opts.paintBody,
      onStreamFrame: opts.onStreamFrame,
      onSeal: opts.onSeal,
      onUser: opts.onUser,
      timeIfKnown: opts.timeIfKnown,
      requestAnimationFrame: opts.requestAnimationFrame,
      cancelAnimationFrame: opts.cancelAnimationFrame,
      isDuplicateUser: opts.isDuplicate,
    });

    // Tag host as widget mount (density is styling only).
    if (host.classList) {
      host.classList.add('conversation-widget');
      host.classList.remove('density-compact', 'density-comfortable');
      host.classList.add('density-' + density);
    }
    if (host.dataset) {
      host.dataset.density = density;
      if (agentId) host.dataset.agentId = agentId;
    }
    if (composerEl && composerEl.classList) {
      composerEl.classList.add('cw-composer');
      composerEl.classList.add('density-' + density);
    }

    function cssEscapeId(id) {
      // IDs in product are simple tokens; avoid CSS.escape dependency.
      return String(id).replace(/([^a-zA-Z0-9_-])/g, '\\$1');
    }

    function setAgentId(next) {
      var prev = agentId;
      if (inputEl && prev && prev !== next) {
        draftStore.set(prev, inputEl.value);
      }
      agentId = next == null ? '' : String(next);
      if (host.dataset) {
        if (agentId) host.dataset.agentId = agentId;
        else delete host.dataset.agentId;
      }
      if (inputEl) {
        if (agentId) {
          var saved = draftStore.get(agentId);
          if (!inputEl.dataset.boundAgent || inputEl.dataset.boundAgent !== agentId) {
            inputEl.value = saved;
          }
          inputEl.dataset.boundAgent = agentId;
        }
      }
      syncSendEnabled();
    }

    function setDensity(next) {
      density = normalizeDensity(next);
      if (host.classList) {
        host.classList.remove('density-compact', 'density-comfortable');
        host.classList.add('density-' + density);
      }
      if (host.dataset) host.dataset.density = density;
      if (composerEl && composerEl.classList) {
        composerEl.classList.remove('density-compact', 'density-comfortable');
        composerEl.classList.add('density-' + density);
      }
    }

    function setComposerVisible(visible) {
      if (!composerEl) return;
      if (composerEl.classList) {
        composerEl.classList.toggle('visible', !!visible);
      }
      composerEl.hidden = !visible;
      if (inputEl) inputEl.disabled = !visible;
      syncSendEnabled();
    }

    function syncSendEnabled() {
      if (!sendBtn) return;
      var empty = isDraftEmpty(inputEl ? inputEl.value : '');
      var extra = typeof opts.sendEnabled === 'function' ? !!opts.sendEnabled() : true;
      var can = !empty && !!agentId && !sending && extra;
      sendBtn.disabled = !can;
    }

    function setSending(v) {
      sending = !!v;
      syncSendEnabled();
    }

    function getDraft() {
      return inputEl ? String(inputEl.value || '') : '';
    }

    function setDraft(text) {
      if (!inputEl) return;
      inputEl.value = String(text == null ? '' : text);
      if (agentId) draftStore.set(agentId, inputEl.value);
      syncSendEnabled();
    }

    function clearComposer() {
      if (inputEl) inputEl.value = '';
      if (agentId) draftStore.clear(agentId);
      syncSendEnabled();
    }

    function stashDraft() {
      if (inputEl && agentId) draftStore.set(agentId, inputEl.value);
    }

    /**
     * Render bubble list from a pane model (inspect/main line array).
     * Uses injected buildMsg + lineSpec; fingerprint skips unchanged.
     */
    function renderModel(model) {
      model = model || {};
      if (!messagesEl) return;
      stream.clearHandles();
      if (model.lines) stream.setLines(model.lines);

      if (model.error) {
        _fp = '';
        _working = false;
        messagesEl.innerHTML = '';
        var err = doc.createElement('div');
        err.className = 'ai-err';
        err.textContent = model.error;
        messagesEl.appendChild(err);
        if (model.empty) {
          var emptyErr = doc.createElement('div');
          emptyErr.className = 'ai-empty';
          emptyErr.textContent = 'No turns yet — agent may still be starting.';
          messagesEl.appendChild(emptyErr);
        }
        return;
      }

      var lines = model.lines || [];
      var wantWorking = !!model.working;
      if (model.empty && !wantWorking) {
        _fp = '';
        _working = false;
        messagesEl.innerHTML = '';
        var empty = doc.createElement('div');
        empty.className = 'ai-empty';
        empty.textContent = 'No transcript turns yet for this agent.';
        messagesEl.appendChild(empty);
        return;
      }

      var fp = linesFingerprint(lines, wantWorking);
      if (fp === _fp &&
          (messagesEl.querySelector('.msg') ||
           messagesEl.querySelector('.inject-nugget') ||
           messagesEl.querySelector('.working-indicator'))) {
        return;
      }

      _working = wantWorking;
      _lines = lines.slice();
      var prevTop = messagesEl.scrollTop;
      messagesEl.innerHTML = '';

      var buildMsg = opts.buildMsg;
      var lineSpec = opts.lineSpec;
      var paintBody = opts.paintBody;

      for (var i = 0; i < lines.length; i++) {
        var line = lines[i];
        if (!line) continue;
        var spec = typeof lineSpec === 'function'
          ? lineSpec(line)
          : {
              kind: 'bubble',
              role: line.role === 'user' ? 'user'
                : (line.role === 'assistant' ? 'jevons' : (line.role || 'status')),
              text: line.text || '',
              when: line.when,
              painted: null,
            };
        if (spec.kind === 'nugget') {
          var wrap = doc.createElement('div');
          wrap.innerHTML = spec.html || '';
          var node = wrap.firstChild;
          if (node) messagesEl.appendChild(node);
          continue;
        }
        if (typeof buildMsg === 'function') {
          var painted = spec.painted;
          var el = buildMsg(spec.role, spec.text, spec.when, {
            timeIfKnown: opts.timeIfKnown !== false && density === DENSITY_COMPACT
              ? true
              : !!opts.timeIfKnown,
            paint: typeof paintBody === 'function'
              ? function (d, role, text) { paintBody(d, role, text, painted); }
              : undefined,
          });
          if (el) messagesEl.appendChild(el);
        } else {
          // Minimal fallback when buildMsg is not injected (hermetic only).
          // Product durable turns always pass buildMsg (T308 one-shell rule).
          var d = doc.createElement('div');
          d.classList.add('msg');
          d.classList.add(spec.role || 'status');
          var body = doc.createElement('div');
          body.classList.add('msg-body');
          body.textContent = spec.text || '';
          d.appendChild(body);
          messagesEl.appendChild(d);
        }
      }

      if (wantWorking) {
        var wi = doc.createElement('div');
        wi.className = 'working-indicator';
        wi.setAttribute('data-aside-working', '1');
        var label = typeof opts.workingLabel === 'function'
          ? opts.workingLabel(model.title || agentId || '')
          : 'Working…';
        var esc = typeof opts.escapeHtml === 'function'
          ? opts.escapeHtml
          : function (s) {
              return String(s == null ? '' : s)
                .replace(/&/g, '&amp;')
                .replace(/</g, '&lt;')
                .replace(/>/g, '&gt;')
                .replace(/"/g, '&quot;');
            };
        wi.innerHTML = '<span class="work-dots" aria-hidden="true"><span></span><span></span><span></span></span> '
          + esc(label);
        messagesEl.appendChild(wi);
      }

      _fp = fp;
      if (scrollFollow && scrollFollow.applyAfterUpdate) {
        scrollFollow.applyAfterUpdate(messagesEl, prevTop);
        if (scrollFollow.shouldPin && scrollFollow.shouldPin()) {
          // rAF pin is host responsibility if needed; sync path here.
          scrollFollow.applyAfterUpdate(messagesEl);
        }
      }
    }

    function invalidatePaint() {
      _fp = '';
    }

    function getLines() {
      return _lines.slice();
    }

    function setLines(lines) {
      _lines = Array.isArray(lines) ? lines.slice() : [];
    }

    /**
     * Build request, stage + paint the owner turn, then call onSend.
     *
     * 🎯T371: the optimistic bubble and the pending stage happen on ACCEPT —
     * before the transport resolves — which is the contract main has had since
     * 🎯T279. Painting only in the success handler left a window in which the
     * pane could be rebound (after-send attention switch) or overwritten (a
     * history frame) before the owner's own turn ever reached the DOM, and a
     * failed send painted nothing at all.
     * @returns {Promise}
     */
    function send(sendOpts) {
      sendOpts = sendOpts || {};
      // Host-owned transport + prefixes + queue (main). Widget is still
      // the only composer send entry — chords land here first (🎯T372).
      if (typeof opts.onComposerAction === 'function') {
        return Promise.resolve(opts.onComposerAction({
          text: getDraft(),
          action: sendOpts.action || 'send',
          interrupt: !!sendOpts.interrupt,
          agentId: agentId,
        }));
      }
      var text = getDraft();
      var purpose = typeof opts.getPurpose === 'function'
        ? opts.getPurpose(agentId)
        : opts.purpose;
      var req = buildSendRequest(agentId, text, {
        purpose: purpose,
        allowOverseer: !!opts.allowOverseer,
        isOverseer: opts.isOverseer,
      });
      if (!req.ok) {
        if (typeof opts.onSendBlocked === 'function') {
          opts.onSendBlocked(req.reason, sendBlockMessage(req.reason));
        }
        syncSendEnabled();
        return Promise.resolve(null);
      }
      if (typeof opts.onSend !== 'function') {
        return Promise.resolve(req);
      }
      // Host may own the live line model (inspect wire); pull it before optimistic.
      if (typeof opts.getLines === 'function') {
        var hostLines = opts.getLines();
        if (Array.isArray(hostLines)) _lines = hostLines.slice();
      }
      setSending(true);
      // Stage first: the turn is durable in the pending set before any
      // transport can fail, and re-applied onto every later history frame.
      var sentAgent = req.name;
      var staged = null;
      if (typeof opts.onStagePending === 'function') {
        staged = opts.onStagePending(sentAgent, req.body.text);
      }
      // Paint on accept (🎯T279 parity), not on 200.
      clearComposer();
      var opt = afterSendOptimistic(_lines, req.body.text, {
        title: sentAgent,
        isDuplicate: opts.isDuplicate,
        normalizeWhen: opts.normalizeWhen,
      });
      _lines = opt.lines;
      _working = true;
      invalidatePaint();
      if (typeof opts.onAfterOptimistic === 'function') {
        opts.onAfterOptimistic(opt);
      } else {
        renderModel(opt.model);
      }
      return Promise.resolve(opts.onSend(req))
        .then(function (res) {
          if (typeof opts.onSendAccepted === 'function') {
            opts.onSendAccepted(sentAgent, res);
          }
          return res;
        })
        .catch(function (err) {
          // A failed send keeps its bubble (marked failed) — never a vanish.
          if (typeof opts.onSendFailed === 'function') {
            opts.onSendFailed(sentAgent, staged, err);
          }
          if (typeof opts.onSendError === 'function') {
            opts.onSendError(err);
          }
          return null;
        })
        .finally(function () {
          setSending(false);
          if (inputEl && typeof inputEl.focus === 'function') {
            try { inputEl.focus(); } catch (_) { /* isolated */ }
          }
        });
    }

    // 🎯T372: one composer wiring. wireComposer:false is gone — both
    // densities bind keys + send here. Host may add Home/End / history
    // listeners; Enter-family chords are this widget's.
    if (inputEl) {
      inputEl.addEventListener('input', function () {
        if (agentId) draftStore.set(agentId, inputEl.value);
        syncSendEnabled();
        if (typeof opts.onDraftChange === 'function') {
          opts.onDraftChange(agentId, inputEl.value);
        }
        if (density === DENSITY_COMPACT) {
          inputEl.style.height = 'auto';
          var maxPx = Math.round((opts.viewportHeight || (typeof window !== 'undefined' ? window.innerHeight : 600)) * 0.22) || 120;
          var nextH = Math.min(inputEl.scrollHeight, maxPx);
          inputEl.style.height = nextH + 'px';
          inputEl.style.overflowY =
            inputEl.scrollHeight > maxPx + 1 ? 'auto' : 'hidden';
        }
      });
      inputEl.addEventListener('keydown', function (e) {
        var empty = typeof opts.isComposerEmpty === 'function'
          ? !!opts.isComposerEmpty(inputEl.value)
          : isDraftEmpty(inputEl.value);
        var qLen = typeof opts.queueLen === 'function' ? (opts.queueLen() | 0) : (opts.queueLen | 0);
        var act = classifyComposerKey(e, {
          density: density,
          ComposerKeys: opts.ComposerKeys,
          composerEmpty: empty,
          queueLen: qLen,
        });
        if (act === 'newline' || act == null) return;
        e.preventDefault();
        e.stopPropagation();
        if (act === 'noop') return;
        if (act === 'send_queue_now') {
          if (typeof opts.onSendQueueNow === 'function') opts.onSendQueueNow();
          return;
        }
        send({ interrupt: act === 'interrupt' || act === 'force_send', action: act });
      });
    }
    if (sendBtn) {
      sendBtn.addEventListener('mousedown', function (e) { e.preventDefault(); });
      sendBtn.addEventListener('click', function (e) {
        e.preventDefault();
        send();
      });
    }

    syncSendEnabled();

    return {
      host: host,
      messagesEl: messagesEl,
      composerEl: composerEl,
      inputEl: inputEl,
      sendBtn: sendBtn,
      density: function () { return density; },
      agentId: function () { return agentId; },
      setAgentId: setAgentId,
      setDensity: setDensity,
      setComposerVisible: setComposerVisible,
      setSending: setSending,
      syncSendEnabled: syncSendEnabled,
      getDraft: getDraft,
      setDraft: setDraft,
      clearComposer: clearComposer,
      stashDraft: stashDraft,
      draftStore: draftStore,
      renderModel: renderModel,
      invalidatePaint: invalidatePaint,
      getLines: getLines,
      setLines: setLines,
      send: send,
      isSending: function () { return sending; },
      ids: ids,
      appendAssistant: function (text, ts, aopts) {
        var el = stream.appendAssistant(text, ts, aopts);
        _lines = stream.getLines();
        return el;
      },
      appendUser: function (text, ts, uopts) {
        var el = stream.appendUser(text, ts, uopts);
        _lines = stream.getLines();
        return el;
      },
      sealAssistant: function (streamId) {
        var el = stream.sealAssistant(streamId);
        _lines = stream.getLines();
        return el;
      },
      applyWireEvent: function (event) {
        stream.applyWireEvent(event);
        _lines = stream.getLines();
      },
      clearStreamHandles: function (streamId) { stream.clearHandles(streamId); },
      getOpenStreamEl: function () { return stream.getOpenEl(); },
      setWorking: function (want) {
        _working = !!want;
        if (!messagesEl) return;
        var existing = messagesEl.querySelector
          ? messagesEl.querySelector('.working-indicator') : null;
        if (_working && !existing) {
          renderModel({ lines: _lines, working: true, title: agentId });
        } else if (!_working && existing && existing.parentNode) {
          existing.parentNode.removeChild(existing);
        }
      },
    };
  }

  return {
    DENSITY_COMPACT: DENSITY_COMPACT,
    DENSITY_COMFORTABLE: DENSITY_COMFORTABLE,
    normalizeDensity: normalizeDensity,
    defaultIds: defaultIds,
    isDraftEmpty: isDraftEmpty,
    createDraftStore: createDraftStore,
    classifyComposerKey: classifyComposerKey,
    agentSendPath: agentSendPath,
    buildSendRequest: buildSendRequest,
    sendBlockMessage: sendBlockMessage,
    afterSendOptimistic: afterSendOptimistic,
    // 🎯T372: bindings to the one contract — never re-define these here.
    emptyPending: PendingTurns.empty,
    stagePendingOwnerTurn: PendingTurns.stage,
    ackPendingOwnerTurns: PendingTurns.ack,
    applyPendingOwnerTurns: PendingTurns.apply,
    markPendingOwnerTurnFailed: PendingTurns.markFailed,
    pendingOwnerTurnsFor: PendingTurns.forAgent,
    composerVisible: composerVisible,
    rootClassName: rootClassName,
    linesFingerprint: linesFingerprint,
    createStreamJoin: createStreamJoin,
    mount: mount,
  };
}));
