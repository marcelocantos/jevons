// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Conversation widget (🎯T309.1): ONE surface for bubble list + message box +
// send. Main root chat and RHS sidebar both mount this module with params
// (agent id, density compact|comfortable). Compact is CSS/param only — not a
// second composer implementation.
//
// Pure helpers are DOM-free so Node hermetic tests can require(); mount() is
// the browser path that binds host nodes and wires input/send/keydown.
//
// Residual (ledger-allowed): VirtualList / history-scale stay main-host params
// and may be supplied by the main adopter rather than reimplemented here.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ConversationWidget = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
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
   * Enter-chord classification for the widget composer.
   * Compact density (RHS): Enter → send, Shift+Enter → newline (no interject).
   * Comfortable density: richer chords when ComposerKeys is available;
   * otherwise same Enter/Shift+Enter base as compact.
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
    var density = normalizeDensity(opts.density);
    var key = e.key;
    var code = e.code;
    var isEnter = key === 'Enter' || code === 'Enter' || code === 'NumpadEnter';
    if (!isEnter) return null;
    if (e.isComposing || e.keyCode === 229) return null;

    if (density === DENSITY_COMPACT) {
      if (e.shiftKey) return 'newline';
      return 'send';
    }

    // Comfortable: prefer shared ComposerKeys when present (main path).
    var CK = opts.ComposerKeys;
    if (CK && typeof CK.classifyEnterAction === 'function') {
      return CK.classifyEnterAction(key, {
        shiftKey: !!e.shiftKey,
        ctrlKey: !!e.ctrlKey,
        metaKey: !!e.metaKey,
        altKey: !!e.altKey,
        code: code,
      }, {
        composerEmpty: !!opts.composerEmpty,
        queueLen: opts.queueLen | 0,
        code: code,
      });
    }
    // Fallback without ComposerKeys: Enter / Shift+Enter only.
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
   * Lines fingerprint for no-op repaint (mirrors AgentTranscript.linesFingerprint shape).
   * @param {Array} lines
   * @param {boolean} [working]
   */
  function linesFingerprint(lines, working) {
    var parts = (lines || []).map(function (l) {
      return (l && l.role) + '\0' + (l && l.text) + '\0' + (l && l.when);
    });
    return parts.join('\n') + (working ? '|w' : '');
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
     * Build request + call onSend; optimistic path via afterSendOptimistic.
     * @returns {Promise}
     */
    function send() {
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
      return Promise.resolve(opts.onSend(req))
        .then(function (res) {
          clearComposer();
          var opt = afterSendOptimistic(_lines, req.body.text, {
            title: agentId,
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
          return res;
        })
        .catch(function (err) {
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

    // Wire composer events when nodes exist (compact path owns keys fully;
    // comfortable main may keep its own richer handlers and only adopt nodes).
    if (inputEl && opts.wireComposer !== false && density === DENSITY_COMPACT) {
      inputEl.addEventListener('input', function () {
        if (agentId) draftStore.set(agentId, inputEl.value);
        syncSendEnabled();
        if (typeof opts.onDraftChange === 'function') {
          opts.onDraftChange(agentId, inputEl.value);
        }
        // Compact auto-grow.
        inputEl.style.height = 'auto';
        var maxPx = Math.round((opts.viewportHeight || (typeof window !== 'undefined' ? window.innerHeight : 600)) * 0.22) || 120;
        var next = Math.min(inputEl.scrollHeight, maxPx);
        inputEl.style.height = next + 'px';
        inputEl.style.overflowY =
          inputEl.scrollHeight > maxPx + 1 ? 'auto' : 'hidden';
      });
      inputEl.addEventListener('keydown', function (e) {
        var act = classifyComposerKey(e, {
          density: density,
          ComposerKeys: opts.ComposerKeys,
          composerEmpty: isDraftEmpty(inputEl.value),
          queueLen: opts.queueLen | 0,
        });
        if (act === 'send') {
          e.preventDefault();
          e.stopPropagation();
          send();
        }
      });
    }
    if (sendBtn && opts.wireComposer !== false && density === DENSITY_COMPACT) {
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
    composerVisible: composerVisible,
    rootClassName: rootClassName,
    linesFingerprint: linesFingerprint,
    mount: mount,
  };
}));
