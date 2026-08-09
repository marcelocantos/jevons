// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T266 — Owner-facing target-ask context chrome (repo / PO / product).
// 🎯T273 — Split speaker-omit from context-paint.
// 🎯T314 — The tab is [optional speaker][optional context], in that order,
//   and nothing else:
//   • speaker = the PROVEN author of the bubble (opts.author / agent /
//     speaker / from). Omitted for the root overseer (jevons / overseer), and
//     never synthesized from repo, product, or the ledger-owning PO. An
//     org/repo label can never occupy the speaker role.
//   • context = repo/ledger chrome in its own dim visual role (ctx-context) —
//     never the bold name-like accent token, and never "· <po>" trailing the
//     head as a fake second speaker.
//   • the ledger-owning PO is provenance metadata (hover title + dataset),
//     not a painted token. It paints only when it is the actual author, in
//     which case it is the speaker.
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

  // The 🎯 is a surrogate pair: a bare `?` would bind to the low unit alone and
  // make the high unit mandatory, so the optional prefix must be a group.
  var TARGET_ID_RE = /(?:🎯)?\s*(T\d+(?:\.\d+)*)\b/gi;
  var ASK_CUE_RE = /needs[-\s]?owner|owner\s+(?:decision|accept|ack|gate|ratify)|decision\s+packet|parked[-\s]?for[-\s]?design|design[-\s]?gated|design[-\s]?discussion|blocked[-\s]?on[-\s]?(?:human|owner)|please\s+(?:confirm|accept|decide|choose|pick)|do\s+you\s+(?:want|accept|approve|prefer)|which\s+(?:repo|ledger|product|po|portfolio)|owner[-\s]?facing|ratif(?:y|ication)|\baccept\b|\bconfirm\b|\bdecide\b/i;
  // U+2329 LEFT-POINTING ANGLE BRACKET / U+232A RIGHT-POINTING ANGLE BRACKET
  var SPEAKER_LT = '〈';
  var SPEAKER_GT = '〉';

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
   * A PO name is a legitimate speaker identity — but only when it is the
   * proven author (🎯T314); it is never synthesized into the speaker role.
   */
  function isOmittedSpeakerIdentity(name) {
    var n = String(name == null ? '' : name).trim().toLowerCase();
    if (!n) return true;
    if (n === 'jevons' || n === 'overseer') return true;
    return false;
  }

  /**
   * 🎯T314: an org/repo label ("marcelocantos/jevons") is context, never a
   * speaker. Guards the speaker role against repo-shaped identities.
   */
  function looksLikeRepoLabel(name) {
    return String(name == null ? '' : name).indexOf('/') >= 0;
  }

  /**
   * 🎯T306 role gate: the context chrome is PROVENANCE — "this system message
   * came from repo X". On a bubble the owner typed there is no provenance to
   * state, so the tab is mislabeled by construction. Owner roles never paint,
   * regardless of 🎯 ids, ambient frontier ledger/cwd, ask cues, or force.
   */
  function isOwnerRole(role) {
    var r = String(role == null ? '' : role).trim().toLowerCase();
    return r === 'user' || r === 'owner' || r === 'me';
  }

  /** Product leaf is the Jevons product (context may still paint). */
  function isJevonsProduct(name) {
    var n = String(name == null ? '' : name).trim().toLowerCase();
    return n === 'jevons';
  }

  /**
   * 🎯T314 speaker = proven message author only.
   *
   * Reads author / authorName / agent / speaker / from — the fields a caller
   * fills in when it actually knows who wrote the bubble. Repo, product and
   * the ledger-owning PO are deliberately NOT consulted: inferring a speaker
   * from ownership is what put "marcelocantos/jevons · jevons-po" in the
   * speaker role. Empty ⇒ omit the speaker segment.
   */
  function messageAuthor(opts) {
    opts = opts && typeof opts === 'object' ? opts : {};
    var name = collapse(opts.author != null ? opts.author
      : (opts.authorName != null ? opts.authorName
        : (opts.agent != null ? opts.agent
          : (opts.speaker != null ? opts.speaker : opts.from))));
    if (!name) return '';
    if (looksLikeRepoLabel(name)) return '';
    if (isOmittedSpeakerIdentity(name)) return '';
    return name;
  }

  /** Back-compat alias: the speaker IS the proven author (🎯T314). */
  function speakerIdentity(ctx) {
    return messageAuthor(ctx);
  }

  /** Speaker byline only: 〈author〉 or empty. Never middle-dot · or bare Jevons. */
  function formatSpeakerLabel(nameOrCtx) {
    var id = '';
    if (nameOrCtx && typeof nameOrCtx === 'object') {
      id = messageAuthor(nameOrCtx);
    } else {
      id = collapse(nameOrCtx);
      if (looksLikeRepoLabel(id) || isOmittedSpeakerIdentity(id)) id = '';
    }
    if (!id) return '';
    return SPEAKER_LT + id + SPEAKER_GT;
  }

  /** Speaker byline HTML: bold 〈author〉 or empty (not context chrome). */
  function formatSpeakerHTML(nameOrCtx) {
    var label = formatSpeakerLabel(nameOrCtx);
    if (!label) return '';
    // Escape only the identity body; brackets are fixed product chrome.
    var id = label.slice(SPEAKER_LT.length, label.length - SPEAKER_GT.length);
    return '<span class="ctx-speaker">' + SPEAKER_LT + escHtml(id) + SPEAKER_GT + '</span>';
  }

  /**
   * Context head: the repo/ledger label. Prefer the full org/repo — a bare
   * product leaf ("jevons") reads as a name, which is the speaker role.
   */
  function contextHead(ctx) {
    ctx = ctx && typeof ctx === 'object' ? ctx : {};
    var repo = collapse(ctx.repo);
    if (repo) return repo;
    var product = collapse(ctx.product);
    if (product && isOmittedSpeakerIdentity(product)) return '';
    return product;
  }

  /**
   * 🎯T314 context-paint: repo/ledger chrome only. The owning PO is never
   * appended — "repo · po" reads as speaker·meta and the PO is inferred from
   * ownership, not authorship.
   */
  function formatChromeLabel(ctx) {
    return contextHead(ctx);
  }

  /**
   * Context chrome HTML. Rendered in ctx-context — the tab's own dim role,
   * visually distinct from the bold ctx-speaker name token.
   */
  function formatContextHTML(ctx) {
    var head = contextHead(ctx);
    if (!head) return '';
    return '<span class="ctx-context">' + escHtml(head) + '</span>';
  }

  function normalizeTargetID(raw) {
    var s = raw == null ? '' : String(raw).trim();
    if (!s) return '';
    s = s.replace(/^🎯/, '').trim();
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

  // Hover/aria title keeps full meta (· separators OK — not painted chrome).
  // 🎯T314: the owning PO is labelled "ledger PO" so the tooltip never implies
  // authorship; the painted tab omits it entirely.
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
    if (po) parts.push('ledger PO ' + po);
    var speaker = messageAuthor(ctx);
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

    // 🎯T306: owner-authored bubbles carry no provenance — gate before ask.
    var ownerAuthored = isOwnerRole(opts.role);
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
    // 🎯T314: speaker is the proven author only — never derived from po/repo.
    var speaker = messageAuthor(opts);
    // Context label is repo/ledger chrome alone (no "· PO" tail).
    var label = formatChromeLabel({ repo: repo, product: product });
    // 🎯T273: context-paint gates on ask + resolved chrome, never on speaker
    // omission. 🎯T306: …and never at all on an owner-authored bubble.
    // 🎯T314: a proven non-overseer author alone is enough to paint.
    var show = !!(ask && (repo || speaker)) && !ownerAuthored;
    var title = formatChromeTitle({
      repo: repo, po: po, product: product, targetId: primary, targetIds: ids,
      author: speaker,
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
   * 🎯T314 painted tab model: [optional speaker][optional context].
   *  - speaker: 〈author〉 in the bold ctx-speaker role, only when the author is
   *    proven and is not the root overseer. Repo labels and ledger-owning POs
   *    never reach this role.
   *  - context: repo/ledger label in the dim ctx-context role.
   *  - the two are separated by a plain space — never "·", which reads as a
   *    second speaker.
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

    var speakerLabel = formatSpeakerLabel(ctx.speaker);
    var speakerHTML = formatSpeakerHTML(ctx.speaker);
    var contextLabel = formatChromeLabel({ repo: ctx.repo, product: ctx.product });
    var contextHTML = formatContextHTML({ repo: ctx.repo, product: ctx.product });

    var joined = !!(speakerHTML && contextHTML);
    // The tab is an inline-flex row, so a plain space between the two flex
    // items can be trimmed away entirely — the painted gap is a NBSP. Plain
    // text (label / aria) keeps an ordinary space.
    var inner = speakerHTML +
      (joined ? '<span class="ctx-gap" aria-hidden="true"> </span>' : '') +
      contextHTML;
    var label = speakerLabel + (joined ? ' ' : '') + contextLabel;

    var show = !!inner;
    return {
      show: show,
      label: show ? label : '',
      title: ctx.title,
      repo: ctx.repo,
      po: ctx.po,
      product: ctx.product,
      speaker: ctx.speaker || '',
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
    isOwnerRole: isOwnerRole,
    isOmittedSpeakerIdentity: isOmittedSpeakerIdentity,
    looksLikeRepoLabel: looksLikeRepoLabel,
    isJevonsProduct: isJevonsProduct,
    messageAuthor: messageAuthor,
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
