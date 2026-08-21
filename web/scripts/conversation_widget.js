// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Conversation widget (🎯T309.1 / 🎯T372 / 🎯T480): ONE surface for bubble
// list + message box + send + T106 size clip. Main and sidebar Transcript
// are this module — same grow-one-bubble, same send entry, same rehydrate,
// same measure+clip. Density is CSS/param only; role chrome is presentation
// only. Collapse is size-only (tall → pocket tab; short → no tab).
// wireComposer:false is gone.
//
// Pure helpers are DOM-free so Node hermetic tests can require(); mount() is
// the browser path that binds host nodes and wires input/send/keydown.
//
// Residual: VirtualList geometry is a comfortable-density param (T119.4).
// Ingest is not: both mounts call applyWireEvent.

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

  // 🎯T480 / T106: size-only clip. Tall → pocket tab; short → no tab.
  // Role, inject kind, harness bootstrap, and "what it was sent for" do
  // not decide collapse. T233 nuggets stay nuggets (not this path).
  // --collapsed-max-height in CSS must match COLLAPSED_MAX_HEIGHT.
  var COLLAPSED_MAX_HEIGHT = '14rem';
  var COLLAPSE_HEIGHT_EPSILON_PX = 6;

  function collapsedMaxHeightPx(opts) {
    opts = opts || {};
    var named = opts.collapsedMaxHeight || COLLAPSED_MAX_HEIGHT;
    var m = /^([\d.]+)rem$/i.exec(named);
    if (m) {
      var root = opts.rootFontPx;
      if (root == null && typeof document !== 'undefined' && document.documentElement
          && typeof getComputedStyle === 'function') {
        try {
          root = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
        } catch (_) {
          root = 16;
        }
      }
      if (root == null || !(root > 0)) root = 16;
      return parseFloat(m[1]) * root;
    }
    var px = /^([\d.]+)px$/i.exec(named);
    return px ? parseFloat(px[1]) : 224;
  }

  function liveBodyHeight(d) {
    if (!d || !d._body) return 0;
    return d._body.scrollHeight || d._body.offsetHeight || 0;
  }

  function probeFullHeight(d, role, text, opts) {
    opts = opts || {};
    var doc = opts.document || (d && d.ownerDocument)
      || (typeof document !== 'undefined' ? document : null);
    var container = opts.container;
    if (!container && d && d.parentNode) container = d.parentNode;
    if (!doc || !container || typeof doc.createElement !== 'function') return 0;
    var width = (d && (d.clientWidth || (d._body && d._body.clientWidth))) || 0;
    var probe = doc.createElement('div');
    var cls = (d && d.className) || ('msg ' + (role || ''));
    probe.className = String(cls).replace(/\bmsg-clipped\b/g, '').trim();
    probe.style.cssText = 'position:absolute;visibility:hidden;pointer-events:none;left:0;top:0;width:'
      + (width > 0 ? width + 'px' : '100%') + ';max-width:85%;box-sizing:border-box;';
    var probeBody = doc.createElement('div');
    if (probeBody.classList && probeBody.classList.add) probeBody.classList.add('msg-body');
    probe.appendChild(probeBody);
    container.appendChild(probe);
    // Size-only: copy already-painted HTML. Do not fork on role to decide
    // what to measure — paintProbe is wrap geometry only.
    if (d && d._body && d._body.innerHTML) {
      probeBody.innerHTML = d._body.innerHTML;
    } else if (typeof opts.paintProbe === 'function') {
      opts.paintProbe(probeBody, role, text);
    } else if (text != null) {
      probeBody.textContent = text;
    }
    var fullH = probeBody.offsetHeight || probeBody.scrollHeight || 0;
    if (probe.parentNode && typeof probe.parentNode.removeChild === 'function') {
      probe.parentNode.removeChild(probe);
    } else if (typeof probe.remove === 'function') {
      probe.remove();
    }
    return fullH;
  }

  /**
   * Measure whether a bubble is tall enough to warrant the T106 pocket.
   * `role` and `text` are unused for the tall decision (🎯T480 size-only);
   * they remain in the signature so index.html / collapse-test keep calling
   * measureCollapse(d, role, text).
   * @returns {{ tall: boolean, fullH: number, collapsedH: number }}
   */
  function measureCollapse(d, role, text, opts) {
    opts = opts || {};
    var collapsedH = collapsedMaxHeightPx(opts);
    var fullH = 0;
    if (opts.fullH != null) {
      fullH = Number(opts.fullH) || 0;
    } else {
      fullH = liveBodyHeight(d);
      if (fullH <= 0) fullH = probeFullHeight(d, role, text, opts);
    }
    if (collapsedH <= 0 || fullH <= 0) {
      return { tall: false, fullH: fullH, collapsedH: collapsedH };
    }
    return {
      tall: fullH > collapsedH + COLLAPSE_HEIGHT_EPSILON_PX,
      fullH: fullH,
      collapsedH: collapsedH,
    };
  }

  function clearClipChrome(d) {
    if (!d) return;
    if (d.classList && d.classList.remove) d.classList.remove('msg-clipped');
    if (d._clipFade) {
      try {
        if (d._clipFade.remove) d._clipFade.remove();
        else if (d._clipFade.parentNode) d._clipFade.parentNode.removeChild(d._clipFade);
      } catch (_) { /* isolated */ }
      d._clipFade = null;
    }
    if (d._expandBtn) {
      try {
        if (d._expandBtn.remove) d._expandBtn.remove();
        else if (d._expandBtn.parentNode) d._expandBtn.parentNode.removeChild(d._expandBtn);
      } catch (_) { /* isolated */ }
      d._expandBtn = null;
    }
  }

  function applyClipState(d, clipped) {
    if (!d || !d._body) return;
    var doc = d.ownerDocument || (typeof document !== 'undefined' ? document : null);
    if (clipped) {
      if (d.classList && d.classList.add) d.classList.add('msg-clipped');
      if (!d._clipFade && doc && typeof doc.createElement === 'function') {
        var fade = doc.createElement('div');
        fade.className = 'msg-clip-fade';
        if (fade.setAttribute) fade.setAttribute('aria-hidden', 'true');
        if (d._body.after) d._body.after(fade);
        else d.appendChild(fade);
        d._clipFade = fade;
      } else if (d._clipFade && d._clipFade.parentNode !== d) {
        if (d._body.after) d._body.after(d._clipFade);
      }
    } else {
      if (d.classList && d.classList.remove) d.classList.remove('msg-clipped');
      if (d._clipFade) {
        try {
          if (d._clipFade.remove) d._clipFade.remove();
          else if (d._clipFade.parentNode) d._clipFade.parentNode.removeChild(d._clipFade);
        } catch (_) { /* isolated */ }
        d._clipFade = null;
      }
    }
  }

  function updateExpandTab(d) {
    if (!d || !d._expandBtn) return;
    var expanded = !!d._expanded;
    var btn = d._expandBtn;
    if (btn.setAttribute) {
      btn.setAttribute('aria-expanded', expanded ? 'true' : 'false');
      btn.setAttribute('aria-label', expanded ? 'Collapse' : 'Expand');
    }
    btn.title = expanded ? 'Collapse' : 'Expand';
    btn.innerHTML = '<span class="chev" aria-hidden="true">'
      + (expanded ? '\u25B4' : '\u25BE') + '</span>';
  }

  function ensureExpandToggle(d, opts) {
    opts = opts || {};
    if (!d || d._fullText == null) {
      if (d && d._expandBtn) {
        try {
          if (d._expandBtn.remove) d._expandBtn.remove();
          else if (d._expandBtn.parentNode) d._expandBtn.parentNode.removeChild(d._expandBtn);
        } catch (_) { /* isolated */ }
        d._expandBtn = null;
      }
      return;
    }
    if (!d._expandBtn) {
      var doc = opts.document || d.ownerDocument
        || (typeof document !== 'undefined' ? document : null);
      if (!doc || typeof doc.createElement !== 'function') return;
      var btn = doc.createElement('button');
      btn.type = 'button';
      btn.className = 'msg-expand-tab';
      btn.tabIndex = -1;
      if (typeof btn.addEventListener === 'function') {
        btn.addEventListener('mousedown', function (e) {
          if (e && e.preventDefault) e.preventDefault();
        });
        btn.addEventListener('click', function () {
          d._expanded = !d._expanded;
          d._userToggled = true;
          applyClipState(d, !d._expanded);
          updateExpandTab(d);
          if (typeof opts.onToggle === 'function') opts.onToggle(d);
          if (typeof opts.onAfterLayout === 'function') opts.onAfterLayout(d);
        });
      }
      if (d._clipFade && d._clipFade.after) d._clipFade.after(btn);
      else if (d._body && d._body.after) d._body.after(btn);
      else d.appendChild(btn);
      d._expandBtn = btn;
    } else if (d._clipFade && d._expandBtn.previousSibling !== d._clipFade
        && d._clipFade.after) {
      d._clipFade.after(d._expandBtn);
    } else if (!d._clipFade && d._body && d._expandBtn.previousSibling !== d._body
        && d._body.after) {
      d._body.after(d._expandBtn);
    }
    if (d._expandBtn) d._expandBtn.tabIndex = -1;
    updateExpandTab(d);
  }

  /**
   * After attach: measure by size and apply T106 clip + pocket tab.
   * Lazy unmeasured shells and mid-stream bubbles skip (T119 / T30.2).
   */
  function layoutSizeClip(d, opts) {
    opts = opts || {};
    if (!d) return null;
    if (d.isConnected === false) return null;
    if (d.classList && d.classList.contains('virt-shell') && !d._virtSize) return null;
    if (typeof d._streamRaw === 'string') {
      clearClipChrome(d);
      d._fullText = null;
      d._expanded = true;
      d._autoExpanded = true;
      return { tall: false, streamed: true, fullH: 0, collapsedH: collapsedMaxHeightPx(opts) };
    }
    var role = d._layoutRole;
    var text = d._layoutText;
    var m = measureCollapse(d, role, text, opts);
    if (!m.tall) {
      clearClipChrome(d);
      d._fullText = null;
      if (typeof opts.onAfterLayout === 'function') opts.onAfterLayout(d, m);
      return m;
    }
    d._fullText = text != null ? text : (d._body && d._body.textContent) || '';
    applyClipState(d, !d._expanded);
    ensureExpandToggle(d, opts);
    if (typeof opts.onAfterLayout === 'function') opts.onAfterLayout(d, m);
    return m;
  }

  function turnSlotLabel(items) {
    var n = items && items.length ? items.length : 0;
    if (!n) return '';
    return '⋯ ' + n + (n === 1 ? ' step' : ' steps');
  }

  // 🎯T119.10: activity-strip N is the open fold slot's item count, never a
  // second host accumulator (el._items.children.length resets on reload).
  function workingProgressFromSlot(slot) {
    var items = (slot && slot.items) || [];
    var n = items.length;
    if (!n) return '';
    var last = '';
    for (var i = n - 1; i >= 0; i--) {
      var it = items[i];
      if (it && (it.cls === 'tool-use' || it.cls === 'tool-result') && it.text) {
        last = String(it.text);
        break;
      }
    }
    var short = last.replace(/\s+/g, ' ').trim();
    if (short.length > 48) short = short.slice(0, 48);
    return n + (n === 1 ? ' step' : ' steps') + (short ? ' · ' + short : '');
  }

  // 🎯T119.6: a second startTurn while a slot is already on the canvas
  // must not mint. Mutation: always-create fails this.
  function shouldMintTurnSlot(existing, connected) {
    return !(existing && connected);
  }

  function ensureTurnSlot(canvas, existing) {
    var kids = canvas && canvas.children ? canvas.children : [];
    if (!shouldMintTurnSlot(existing, existing && kids.indexOf(existing) >= 0)) {
      return existing;
    }
    var el = { id: kids.length };
    kids.push(el);
    return el;
  }

  function summariseToolUse(c) {
    var name = (c && c.name) ? String(c.name) : 'tool';
    var extra = '';
    if (c && c.input && typeof ToolSummary !== 'undefined' && ToolSummary.summariseInput) {
      extra = ToolSummary.summariseInput(c.input);
    }
    return extra ? (name + ': ' + extra) : name;
  }

  function summariseToolResult(c) {
    if (!c) return '';
    var inner = c.content;
    if (typeof inner === 'string') return inner.slice(0, 120);
    if (Array.isArray(inner)) {
      var bits = [];
      for (var i = 0; i < inner.length; i++) {
        if (inner[i] && inner[i].type === 'text' && inner[i].text) {
          bits.push(String(inner[i].text).slice(0, 120));
        }
      }
      return bits.join(' ');
    }
    return '';
  }

  /**
   * Display model f(raw events). Consecutive tools with no other row
   * between them are one turn-slot (⋯ n steps, including n=1). A user
   * or visible assistant row after the open slot breaks the scope.
   * Parking on an earlier _stream (growAssistant skips turn-slots) is
   * not a visible break (🎯T119.9). Tools-only end_turn does not break.
   */
  function newDisplayFold(CE) {
    return {
      CE: CE || loadChatEvents(),
      out: [],
      open: null,
      silentById: Object.create(null),
      segmentEdge: false,
    };
  }

  function displayFromEvents(events, CE) {
    var st = newDisplayFold(CE);
    var tape = Array.isArray(events) ? events : [];
    for (var fi = 0; fi < tape.length; fi++) {
      foldDisplayEvent(st, tape[fi]);
    }
    return st.out;
  }

  function isOwnerUserBarrierText(text, CE) {
    if (text == null || !String(text).trim()) return false;
    if (CE && CE.isNonBoundaryUserText && CE.isNonBoundaryUserText(text)) return false;
    return true;
  }

  function sealOpenStreamFlags(rows) {
    var out = rows || [];
    for (var i = out.length - 1; i >= 0; i--) {
      if (out[i] && out[i]._stream) {
        delete out[i]._stream;
        delete out[i]._streamId;
      }
    }
  }

  // Scan backwards for an open assistant to grow (🎯T119.9 / 🎯T504).
  // Turn-slots are not barriers (T496 park / T119.9). T329 inject and
  // protocol-control user rows are not barriers. A real owner user row
  // is a scan barrier, and so is a sealed visible assistant — do not
  // grow an older open stream past either.
  function findGrowAssistantIndex(rows, streamId, CE) {
    var sid = streamId ? String(streamId) : '';
    var out = rows || [];
    for (var i = out.length - 1; i >= 0; i--) {
      var l = out[i];
      if (!l) continue;
      if (l.kind === 'turn-slot' || l.role === 'turn-slot') continue;
      if (l.role === 'user') {
        if (CE && CE.isNonBoundaryUserText && CE.isNonBoundaryUserText(l.text)) continue;
        return -1;
      }
      if (l.role !== 'assistant' && l.role !== 'jevons') continue;
      if (!l._stream) return -1;
      if (sid && l._streamId && l._streamId !== sid) continue;
      return i;
    }
    return -1;
  }

  // Incremental f (🎯T119.5). fold(prefix) equals displayFromEvents(prefix).
  function foldDisplayEvent(st, event) {
    if (!st) return [];
    var CE = st.CE;
    var out = st.out;
    var silentById = st.silentById;
    function closeOpen() {
      st.open = null;
    }
    function addTool(cls, text, ts) {
      var body = text == null ? '' : String(text);
      if (!body) return;
      if (!st.open) {
        st.open = { kind: 'turn-slot', role: 'turn-slot', items: [], text: '', when: ts };
        out.push(st.open);
      }
      st.open.items.push({ cls: cls || '', text: body });
      st.open.text = turnSlotLabel(st.open.items);
    }
    function findOpenStreamIndex(streamId) {
      return findGrowAssistantIndex(out, streamId, CE);
    }
    function openSlotIndex() {
      if (!st.open) return -1;
      for (var i = out.length - 1; i >= 0; i--) {
        if (out[i] === st.open) return i;
      }
      return -1;
    }
    function growAssistant(text, streamId, edge, ts) {
      var sid = streamId ? String(streamId) : '';
      var idx = findOpenStreamIndex(sid);
      if (idx >= 0) {
        var l = out[idx];
        l.text = edge
          ? (CE && CE.joinAssistantSegments ? CE.joinAssistantSegments(l.text, text) : (l.text + '\n\n' + text))
          : (CE && CE.appendAssistantStream ? CE.appendAssistantStream(l.text, text) : (l.text + text));
        if (sid) l._streamId = sid;
        return;
      }
      var row = { role: 'assistant', text: text, _stream: true, when: ts };
      if (sid) row._streamId = sid;
      out.push(row);
    }
    if (!event || !CE) return out;
    if (event.recorded === 'lossless') return out;
    var ts = event.when != null ? event.when
      : (event.timestamp ? new Date(event.timestamp).getTime() : undefined);
    if (event.type === 'user') {
      var utext = CE.userContentText ? CE.userContentText(event) : '';
      if (!utext) return out;
      if (CE.isProtocolControlFrameText && CE.isProtocolControlFrameText(utext)) return out;
      closeOpen();
      // 🎯T504: a real owner user seals open streams so later text cannot
      // grow the bubble above this row. T329 inject is not a barrier.
      if (isOwnerUserBarrierText(utext, CE)) sealOpenStreamFlags(out);
      var urow = { role: 'user', text: utext };
      if (ts != null) urow.when = ts;
      if (CE.turnOriginOf) urow.origin = CE.turnOriginOf(event);
      var last = out[out.length - 1];
      if (last && last.role === 'user' && String(last.text || '').trim() === utext.trim()) return out;
      out.push(urow);
      return out;
    }
    if (event.type === 'agent_note') {
      addTool('agent-note', event.text || '', ts);
      return out;
    }
    if (event.type === 'tool_result' || event.type === 'result') {
      // 🎯T119.10: a result completes a tool_use — it is not a second step.
      // Live WS may carry tool_result while hydrate journals only tool_use.
      // Counting both live and one on reload is the RED mutant.
      st.segmentEdge = true;
      return out;
    }
    if (event.type === 'system') {
      // System is not a display break. Closing here minted one host
      // turn-slot per agent_note+system pair during replay (🎯T494.1).
      return out;
    }
    if (event.type !== 'assistant') return out;
    var sid = CE.streamIdOf ? CE.streamIdOf(event) : String(event.stream_id || event.streamId || '');
    var content = event.message && event.message.content;
    if (!Array.isArray(content)) return out;
    var emitted = false;
    for (var i = 0; i < content.length; i++) {
      var c = content[i];
      if (!c) continue;
      if (c.type === 'tool_use') {
        st.segmentEdge = true;
        addTool('tool-use', summariseToolUse(c), ts);
        continue;
      }
      if (c.type !== 'text' || !c.text) continue;
      var alreadySilent = !!(sid && silentById[sid]);
      var thisSilent = CE.isSilentAssistantText && CE.isSilentAssistantText(c.text);
      if (alreadySilent || thisSilent) {
        if (sid) silentById[sid] = true;
        continue;
      }
      // 🎯T119.9: close only when this text will appear AFTER the open
      // slot (new tail row, or a stream that already sits after it).
      // growAssistant skips turn-slots (T119.9 park) and T329 inject, but
      // not an owner user or a sealed visible assistant (🎯T504). Closing
      // a park mints a second capsule under the previous one.
      var parkAt = findOpenStreamIndex(sid);
      var openAt = openSlotIndex();
      if (parkAt < 0 || (openAt >= 0 && parkAt > openAt)) {
        closeOpen();
      }
      var edge = st.segmentEdge || emitted;
      growAssistant(c.text, sid, edge, ts);
      st.segmentEdge = false;
      emitted = true;
    }
    if (CE.shouldClearWorking && CE.shouldClearWorking(event)) {
      for (var si = out.length - 1; si >= 0; si--) {
        if (out[si] && out[si]._stream && (!sid || out[si]._streamId === sid)) {
          delete out[si]._stream;
          break;
        }
      }
    }
    return out;
  }

  function createTurnMarkerEl(doc, slot) {
    if (!doc || typeof doc.createElement !== 'function') return null;
    slot = slot || { items: [] };
    var el = doc.createElement('div');
    el.className = 'turn-marker';
    var label = doc.createElement('span');
    el.appendChild(label);
    var tip = doc.createElement('div');
    tip.className = 'turn-tip';
    el.appendChild(tip);
    el._label = label;
    el._items = tip;
    var items = slot.items || [];
    // Empty items must not inherit a leftover "⋯ 1 step" label (🎯T119.10).
    label.textContent = items.length ? (slot.text || turnSlotLabel(items)) : '';
    for (var i = 0; i < items.length; i++) {
      var it = items[i];
      var d = doc.createElement('div');
      d.className = 'turn-item ' + ((it && it.cls) || '');
      d.textContent = (it && it.text) || '';
      tip.appendChild(d);
    }
    return el;
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
    var events = [];
    var fold = newDisplayFold();
    var messagesEl = opts.messagesEl || null;
    var doc = opts.document || (typeof document !== 'undefined' ? document : null);

    function paintTurnSlotEl(slot) {
      if (!slot || !slot.el) return;
      var label = turnSlotLabel(slot.items);
      slot.text = label;
      if (slot.el._label) slot.el._label.textContent = label;
      if (slot.el._items && doc) {
        slot.el._items.innerHTML = '';
        for (var i = 0; i < slot.items.length; i++) {
          var it = slot.items[i];
          var d = doc.createElement('div');
          d.className = 'turn-item ' + (it.cls || '');
          d.textContent = it.text || '';
          slot.el._items.appendChild(d);
        }
      }
    }

    function mintTurnMarkerEl(slot) {
      var el = createTurnMarkerEl(doc, slot);
      if (el) slot.el = el;
      return el;
    }

    function openTurnSlot(ts) {
      if (turnSlot) return turnSlot;
      turnSlot = { kind: 'turn-slot', role: 'turn-slot', items: [], text: '', when: ts };
      lines.push(turnSlot);
      if (typeof opts.onTurnSlotOpen === 'function') {
        opts.onTurnSlotOpen(turnSlot);
      } else if (doc && messagesEl) {
        var el = mintTurnMarkerEl(turnSlot);
        if (el && typeof messagesEl.appendChild === 'function') messagesEl.appendChild(el);
      }
      return turnSlot;
    }

    function addTurnSlotItem(cls, text, ts) {
      var body = text == null ? '' : String(text);
      if (!body) return;
      openTurnSlot(ts);
      turnSlot.items.push({ cls: cls || '', text: body });
      turnSlot.text = turnSlotLabel(turnSlot.items);
      if (typeof opts.onTurnSlotItem === 'function') {
        opts.onTurnSlotItem(turnSlot, cls, body);
      } else {
        paintTurnSlotEl(turnSlot);
      }
    }

    function closeTurnSlot() {
      if (!turnSlot) return;
      if (!turnSlot.items.length) {
        var idx = lines.indexOf(turnSlot);
        if (idx >= 0) lines.splice(idx, 1);
        if (typeof opts.onTurnSlotCancel === 'function') {
          opts.onTurnSlotCancel(turnSlot);
        } else if (turnSlot.el && turnSlot.el.parentNode) {
          try { turnSlot.el.parentNode.removeChild(turnSlot.el); } catch (_) { /* isolated */ }
        }
      }
      turnSlot = null;
    }

    function clipOpts() {
      return {
        container: messagesEl,
        document: doc,
        onToggle: opts.onClipToggle,
        onAfterLayout: opts.onAfterClip,
        paintProbe: opts.paintProbe,
      };
    }

    function clipAttached(el) {
      if (!el) return el;
      layoutSizeClip(el, clipOpts());
      return el;
    }

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

    function sealJoinOnOwnerUser(text) {
      if (!isOwnerUserBarrierText(text, loadChatEvents())) return;
      var ids = Object.keys(byId);
      for (var i = 0; i < ids.length; i++) sealAssistant(ids[i]);
      if (openEl && typeof openEl._streamRaw === 'string') {
        sealAssistant(openEl._streamId || '');
      }
      sealOpenStreamFlags(lines);
    }

    function growLine(text, streamId, edge) {
      var sid = streamId ? String(streamId) : '';
      var CE = loadChatEvents();
      var gi = findGrowAssistantIndex(lines, sid, CE);
      if (gi >= 0) {
        var l = lines[gi];
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
      // 🎯T504: leftover createStreamJoin callers must not grow a bubble
      // above an owner user even if resolveOpen still finds its handle.
      if (target && findGrowAssistantIndex(lines, streamId, CE) >= 0) {
        target._streamRaw = edge
          ? (CE ? CE.joinAssistantSegments(target._streamRaw, chunk) : (target._streamRaw + '\n\n' + chunk))
          : (CE ? CE.appendAssistantStream(target._streamRaw, chunk) : (target._streamRaw + chunk));
        growLine(chunk, streamId, edge);
        scheduleRender(target);
        return target;
      }
      var el = mintBubble(chunk, ts);
      growLine(chunk, streamId, false);
      if (!el) return null;
      if (streamId) {
        el._streamId = streamId;
        byId[streamId] = el;
      }
      openEl = el;
      segmentEdge = false;
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
      // 🎯T504: leftover path — seal open streams so resolveOpen cannot
      // grow the pre-user bubble. T329 inject does not seal.
      sealJoinOnOwnerUser(body);
      var row = { role: 'user', text: body };
      if (ts != null) row.when = ts;
      if (userOpts.origin) row.origin = userOpts.origin;
      lines.push(row);
      if (typeof opts.onUser === 'function') {
        return opts.onUser(body, ts, userOpts);
      }
      // Prefer addMsg (host list/canvas) over buildMsg+append to messagesEl.
      // Main passes both; appending to #messages parks bubbles AFTER the
      // canvas — a stack of left-aligned owner lines at the live end.
      if (typeof opts.addMsg === 'function') {
        return opts.addMsg('user', body, ts, {
          turnOrigin: userOpts.origin,
          timeIfKnown: !!opts.timeIfKnown,
        });
      }
      if (typeof opts.buildMsg === 'function' && messagesEl) {
        var el = opts.buildMsg('user', body, ts, { timeIfKnown: !!opts.timeIfKnown });
        if (el) messagesEl.appendChild(el);
        return clipAttached(el);
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
        d._layoutRole = 'user';
        d._layoutText = body;
        d.appendChild(b);
        messagesEl.appendChild(d);
        return clipAttached(d);
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
          // Host onSeal (main renderBody) applies the same clip. Inspect
          // has no onSeal — clip here after the default paint.
          clipAttached(el);
        }
      }
      markLineSealed(sid);
      clearHandles(sid || undefined);
      return el;
    }

    function paintNewDisplayRow(row) {
      if (!row) return;
      if (row.kind === 'turn-slot') {
        if (typeof opts.onTurnSlotOpen === 'function') opts.onTurnSlotOpen(row);
        else if (messagesEl) {
          var el = mintTurnMarkerEl(row);
          if (el && typeof messagesEl.appendChild === 'function') messagesEl.appendChild(el);
        }
        if (typeof opts.onTurnSlotItem === 'function') {
          for (var ti = 0; ti < (row.items || []).length; ti++) {
            opts.onTurnSlotItem(row, row.items[ti].cls, row.items[ti].text);
          }
        } else {
          paintTurnSlotEl(row);
        }
        return;
      }
      if (row.role === 'user') {
        if (typeof opts.onUser === 'function') {
          opts.onUser(row.text, row.when, { origin: row.origin });
        } else if (typeof opts.addMsg === 'function') {
          opts.addMsg('user', row.text, row.when, { turnOrigin: row.origin });
        } else if (typeof opts.buildMsg === 'function' && messagesEl) {
          var uel = opts.buildMsg('user', row.text, row.when, { timeIfKnown: !!opts.timeIfKnown });
          if (uel) messagesEl.appendChild(uel);
        }
        return;
      }
      if (row.role === 'assistant' || row.role === 'jevons') {
        var bel = mintBubble(row.text, row.when);
        if (bel) {
          bel._streamRaw = row.text;
          if (row._streamId) {
            bel._streamId = row._streamId;
            byId[row._streamId] = bel;
          }
          openEl = bel;
        }
      }
    }

    function syncDisplay(prev, next) {
      prev = prev || [];
      next = next || [];
      if (next.length === prev.length && next.length) {
        var a = prev[prev.length - 1];
        var b = next[next.length - 1];
        if (a && b && a.kind === 'turn-slot' && b.kind === 'turn-slot') {
          var oldN = (a.items || []).length;
          var newN = (b.items || []).length;
          if (newN > oldN) {
            if (typeof opts.onTurnSlotItem === 'function') {
              for (var i = oldN; i < newN; i++) {
                opts.onTurnSlotItem(b, b.items[i].cls, b.items[i].text);
              }
            } else {
              paintTurnSlotEl(b);
            }
          }
          // No return: the fold can grow an assistant stream row that is
          // no longer last (post-tool final text — 🎯T496) in the same
          // apply that leaves the turn-slot as the last row.
        }
        for (var di = 0; di < next.length; di++) {
          var p = prev[di];
          var n = next[di];
          if (!n || (n.role !== 'assistant' && n.role !== 'jevons')) continue;
          if (p === n) continue; // aliased, not snapshotted ⇒ unchanged
          if (p && p.text === n.text) continue;
          var target = resolveOpen(n._streamId || '');
          if (target) {
            target._streamRaw = n.text;
            scheduleRender(target);
          }
        }
        return;
      }
      if (next.length > prev.length) {
        for (var j = prev.length; j < next.length; j++) paintNewDisplayRow(next[j]);
      }
    }

    // fold.out is mutated in place. Aliasing prev to that array makes
    // syncDisplay see equal lengths after the first row and skip paint
    // (🎯T491: connect replay left one leftover bubble). Snapshot every
    // row the fold can mutate *before* fold: the last row, and any
    // still-open assistant stream row — post-tool final text grows a row
    // that is no longer last (🎯T496).
    function snapshotDisplay(rows) {
      rows = rows || [];
      var copy = rows.slice();
      for (var i = 0; i < copy.length; i++) {
        var r = copy[i];
        if (!r) continue;
        var streamRow = r._stream && (r.role === 'assistant' || r.role === 'jevons');
        if (i !== copy.length - 1 && !streamRow) continue;
        copy[i] = {
          kind: r.kind,
          role: r.role,
          text: r.text,
          items: r.items ? r.items.slice() : undefined,
          _stream: r._stream,
          _streamId: r._streamId,
        };
      }
      return copy;
    }

    function applyWireEvent(event) {
      if (event) events.push(event);
      var prev = snapshotDisplay(lines);
      if (event) foldDisplayEvent(fold, event);
      lines = fold.out;
      if (event && event.type === 'user') {
        var liveCE = loadChatEvents();
        var liveText = (liveCE && liveCE.userContentText)
          ? liveCE.userContentText(event)
          : '';
        if (isOwnerUserBarrierText(liveText, liveCE)) sealJoinOnOwnerUser(liveText);
      }
      syncDisplay(prev, lines);
      var follow = opts.scrollFollow;
      if (follow && follow.shouldPin && follow.shouldPin()
          && follow.applyAfterUpdate) {
        follow.applyAfterUpdate(messagesEl);
      }
      if (typeof opts.onWorkingProgress === 'function' && fold.open
          && fold.open.items && fold.open.items.length) {
        opts.onWorkingProgress(fold.open);
      }
    }

    function setLines(next) {
      lines = Array.isArray(next) ? next.slice() : [];
    }

    function getLines() {
      return lines.slice();
    }

    function reset() {
      fold = newDisplayFold();
      lines = [];
      events = [];
      clearHandles();
      if (messagesEl && messagesEl.innerHTML !== undefined) {
        messagesEl.innerHTML = '';
      }
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
      reset: reset,
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

    function sizeClipOpts() {
      return {
        container: messagesEl,
        document: doc,
        onToggle: opts.onClipToggle,
        onAfterLayout: opts.onAfterClip,
        paintProbe: opts.paintProbe,
      };
    }

    var stream = createStreamJoin({
      messagesEl: messagesEl,
      document: doc,
      scrollFollow: scrollFollow,
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
      onTurnSlotOpen: opts.onTurnSlotOpen,
      onTurnSlotItem: opts.onTurnSlotItem,
      onTurnSlotCancel: opts.onTurnSlotCancel,
      onWorkingProgress: opts.onWorkingProgress,
      onClipToggle: opts.onClipToggle,
      onAfterClip: opts.onAfterClip,
      paintProbe: opts.paintProbe,
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
          // 🎯T233 nuggets stay nuggets — T480 is about bubbles.
          var wrap = doc.createElement('div');
          wrap.innerHTML = spec.html || '';
          var node = wrap.firstChild;
          if (node) messagesEl.appendChild(node);
          continue;
        }
        if (spec.kind === 'turn-slot' || line.kind === 'turn-slot' || line.role === 'turn-slot') {
          var marker = createTurnMarkerEl(doc, {
            items: spec.items || line.items || [],
            text: spec.text || line.text || '',
          });
          if (marker) messagesEl.appendChild(marker);
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
          if (el) {
            messagesEl.appendChild(el);
            layoutSizeClip(el, sizeClipOpts());
          }
        } else {
          // Minimal fallback when buildMsg is not injected (hermetic only).
          // Product durable turns always pass buildMsg (T308 one-shell rule).
          var d = doc.createElement('div');
          d.classList.add('msg');
          d.classList.add(spec.role || 'status');
          var body = doc.createElement('div');
          body.classList.add('msg-body');
          body.textContent = spec.text || '';
          d._body = body;
          d._layoutRole = spec.role || 'status';
          d._layoutText = spec.text || '';
          d.appendChild(body);
          messagesEl.appendChild(d);
          layoutSizeClip(d, sizeClipOpts());
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

    function reset() {
      if (stream && typeof stream.reset === 'function') stream.reset();
      _lines = [];
      _fp = '';
      _working = false;
      if (messagesEl) messagesEl.innerHTML = '';
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
      reset: reset,
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
        if (scrollFollow && scrollFollow.shouldPin && scrollFollow.shouldPin()
            && scrollFollow.applyAfterUpdate) {
          scrollFollow.applyAfterUpdate(messagesEl);
        }
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
    applyEventTape: function (events) {
      return displayFromEvents(events);
    },
    displayFromEvents: displayFromEvents,
    newDisplayFold: newDisplayFold,
    foldDisplayEvent: foldDisplayEvent,
    createStreamJoin: createStreamJoin,
    turnSlotLabel: turnSlotLabel,
    workingProgressFromSlot: workingProgressFromSlot,
    shouldMintTurnSlot: shouldMintTurnSlot,
    ensureTurnSlot: ensureTurnSlot,
    createTurnMarkerEl: createTurnMarkerEl,
    mount: mount,
    // 🎯T480 / T106: one size-clip implementation for main and Transcript.
    COLLAPSED_MAX_HEIGHT: COLLAPSED_MAX_HEIGHT,
    COLLAPSE_HEIGHT_EPSILON_PX: COLLAPSE_HEIGHT_EPSILON_PX,
    collapsedMaxHeightPx: collapsedMaxHeightPx,
    measureCollapse: measureCollapse,
    clearClipChrome: clearClipChrome,
    applyClipState: applyClipState,
    updateExpandTab: updateExpandTab,
    ensureExpandToggle: ensureExpandToggle,
    layoutSizeClip: layoutSizeClip,
  };
}));
