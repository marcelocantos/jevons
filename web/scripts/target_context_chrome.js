// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T266 — Owner-facing target-ask context chrome (repo / PO / product).
// 🎯T273 — Split speaker-omit from context-paint:
//   • speaker-omit: pure overseer / product name "Jevons" only (not jevons-po).
//   • context-paint: always keep repo/target/ledger/owning-PO chrome when
//     resolved — including when context is/includes jevons-po.
//   • non-overseer speaker (other products): bold-purple 〈agent〉 only (no ·).
// DOM-free pure helpers; browser attach lives in index.html.
// T267 may reuse extractTargetIDs / resolveTargetContext for PO focus.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.TargetContextChrome = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  var TARGET_ID_RE = /(?:🎯|\uD83C\uDFAF)?\s*(T\d+(?:\.\d+)*)\b/gi;
  var ASK_CUE_RE = /needs[-\s]?owner|owner\s+(?:decision|accept|ack|gate|ratify)|decision\s+packet|parked[-\s]?for[-\s]?design|design[-\s]?gated|design[-\s]?discussion|blocked[-\s]?on[-\s]?(?:human|owner)|please\s+(?:confirm|accept|decide|choose|pick)|do\s+you\s+(?:want|accept|approve|prefer)|which\s+(?:repo|ledger|product|po|portfolio)|owner[-\s]?facing|ratif(?:y|ication)|\baccept\b|\bconfirm\b|\bdecide\b/i;
  // U+2329 LEFT-POINTING ANGLE BRACKET / U+232A RIGHT-POINTING ANGLE BRACKET
  var SPEAKER_LT = '\u2329';
  var SPEAKER_GT = '\u232A';

  function collapse(s) {
    return String(s == null ? '' : s).replace(/\s+/g, ' ').trim();
  }

  function escHtml(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  /**
   * 🎯T273 speaker-omit: pure overseer / product name "Jevons" only.
   * Does NOT omit jevons-po (owning PO is context, not a bare speaker stamp).
   */
  function isOmittedSpeakerIdentity(name) {
    var n = String(name == null ? '' : name).trim().toLowerCase();
    if (!n) return true;
    if (n === 'jevons' || n === 'overseer') return true;
    return false;
  }

  /** Product leaf is the Jevons product (context may still paint). */
  function isJevonsProduct(name) {
    var n = String(name == null ? '' : name).trim().toLowerCase();
    return n === 'jevons';
  }

  /**
   * Prefer PO / agent; else product when not pure Jevons speaker.
   * Empty ⇒ omit speaker byline only (context chrome is separate).
   */
  function speakerIdentity(ctx) {
    ctx = ctx && typeof ctx === 'object' ? ctx : {};
    var po = collapse(ctx.po);
    if (po && !isOmittedSpeakerIdentity(po)) return po;
    var product = collapse(ctx.product);
    if (!product) product = productFromRepoLabel(ctx.repo);
    if (product && !isOmittedSpeakerIdentity(product)) return product;
    var agent = collapse(ctx.agent || ctx.speaker || ctx.name);
    if (agent && !isOmittedSpeakerIdentity(agent)) return agent;
    return '';
  }

  /** Speaker byline only: 〈agent〉 or empty. Never middle-dot · or bare Jevons. */
  function formatSpeakerLabel(nameOrCtx) {
    var id = '';
    if (nameOrCtx && typeof nameOrCtx === 'object') {
      id = speakerIdentity(nameOrCtx);
    } else {
      id = collapse(nameOrCtx);
      if (isOmittedSpeakerIdentity(id)) id = '';
    }
    if (!id) return '';
    return SPEAKER_LT + id + SPEAKER_GT;
  }

  /** Speaker byline HTML: bold-purple 〈agent〉 or empty (not context chrome). */
  function formatSpeakerHTML(nameOrCtx) {
    var label = formatSpeakerLabel(nameOrCtx);
    if (!label) return '';
    // Escape only the identity body; brackets are fixed product chrome.
    var id = label.slice(SPEAKER_LT.length, label.length - SPEAKER_GT.length);
    return '<span class="ctx-speaker">' + SPEAKER_LT + escHtml(id) + SPEAKER_GT + '</span>';
  }

  /**
   * Context head (repo/product). Never use pure overseer/jevons product leaf
   * as the painted head — that stamps a speaker-looking "jevons" token
   * (b81267d residual). Prefer full repo when product is omitted speaker id.
   */
  function contextHead(ctx) {
    ctx = ctx && typeof ctx === 'object' ? ctx : {};
    var product = collapse(ctx.product);
    var repo = collapse(ctx.repo);
    if (product && isOmittedSpeakerIdentity(product)) {
      return repo || '';
    }
    return product || repo;
  }

  /**
   * 🎯T266 context-paint: repo/product + optional owning PO.
   * Always includes jevons-po when present — never blanked by speaker-omit.
   * Pure overseer product leaf is never the painted head (speaker-omit).
   */
  function formatChromeLabel(ctx) {
    ctx = ctx && typeof ctx === 'object' ? ctx : {};
    var head = contextHead(ctx);
    if (!head) return '';
    var po = collapse(ctx.po);
    return po ? head + ' · ' + po : head;
  }

  /** Context chrome HTML (repo/PO spans). Independent of speaker-omit. */
  function formatContextHTML(ctx) {
    ctx = ctx && typeof ctx === 'object' ? ctx : {};
    var head = contextHead(ctx);
    if (!head) return '';
    var po = collapse(ctx.po);
    var html = '<span class="ctx-repo">' + escHtml(head) + '</span>';
    if (po) {
      html += '<span class="ctx-sep" aria-hidden="true"> · </span>' +
        '<span class="ctx-po">' + escHtml(po) + '</span>';
    }
    return html;
  }

  function normalizeTargetID(raw) {
    var s = raw == null ? '' : String(raw).trim();
    if (!s) return '';
    s = s.replace(/^\uD83C\uDFAF/, '').replace(/^🎯/, '').trim();
    if (/^t\d/i.test(s)) s = 'T' + s.slice(1);
    return s;
  }

  function extractTargetIDs(text) {
    var s = String(text == null ? '' : text);
    if (!s) return [];
    var out = [];
    var seen = Object.create(null);
    TARGET_ID_RE.lastIndex = 0;
    var m;
    while ((m = TARGET_ID_RE.exec(s)) !== null) {
      var id = normalizeTargetID(m[1]);
      if (!id || seen[id]) continue;
      seen[id] = true;
      out.push(id);
    }
    return out;
  }

  function looksLikeTargetAsk(text) {
    var s = String(text == null ? '' : text);
    if (!s) return false;
    if (!extractTargetIDs(s).length) return false;
    if (ASK_CUE_RE.test(s)) return true;
    if (/\?/.test(s)) return true;
    if (/\bowner\b/i.test(s) && /\b(target|frontier|ledger|bullseye)\b/i.test(s)) return true;
    return false;
  }

  function repoLabelFromPath(path) {
    var s = String(path == null ? '' : path).replace(/\\/g, '/').replace(/\/+$/, '');
    if (!s) return '';
    s = s.replace(/\/bullseye\.ya?ml$/i, '');
    var gh = /(?:^|\/)(?:Users\/[^/]+\/)?work\/github\.com\/(.+)$/.exec(s)
      || /github\.com\/(.+)$/.exec(s);
    if (gh) {
      var parts = gh[1].replace(/\/+$/, '').split('/').filter(Boolean);
      if (parts.length >= 2) return parts[0] + '/' + parts[1];
      return parts.join('/') || '';
    }
    var work = /(?:^|\/)work\/(?:bitbucket\.org|gitlab\.com)\/(.+)$/.exec(s);
    if (work) {
      var wparts = work[1].split('/').filter(Boolean);
      if (wparts.length >= 2) return wparts[0] + '/' + wparts[1];
      return work[1];
    }
    var leaf = s.split('/').filter(Boolean).pop() || '';
    if (leaf === 'bullseye.yaml' || leaf === '.jevons') return '';
    return leaf;
  }

  function productFromRepoLabel(repo) {
    var r = collapse(repo);
    if (!r) return '';
    var i = r.lastIndexOf('/');
    return i >= 0 ? r.slice(i + 1) : r;
  }

  function isProductOwnerName(name) {
    var n = String(name || '').trim().toLowerCase();
    return !!(n && n.length >= 4 && n.slice(-3) === '-po');
  }

  function findAgentByName(agents, name) {
    var want = String(name || '').trim();
    if (!want) return null;
    var list = Array.isArray(agents) ? agents : [];
    for (var i = 0; i < list.length; i++) {
      if (list[i] && String(list[i].name || '').trim() === want) return list[i];
    }
    return null;
  }

  function findAgentsByTargetID(agents, targetId) {
    var tid = normalizeTargetID(targetId);
    if (!tid) return [];
    var list = Array.isArray(agents) ? agents : [];
    var out = [];
    for (var i = 0; i < list.length; i++) {
      var a = list[i];
      if (!a) continue;
      var at = normalizeTargetID(a.target_id != null ? a.target_id : (a.targetId || ''));
      if (at && at === tid) out.push(a);
    }
    return out;
  }

  function resolvePOForAgent(agents, agent) {
    if (!agent || typeof agent !== 'object') return '';
    if (isProductOwnerName(agent.name)) return String(agent.name).trim();
    var seen = Object.create(null);
    var cur = agent;
    var hops = 0;
    while (cur && hops < 16) {
      hops++;
      var parentName = String(cur.parent || '').trim();
      if (!parentName || seen[parentName]) break;
      seen[parentName] = true;
      if (isProductOwnerName(parentName)) return parentName;
      var parentRow = findAgentByName(agents, parentName);
      if (!parentRow) break;
      if (String(parentRow.purpose || parentRow.role || '').toLowerCase() === 'overseer') break;
      if (isProductOwnerName(parentRow.name)) return String(parentRow.name).trim();
      cur = parentRow;
    }
    return '';
  }

  function extractRepoHintsFromText(text) {
    var s = String(text == null ? '' : text);
    var out = [];
    var seen = Object.create(null);
    var ghRe = /github\.com\/([A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+)/g;
    var m;
    while ((m = ghRe.exec(s)) !== null) {
      var r = collapse(m[1]);
      if (r && !seen[r]) { seen[r] = true; out.push(r); }
    }
    return out;
  }

  // Hover/aria title keeps full meta (· separators OK — not speaker byline).
  function formatChromeTitle(ctx) {
    ctx = ctx && typeof ctx === 'object' ? ctx : {};
    var parts = [];
    var tid = normalizeTargetID(ctx.targetId);
    if (tid) parts.push('🎯' + tid);
    var repo = collapse(ctx.repo);
    if (repo) parts.push('repo ' + repo);
    var product = collapse(ctx.product);
    if (product && product !== repo && repo.indexOf('/' + product) < 0) {
      parts.push('product ' + product);
    }
    var po = collapse(ctx.po);
    if (po) parts.push('PO ' + po);
    var speaker = speakerIdentity(ctx);
    if (speaker) parts.push('speaker ' + SPEAKER_LT + speaker + SPEAKER_GT);
    return parts.length ? parts.join(' · ') : 'Target context';
  }

  function resolveTargetContext(opts) {
    opts = opts && typeof opts === 'object' ? opts : {};
    var text = opts.text != null ? String(opts.text) : '';
    var agents = Array.isArray(opts.agents) ? opts.agents : [];
    var ids = extractTargetIDs(text);
    var primary = normalizeTargetID(opts.targetId) || ids[0] || '';
    if (primary && ids.indexOf(primary) < 0) ids = [primary].concat(ids);

    var force = !!opts.force;
    var ask = force || looksLikeTargetAsk(text);
    if (!ask && (opts.repo || opts.workdir || opts.ledger || opts.cwd) && primary) {
      ask = true;
    }

    var repo = collapse(opts.repo);
    var po = collapse(opts.po);
    var product = collapse(opts.product);
    var workdir = opts.workdir != null ? String(opts.workdir) : '';

    if (primary) {
      var engaged = findAgentsByTargetID(agents, primary);
      var pick = null;
      for (var i = 0; i < engaged.length; i++) {
        var purp = String(engaged[i].purpose || engaged[i].role || '').toLowerCase();
        if (purp !== 'overseer') { pick = engaged[i]; break; }
      }
      if (!pick && engaged.length) pick = engaged[0];
      if (pick) {
        if (!workdir && pick.workdir) workdir = String(pick.workdir);
        if (!po) po = resolvePOForAgent(agents, pick);
      }
    }

    if (!repo) repo = repoLabelFromPath(workdir);
    if (!repo) repo = repoLabelFromPath(opts.ledger);
    if (!repo) repo = repoLabelFromPath(opts.cwd);
    if (!repo) {
      var hints = extractRepoHintsFromText(text);
      if (hints.length) repo = hints[0];
    }

    if (!po && workdir) {
      var wdNorm = workdir.replace(/\\/g, '/').replace(/\/+$/, '');
      for (var j = 0; j < agents.length; j++) {
        var a = agents[j];
        if (!a || !isProductOwnerName(a.name)) continue;
        var aw = String(a.workdir || '').replace(/\\/g, '/').replace(/\/+$/, '');
        if (!aw) continue;
        if (aw === wdNorm || wdNorm.indexOf(aw) === 0 || aw.indexOf(wdNorm) === 0) {
          po = String(a.name).trim();
          break;
        }
      }
    }

    // Ledger/cwd-only asks: match owning PO by agent workdir → same repo label.
    if (!po && repo) {
      for (var k = 0; k < agents.length; k++) {
        var pa = agents[k];
        if (!pa || !isProductOwnerName(pa.name)) continue;
        var prow = repoLabelFromPath(pa.workdir);
        if (prow && prow === repo) {
          po = String(pa.name).trim();
          break;
        }
      }
    }

    if (!product) product = productFromRepoLabel(repo);
    var speaker = speakerIdentity({ repo: repo, po: po, product: product });
    // Context label always (product · PO), independent of speaker-omit.
    var label = formatChromeLabel({ repo: repo, po: po, product: product, targetId: primary });
    // 🎯T273: context-paint gates on ask + repo only — never on speaker omit.
    var show = !!(ask && repo);
    var title = formatChromeTitle({
      repo: repo, po: po, product: product, targetId: primary, targetIds: ids,
    });

    return {
      show: show,
      targetId: primary,
      targetIds: ids,
      repo: repo,
      po: po,
      product: product,
      speaker: speaker,
      label: label,
      title: title,
    };
  }

  /**
   * Painted tab model.
   *  - Context path (show): ask + resolved repo — keeps jevons-po context.
   *  - Pure overseer/jevons: omit speaker token only; never stamp product leaf
   *    "jevons" as the heading (prefer full repo · PO context).
   *  - Non-overseer other-product speaker: 〈agent〉 bold purple, no ·.
   *  - Pure speaker helpers still omit bare Jevons/overseer (formatSpeaker*).
   */
  function chromeModel(opts) {
    var ctx = resolveTargetContext(opts || {});
    if (!ctx.show) {
      return {
        show: false,
        label: '',
        title: ctx.title,
        repo: ctx.repo,
        po: ctx.po,
        product: ctx.product,
        speaker: ctx.speaker || '',
        targetId: ctx.targetId,
        targetIds: ctx.targetIds,
        innerHTML: '',
      };
    }

    var product = collapse(ctx.product) || productFromRepoLabel(ctx.repo);
    var speaker = collapse(ctx.speaker) || speakerIdentity({
      repo: ctx.repo, po: ctx.po, product: product, agent: ctx.speaker,
    });
    // Never speaker-paint pure overseer/jevons — even if product mis-resolves.
    if (isOmittedSpeakerIdentity(speaker)) speaker = '';
    var useSpeakerPaint = !!(speaker && !isJevonsProduct(product) &&
      !isOmittedSpeakerIdentity(speaker));

    var inner = '';
    var label = '';
    if (useSpeakerPaint) {
      // Non-overseer speaker on other products: 〈agent〉 only (no ·).
      label = formatSpeakerLabel({
        repo: ctx.repo, po: ctx.po, product: product, agent: speaker,
      });
      inner = formatSpeakerHTML({
        repo: ctx.repo, po: ctx.po, product: product, agent: speaker,
      });
      // Defense: never emit pure overseer/jevons speaker token.
      if (inner && (isOmittedSpeakerIdentity(speaker) ||
          /〈\s*jevons\s*〉/i.test(inner) ||
          inner.indexOf(SPEAKER_LT + 'jevons' + SPEAKER_GT) >= 0 ||
          inner.indexOf(SPEAKER_LT + 'overseer' + SPEAKER_GT) >= 0)) {
        inner = '';
        label = '';
      }
      if (inner && (inner.indexOf('\u00b7') >= 0 || inner.indexOf(' · ') >= 0)) {
        inner = '';
        label = '';
      }
    }
    if (!inner) {
      // Context chrome (repo · owning PO) — includes jevons-po; never blank
      // the whole heading because speaker is pure overseer/Jevons.
      label = formatChromeLabel({
        repo: ctx.repo, po: ctx.po, product: product, targetId: ctx.targetId,
      });
      inner = formatContextHTML({
        repo: ctx.repo, po: ctx.po, product: product, targetId: ctx.targetId,
      });
    }

    var show = !!(inner);
    return {
      show: show,
      label: show ? label : '',
      title: ctx.title,
      repo: ctx.repo,
      po: ctx.po,
      product: ctx.product,
      speaker: speaker || '',
      targetId: ctx.targetId,
      targetIds: ctx.targetIds,
      innerHTML: show ? inner : '',
    };
  }

  return {
    extractTargetIDs: extractTargetIDs,
    looksLikeTargetAsk: looksLikeTargetAsk,
    normalizeTargetID: normalizeTargetID,
    repoLabelFromPath: repoLabelFromPath,
    productFromRepoLabel: productFromRepoLabel,
    isProductOwnerName: isProductOwnerName,
    isOmittedSpeakerIdentity: isOmittedSpeakerIdentity,
    isJevonsProduct: isJevonsProduct,
    speakerIdentity: speakerIdentity,
    formatSpeakerLabel: formatSpeakerLabel,
    formatSpeakerHTML: formatSpeakerHTML,
    formatContextHTML: formatContextHTML,
    contextHead: contextHead,
    findAgentsByTargetID: findAgentsByTargetID,
    resolvePOForAgent: resolvePOForAgent,
    extractRepoHintsFromText: extractRepoHintsFromText,
    resolveTargetContext: resolveTargetContext,
    formatChromeLabel: formatChromeLabel,
    formatChromeTitle: formatChromeTitle,
    chromeModel: chromeModel,
    SPEAKER_LT: SPEAKER_LT,
    SPEAKER_GT: SPEAKER_GT,
  };
}));
